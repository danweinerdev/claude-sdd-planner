package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func TestInputsUseMappedTargetRepository(t *testing.T) {
	root := fixtureRoot(t, specOneAC())
	target := t.TempDir()
	writeInputFile(t, root, "planning-config.json", fmt.Sprintf(`{"planMapping":{"SamplePlan":"target"},"repositories":{"target":{"path":%q}}}`, target))
	writeInputFile(t, root, "docs/context.md", "# Wrong repository\n")
	writeInputFile(t, target, "docs/context.md", "# Context\n## Alpha\nMapped target content.\n")
	stage(t, root, inputProposal)
	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil || len(findings) != 0 {
		t.Fatalf("mapped input did not resolve from target repository: %v %v", err, findings)
	}
	sources, err := NewSources(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := sources.InputResolver().Resolve(model.Input{Root: model.InputRootRepository, Path: "docs/context.md"})
	if err != nil || !strings.Contains(resolved.Text, "Mapped target content") {
		t.Fatalf("source snapshot disagrees with compile target: %+v %v", resolved, err)
	}
}

// specOneAC drops AC-02 so a minimal proposal covering AC-01 alone compiles.
func specOneAC() string {
	return strings.Replace(fixtureSpec, "- [ ] **AC-02**: An unknown key names itself in the refusal.\n", "", 1)
}

func writeInputFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const inputProposal = `{
  "version": 1,
  "nodes": [
    {"id": "impl-input", "contract": "reads context", "justifies": ["FR-01", "D-0001"],
     "gate": {"type": "tests", "tests": [{"id": "test_input", "file": "t.ext"}]},
     "hazards": [],
     "inputs": [
       {"root": "repository", "path": "docs/context.md"},
       {"root": "repository", "path": "docs/context.md", "section": {"heading_path": ["Alpha"]}}
     ]},
    {"id": "feature-gate", "contract": "feature survives a full validation cycle", "justifies": ["AC-01"],
     "deps": ["impl-input"], "gate": {"type": "review", "lanes": "full"}, "hazards": []}
  ]
}
`

// TestCompileAnchorsInputs: compile embeds a digest per declared input — a
// whole-file input and a section input key distinctly, both sha256-prefixed.
func TestCompileAnchorsInputs(t *testing.T) {
	root := fixtureRoot(t, specOneAC())
	writeInputFile(t, root, "docs/context.md", "# Context\n\n## Alpha\n\nalpha body\n\n## Beta\n\nbeta body\n")
	stage(t, root, inputProposal)

	res, findings, err := Run(root, root, "SamplePlan")
	if err != nil || len(findings) != 0 {
		t.Fatalf("compile: %v %v", err, findings)
	}
	g, err := gstore.Load(res.GraphPath)
	if err != nil {
		t.Fatal(err)
	}
	n := g.NodeByID("impl-input")
	wholeKey := `{"root":"repository","path":"docs/context.md"}`
	sectionKey := `{"root":"repository","path":"docs/context.md","section":{"heading_path":["Alpha"]}}`
	if !strings.HasPrefix(n.InputHashes[wholeKey], "sha256:") {
		t.Fatalf("whole-file input hash not embedded: %+v", n.InputHashes)
	}
	if !strings.HasPrefix(n.InputHashes[sectionKey], "sha256:") {
		t.Fatalf("section input hash not embedded: %+v", n.InputHashes)
	}
	if n.InputHashes[wholeKey] == n.InputHashes[sectionKey] {
		t.Fatalf("whole-file and section fingerprints must differ")
	}
}

// TestCompileRefusesUnresolvedInput: a declared input that does not resolve
// (missing file, ambiguous heading) is a finding, never a silent skip.
func TestCompileRefusesUnresolvedInput(t *testing.T) {
	root := fixtureRoot(t, specOneAC())
	// Missing file.
	stage(t, root, inputProposal)
	if _, findings, err := Run(root, root, "SamplePlan"); err != nil || len(findings) == 0 {
		t.Fatalf("a missing input must be a finding: err=%v findings=%v", err, findings)
	} else {
		joined := findingsToString(findings)
		if !strings.Contains(joined, "does not exist") {
			t.Fatalf("finding should name the missing input: %v", joined)
		}
	}
}

// TestInputSectionEditsVsUnrelatedEdits: editing the selected section changes
// its fingerprint (INPUT-STALE); editing an unrelated section leaves it
// unchanged — the property the staleness axis depends on.
func TestInputSectionEditsVsUnrelatedEdits(t *testing.T) {
	root := fixtureRoot(t, specOneAC())
	docPath := filepath.Join(root, "docs", "context.md")
	writeInputFile(t, root, "docs/context.md", "# Context\n\n## Alpha\n\nalpha body\n\n## Beta\n\nbeta body\n")

	inRes := NewInputResolver(root, root)
	spec := model.Input{Root: model.InputRootRepository, Path: "docs/context.md",
		Section: &model.InputSection{HeadingPath: []string{"Alpha"}}}

	before, err := inRes.Resolve(spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Edit the selected section's body.
	if err := os.WriteFile(docPath, []byte("# Context\n\n## Alpha\n\nALPHA CHANGED\n\n## Beta\n\nbeta body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterSection, err := NewInputResolver(root, root).Resolve(spec)
	if err != nil {
		t.Fatal(err)
	}
	if before.Digest == afterSection.Digest {
		t.Fatalf("an edit to the selected section must change its fingerprint")
	}

	// Edit an UNRELATED section: the Alpha fingerprint must not move.
	if err := os.WriteFile(docPath, []byte("# Context\n\n## Alpha\n\nALPHA CHANGED\n\n## Beta\n\nBETA CHANGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterUnrelated, err := NewInputResolver(root, root).Resolve(spec)
	if err != nil {
		t.Fatal(err)
	}
	if afterSection.Digest != afterUnrelated.Digest {
		t.Fatalf("an edit to an unrelated section must not change the selected section's fingerprint")
	}
}

// TestInputStaleDeriveWiring: a recorded-pass node whose declared input no
// longer matches its embedded fingerprint derives STALE with the input key
// named in InputStale.
func TestInputStaleDeriveWiring(t *testing.T) {
	root := fixtureRoot(t, specOneAC())
	writeInputFile(t, root, "docs/context.md", "# Context\n\n## Alpha\n\nalpha body\n")
	stage(t, root, inputProposal)
	res, findings, err := Run(root, root, "SamplePlan")
	if err != nil || len(findings) != 0 {
		t.Fatalf("compile: %v %v", err, findings)
	}
	g, err := gstore.Load(res.GraphPath)
	if err != nil {
		t.Fatal(err)
	}
	n := g.NodeByID("impl-input")
	n.Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}

	// Matching content: GREEN.
	derive := func() states.NodeState {
		return states.Derive(states.Inputs{Graph: g, CurrentInputHashes: NewInputResolver(root, root).GraphHashes(g)})[n.ID]
	}
	if s := derive(); s.State != states.Green {
		t.Fatalf("matching input must derive GREEN: %+v", s)
	}

	// Edit the whole file: both declared inputs drift, and the node is stale.
	// A fresh resolver each derive models a fresh command (the resolver
	// memoizes within one invocation, never across one).
	writeInputFile(t, root, "docs/context.md", "# Context\n\n## Alpha\n\nALPHA CHANGED\n")
	if s := derive(); s.State != states.Stale || len(s.InputStale) == 0 {
		t.Fatalf("edited input must derive STALE: %+v", s)
	}
}

func findingsToString(fs []Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.String() + "\n")
	}
	return b.String()
}
