package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func writeInputDoc(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const inputDecls = `[
  {"root": "repository", "path": "docs/context.md"},
  {"root": "repository", "path": "docs/context.md", "section": {"heading_path": ["Alpha"]}}
]`

// TestSetInputsReplacesInputsAndHashes: set-inputs writes the declarations and
// the tool-computed hashes, and nothing else.
func TestSetInputsReplacesInputsAndHashes(t *testing.T) {
	root, planDir := fixtureRoot(t)
	writeInputDoc(t, root, "docs/context.md", "# Context\n\n## Alpha\n\nalpha body\n")

	before := loadNode(t, planDir, "helper")
	decl, err := model.DecodeInputs([]byte(inputDecls))
	if err != nil {
		t.Fatalf("decode inputs: %v", err)
	}
	res, err := SetInputs(root, root, "SamplePlan", "helper", decl, false)
	if err != nil {
		t.Fatalf("set-inputs: %v", err)
	}
	if res.Inputs != 2 || len(res.Hashes) != 2 {
		t.Fatalf("result: %+v", res)
	}

	after := loadNode(t, planDir, "helper")
	if len(after.Inputs) != 2 {
		t.Fatalf("inputs not written: %+v", after.Inputs)
	}
	if !strings.HasPrefix(after.InputHashes[`{"root":"repository","path":"docs/context.md"}`], "sha256:") ||
		!strings.HasPrefix(after.InputHashes[`{"root":"repository","path":"docs/context.md","section":{"heading_path":["Alpha"]}}`], "sha256:") {
		t.Fatalf("hashes not embedded: %+v", after.InputHashes)
	}
	// No other field changed.
	if before.Contract != after.Contract || before.Estimate != after.Estimate ||
		len(before.Justifies) != len(after.Justifies) || before.Gate.Type != after.Gate.Type {
		t.Fatalf("set-inputs must change only inputs and input_hashes:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func loadNode(t *testing.T, planDir, id string) model.Node {
	t.Helper()
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	n := g.NodeByID(id)
	if n == nil {
		t.Fatalf("node %q missing", id)
	}
	return *n
}

// TestSetInputsDryRunWritesNothing: --dry-run reports the same plan and the
// graph stays byte-identical.
func TestSetInputsDryRunWritesNothing(t *testing.T) {
	root, planDir := fixtureRoot(t)
	writeInputDoc(t, root, "docs/context.md", "# Context\n\n## Alpha\n\nalpha body\n")
	path := gstore.PathFor(planDir)
	beforeBytes, _ := os.ReadFile(path)

	decl, _ := model.DecodeInputs([]byte(inputDecls))
	res, err := SetInputs(root, root, "SamplePlan", "helper", decl, true)
	if err != nil {
		t.Fatalf("dry-run set-inputs: %v", err)
	}
	if !res.DryRun || res.Inputs != 2 {
		t.Fatalf("dry-run result: %+v", res)
	}
	afterBytes, _ := os.ReadFile(path)
	if string(beforeBytes) != string(afterBytes) {
		t.Fatalf("dry-run must not write the graph")
	}
}

// TestSetInputsRefusesEligibility: a claimed, verified, or red-observed node
// refuses atomically (RefusedError, exit 1) and nothing is written.
func TestSetInputsRefusesEligibility(t *testing.T) {
	decl, _ := model.DecodeInputs([]byte(inputDecls))

	cases := []struct {
		name string
		mut  func(g *model.Graph) error
		want string
	}{
		{"claimed", func(g *model.Graph) error {
			g.NodeByID("helper").Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
			return nil
		}, "is claimed by"},
		{"verified", func(g *model.Graph) error {
			g.NodeByID("helper").Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}
			return nil
		}, "recorded verification"},
		{"red-observed", func(g *model.Graph) error {
			g.NodeByID("helper").RedSeqs = map[string]int{"t": 1}
			return nil
		}, "red observations"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, planDir := fixtureRoot(t)
			writeInputDoc(t, root, "docs/context.md", "# Context\n\n## Alpha\n\nalpha body\n")
			if _, err := gstore.Update(gstore.PathFor(planDir), tc.mut); err != nil {
				t.Fatal(err)
			}
			_, err := SetInputs(root, root, "SamplePlan", "helper", decl, false)
			var refusal *RefusedError
			if err == nil || !errorAs(err, &refusal) {
				t.Fatalf("expected a RefusedError, got %v", err)
			}
			found := false
			for _, r := range refusal.Reasons {
				if strings.Contains(r, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("refusal reasons %v missing %q", refusal.Reasons, tc.want)
			}
			n := loadNode(t, planDir, "helper")
			if len(n.Inputs) != 0 || len(n.InputHashes) != 0 {
				t.Fatalf("a refused set-inputs must write nothing: %+v", n)
			}
		})
	}
}

func errorAs(err error, target **RefusedError) bool {
	for err != nil {
		if re, ok := err.(*RefusedError); ok {
			*target = re
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
			continue
		}
		return false
	}
	return false
}

// TestSetInputsRefusesUnresolvedInput: a declaration that does not resolve is
// an authoritative refusal, and nothing is written.
func TestSetInputsRefusesUnresolvedInput(t *testing.T) {
	root, planDir := fixtureRoot(t)
	decl, _ := model.DecodeInputs([]byte(`[{"root": "repository", "path": "docs/nope.md"}]`))
	_, err := SetInputs(root, root, "SamplePlan", "helper", decl, false)
	var refusal *RefusedError
	if err == nil || !errorAs(err, &refusal) {
		t.Fatalf("expected a RefusedError, got %v", err)
	}
	if !strings.Contains(refusal.Error(), "does not exist") {
		t.Fatalf("refusal should name the missing input: %v", refusal.Error())
	}
	n := loadNode(t, planDir, "helper")
	if len(n.Inputs) != 0 {
		t.Fatalf("a refused set-inputs must write nothing: %+v", n.Inputs)
	}
}

// TestSetInputsRefusesCompetingClaimOnCASRetry: a claim landing between the
// first eligibility check and the write forces a re-plan that refuses and
// preserves the competing claim.
func TestSetInputsRefusesCompetingClaimOnCASRetry(t *testing.T) {
	root, planDir := fixtureRoot(t)
	writeInputDoc(t, root, "docs/context.md", "# Context\n\n## Alpha\n\nalpha body\n")
	decl, _ := model.DecodeInputs([]byte(inputDecls))
	path := gstore.PathFor(planDir)

	_, err := setInputsWith(root, root, "SamplePlan", "helper", decl, false,
		contendWith(t, path, func(g *model.Graph) error {
			g.NodeByID("helper").Claim = &model.Claim{By: "racer", LeaseExpires: "2099-01-01T00:00:00Z"}
			return nil
		}))
	var refusal *RefusedError
	if err == nil || !errorAs(err, &refusal) {
		t.Fatalf("the retry must re-plan against the competing claim and refuse: %v", err)
	}
	g, err := gstore.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	n := g.NodeByID("helper")
	if n.Claim == nil || n.Claim.By != "racer" {
		t.Fatalf("the competing claim must be preserved: %+v", n.Claim)
	}
	if len(n.Inputs) != 0 {
		t.Fatalf("set-inputs must not write over a competing claim: %+v", n.Inputs)
	}
}

// TestSplitAnchorsChildrenInputs: split resolves and embeds each child's OWN
// declared inputs through the shared resolver.
func TestSplitAnchorsChildrenInputs(t *testing.T) {
	root, planDir := fixtureRoot(t)
	writeInputDoc(t, root, "docs/context.md", "# Context\n\n## Alpha\n\nalpha body\n")
	children := `{
  "version": 1,
  "nodes": [
    {"id": "big-parse", "contract": "parses", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "t1", "file": "t.ext", "satisfies": ["external-format"]}]},
     "hazards": ["external-format"],
     "inputs": [{"root": "repository", "path": "docs/context.md", "section": {"heading_path": ["Alpha"]}}]},
    {"id": "big-render", "contract": "renders", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "t2", "file": "t.ext"}]}, "hazards": []}
  ]
}
`
	if _, err := Split(root, root, "SamplePlan", "big", []byte(children)); err != nil {
		t.Fatalf("split: %v", err)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	parse := g.NodeByID("big-parse")
	if !strings.HasPrefix(parse.InputHashes[`{"root":"repository","path":"docs/context.md","section":{"heading_path":["Alpha"]}}`], "sha256:") {
		t.Fatalf("split must anchor the child's declared inputs: %+v", parse.InputHashes)
	}
	render := g.NodeByID("big-render")
	if len(render.InputHashes) != 0 {
		t.Fatalf("a child that declares no inputs must carry none: %+v", render.InputHashes)
	}
}
