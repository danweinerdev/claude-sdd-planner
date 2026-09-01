package compile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

const fixtureSpec = `---
title: "Sample Spec"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# Sample Spec

## Requirements

- **FR-01**: The loader SHALL accept every documented key and reject an
  unknown key by name.

## Acceptance Criteria

- [ ] **AC-01**: A valid config loads with zero findings.
- [ ] **AC-02**: An unknown key names itself in the refusal.
`

const fixtureDesign = `---
title: "Sample Design"
type: design
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [design]
related: []
---

# Sample Design

## Design Decisions

- **DD-1**: Strict decoding.
  Context: silent drops. Decision: refuse unknown keys. Rationale: drift.
`

const fixturePlan = `---
title: "Sample Plan"
type: plan
status: active
created: 2026-08-01
updated: 2026-08-01
tags: []
related: [Specs/Sample, Designs/Sample]
phases: []
---

# Sample Plan
`

const fixtureDecisions = `---
title: "Decisions"
type: decision-log
status: active
created: 2026-08-01
updated: 2026-08-01
tags: []
related: []
decisions:
  - id: D-0001
    kind: decision
    status: accepted
    date: 2026-08-01
    decided_by: user
    statement: "An accepted truth."
    scope: []
  - id: D-0002
    kind: decision
    status: superseded
    date: 2026-08-01
    decided_by: user
    statement: "A retired truth."
    scope: []
---

# Decisions
`

// fixtureRoot builds a minimal planning root: spec + design + plan README +
// ledger + initialized graph. Returns the root (== repo root: planningRoot
// is ".").
func fixtureRoot(t *testing.T, spec string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("planning-config.json", `{"planningRoot": "."}`)
	write("Specs/Sample/README.md", spec)
	write("Designs/Sample/README.md", fixtureDesign)
	write("Plans/SamplePlan/README.md", fixturePlan)
	write("Decisions/decisions.md", fixtureDecisions)
	if _, err := gstore.Init(filepath.Join(root, "Plans", "SamplePlan")); err != nil {
		t.Fatal(err)
	}
	return root
}

func stage(t *testing.T, root, payload string) {
	t.Helper()
	if _, err := proposal.Stage(filepath.Join(root, "Plans", "SamplePlan"), []byte(payload)); err != nil {
		t.Fatalf("stage: %v", err)
	}
}

const happyProposal = `{
  "version": 1,
  "nodes": [
    {"id": "impl-fr", "contract": "loader built", "justifies": ["FR-01", "D-0001"],
     "gate": {"type": "tests", "tests": [{"id": "test_fr", "file": "t.ext"}]},
     "hazards": [], "artifacts": ["src/a.ext"]},
    {"id": "impl-ac1", "contract": "valid loads clean", "justifies": ["AC-01"], "deps": ["impl-fr"],
     "gate": {"type": "tests", "tests": [{"id": "test_ac1", "file": "t.ext"}]}, "hazards": []},
    {"id": "impl-ac2", "contract": "unknown key named", "justifies": ["AC-02", "DD-1"],
     "gate": {"type": "tests", "tests": [{"id": "test_ac2", "file": "t.ext"}]}, "hazards": []},
    {"id": "feature-gate", "contract": "feature survives a full validation cycle", "justifies": ["AC-01"],
     "deps": ["impl-ac1", "impl-ac2"], "gate": {"type": "review", "lanes": "full"}, "hazards": []}
  ]
}
`

func TestCompileHappyPathEmbedsFingerprintsAndConsumes(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	stage(t, root, happyProposal)

	res, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected zero findings, got:\n%v", findings)
	}
	if len(res.Added) != 4 {
		t.Fatalf("added = %v", res.Added)
	}
	g, err := gstore.Load(res.GraphPath)
	if err != nil {
		t.Fatal(err)
	}
	implFR := g.NodeByID("impl-fr")
	if implFR == nil || implFR.IntentHashes["FR-01"] == "" ||
		!strings.HasPrefix(implFR.IntentHashes["FR-01"], "sha256:") {
		t.Fatalf("FR-01 fingerprint not embedded: %+v", implFR)
	}
	if _, hashed := implFR.IntentHashes["D-0001"]; hashed {
		t.Fatal("ledger citations resolve but are not fingerprinted (the ledger has its own supersession machinery)")
	}
	if g.NodeByID("impl-ac2").IntentHashes["DD-1"] == "" {
		t.Fatal("DD fingerprints must embed from the related design")
	}
	if _, err := os.Stat(res.Consumed); !os.IsNotExist(err) {
		t.Fatalf("the compiled proposal must be consumed: %s", res.Consumed)
	}
}

// TestRewrapDoesNotChangeFingerprints is DD-4's contractual property at the
// compile level: a whitespace-only spec rewrap must embed the same hash.
func TestRewrapDoesNotChangeFingerprints(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	stage(t, root, happyProposal)
	if _, findings, err := Run(root, root, "SamplePlan"); err != nil || len(findings) != 0 {
		t.Fatalf("first compile: %v %v", err, findings)
	}
	g, _ := gstore.Load(gstore.PathFor(filepath.Join(root, "Plans", "SamplePlan")))
	first := g.NodeByID("impl-fr").IntentHashes["FR-01"]

	// Rewrap FR-01 (line break moves; wording identical), then compile a
	// second proposal citing it.
	rewrapped := strings.Replace(fixtureSpec,
		"SHALL accept every documented key and reject an\n  unknown key by name.",
		"SHALL accept every documented key and\n  reject an unknown key by name.", 1)
	if rewrapped == fixtureSpec {
		t.Fatal("fixture rewrap did not apply")
	}
	if err := os.WriteFile(filepath.Join(root, "Specs/Sample/README.md"), []byte(rewrapped), 0o644); err != nil {
		t.Fatal(err)
	}
	stage(t, root, `{
  "version": 1,
  "nodes": [
    {"id": "impl-fr-again", "contract": "more loader work", "justifies": ["FR-01"], "deps": ["impl-fr"],
     "gate": {"type": "tests", "tests": [{"id": "test_fr2", "file": "t.ext"}]}, "hazards": []},
    {"id": "gate-2", "contract": "second slice survives review", "justifies": ["AC-01"],
     "deps": ["impl-fr-again", "feature-gate"], "gate": {"type": "review", "lanes": "full"}, "hazards": []}
  ]
}
`)
	if _, findings, err := Run(root, root, "SamplePlan"); err != nil || len(findings) != 0 {
		t.Fatalf("second compile: %v %v", err, findings)
	}
	g, _ = gstore.Load(gstore.PathFor(filepath.Join(root, "Plans", "SamplePlan")))
	second := g.NodeByID("impl-fr-again").IntentHashes["FR-01"]
	if first != second {
		t.Fatalf("a rewrap-only spec edit must not change the fingerprint: %s vs %s", first, second)
	}
}

// TestCompileBatchesEveryFinding is the one-pass contract: a deliberately
// broken proposal reports ALL of its violations together, writes nothing,
// and consumes nothing.
func TestCompileBatchesEveryFinding(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	planDir := filepath.Join(root, "Plans", "SamplePlan")

	// Two claimed master nodes sharing an artifact (claims are tool-owned,
	// so they enter through the store, never a payload).
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		claim := func(by string) *model.Claim {
			return &model.Claim{By: by, LeaseExpires: "2026-09-01T00:00:00Z"}
		}
		g.Nodes = append(g.Nodes,
			model.Node{ID: "claimed-a", Contract: "c", Justifies: []string{"AC-01"},
				Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1,
				Artifacts: []string{"src/shared.ext"}, Claim: claim("agent-1")},
			model.Node{ID: "claimed-b", Contract: "c", Justifies: []string{"AC-01"},
				Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1,
				Artifacts: []string{"src/shared.ext"}, Claim: claim("agent-2")})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	stage(t, root, `{
  "version": 1,
  "nodes": [
    {"id": "dup-node", "contract": "first", "justifies": ["AC-01"], "gate": {"type": "tests"}, "hazards": []},
    {"id": "dup-node", "contract": "second", "justifies": ["AC-01"], "gate": {"type": "tests"}, "hazards": []},
    {"id": "dangler", "contract": "c", "justifies": ["AC-01"], "deps": ["ghost"],
     "gate": {"type": "tests"}, "hazards": "untriaged"},
    {"id": "cyc-a", "contract": "c", "justifies": ["AC-01"], "deps": ["cyc-b"], "gate": {"type": "tests"}, "hazards": []},
    {"id": "cyc-b", "contract": "c", "justifies": ["AC-01"], "deps": ["cyc-a"], "gate": {"type": "tests"}, "hazards": []},
    {"id": "bad-hazards", "contract": "c", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "t1", "file": "f.ext", "satisfies": ["order-sensitive"]}]},
     "hazards": ["race-condition", "external-format"]},
    {"id": "no-source", "contract": "c", "gate": {"type": "tests"}, "hazards": []},
    {"id": "bad-cites", "contract": "c", "justifies": ["AC-99", "D-0002", "D-0099"],
     "gate": {"type": "tests"}, "hazards": []}
  ]
}
`)
	graphBefore, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}

	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("compile must refuse with findings, not fail: %v", err)
	}
	joined := ""
	for _, f := range findings {
		joined += f.String() + "\n"
	}
	for _, want := range []string{
		`dup-node: declared more than once in this proposal`,
		`dangler: deps on "ghost", which no node declares`,
		`dangler: hazards are untriaged`,
		`graph: dependency cycle: cyc-a -> cyc-b -> cyc-a`,
		`"race-condition" is not a hazard in the closed vocabulary`,
		`test "t1" satisfies "order-sensitive", which the node does not declare`,
		`hazard "external-format" is discharged by no test`,
		`no-source: cites nothing`,
		`bad-cites: cites "AC-99", which resolves in no related spec, design, or decision ledger`,
		`bad-cites: cites decision D-0002 with status "superseded"`,
		`bad-cites: cites "D-0099", which resolves in no related spec`,
		`graph: AC-02 has no covering node`,
		`covered by no full review gate`,
		`graph: claimed-artifact overlap: src/shared.ext is claimed by claimed-a and claimed-b`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing finding %q in:\n%s", want, joined)
		}
	}

	// Refusal is atomic: graph unchanged, proposal still staged.
	graphAfter, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(graphBefore) != string(graphAfter) {
		t.Fatal("a refused compile must not touch the graph")
	}
	entries, _ := os.ReadDir(proposal.FragmentsDir(planDir))
	staged := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			staged++
		}
	}
	if staged != 1 {
		t.Fatalf("a refused compile must not consume the staged payload; %d fragments remain", staged)
	}
}

// TestFilledExemplarCompilesClean is the round-trip gate's compile
// extension (task 1.3's TODO): the template exemplar, filled the way an
// authoring session fills it — placeholder ids replaced, the untriaged
// sentinel resolved (its contract says REPLACE in capitals) — compiles with
// zero findings.
func TestFilledExemplarCompilesClean(t *testing.T) {
	spec := strings.Replace(fixtureSpec, "- [ ] **AC-02**: An unknown key names itself in the refusal.\n", "", 1)
	root := fixtureRoot(t, spec)

	raw, err := proposal.ExemplarJSON()
	if err != nil {
		t.Fatal(err)
	}
	filled := string(raw)
	filled = strings.ReplaceAll(filled, "AC-NN", "AC-01")
	filled = strings.ReplaceAll(filled, "FR-NN", "FR-01")
	filled = strings.ReplaceAll(filled, "DD-N", "DD-1")
	filled = strings.ReplaceAll(filled, `"untriaged"`, "[]")
	stage(t, root, filled)

	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("the filled exemplar must compile clean, got:\n%v", findings)
	}
}

func TestCompileInputSelection(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	planDir := filepath.Join(root, "Plans", "SamplePlan")

	// Nothing staged: could-not-run, pointing at the authoring flow.
	_, _, err := Run(root, root, "SamplePlan")
	if err == nil || !strings.Contains(err.Error(), "sdd graph propose") {
		t.Fatalf("empty staging must point at propose: %v", err)
	}

	// Two fragments: points at assemble.
	stage(t, root, happyProposal)
	stage(t, root, `{"version": 1, "nodes": [{"id": "extra", "contract": "c", "justifies": ["AC-01"], "gate": {"type": "tests"}, "hazards": []}]}`)
	_, _, err = Run(root, root, "SamplePlan")
	if err == nil || !strings.Contains(err.Error(), "sdd graph assemble") {
		t.Fatalf("multiple fragments must point at assemble: %v", err)
	}

	// Assembled: compiles the merged set. (The extra node hangs off the
	// gate so coverage holds.)
	entries, _ := os.ReadDir(proposal.FragmentsDir(planDir))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			os.Remove(filepath.Join(proposal.FragmentsDir(planDir), e.Name()))
		}
	}
	stage(t, root, strings.Replace(happyProposal,
		`"deps": ["impl-ac1", "impl-ac2"]`,
		`"deps": ["impl-ac1", "impl-ac2", "extra"]`, 1))
	stage(t, root, `{"version": 1, "nodes": [{"id": "extra", "contract": "c", "justifies": ["AC-01"], "gate": {"type": "tests"}, "hazards": []}]}`)
	if _, _, err := proposal.Assemble(planDir); err != nil {
		t.Fatal(err)
	}
	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil || len(findings) != 0 {
		t.Fatalf("assembled compile: %v %v", err, findings)
	}
}
