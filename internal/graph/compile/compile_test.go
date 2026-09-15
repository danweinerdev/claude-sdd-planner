package compile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/intent"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// fixtureDecisionStatement is the one plan decision fixtureRoot records —
// content-addressed, so its id is derived via decisions.IDFor rather than
// hardcoded.
const fixtureDecisionStatement = "An accepted truth."

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
status: draft
created: 2026-08-01
updated: 2026-08-01
tags: []
related: [Specs/Sample, Designs/Sample]
phases: []
---

# Sample Plan

## Overview

A fixture plan.

## Non-Goals

None.

## Architecture

Simple.

## Key Decisions

None.

## Dependencies

None.

## Plan Completion Evidence

Pending — not complete.
`

// fixtureRoot builds a minimal planning root: spec + design + plan README +
// initialized graph. Returns the root (== repo root: planningRoot is ".").
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
	planDir := filepath.Join(root, "Plans", "SamplePlan")
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCompileRejectsAcceptanceWithoutFullReviewUpstream(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	p, err := model.DecodeProposal([]byte(strings.Replace(happyProposal, `"FR-01", "D-0001"`, `"FR-01"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	p.Nodes[3].Gate.Lanes = []string{model.ReviewLanes[0]}
	p.Nodes = append(p.Nodes,
		model.Node{ID: "accept", Role: model.RoleIntegrationAcceptance, Contract: "accept", Justifies: []string{"AC-01"}, Deps: []string{"feature-gate"}, Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1},
		model.Node{ID: "final-review", Role: model.RoleReview, Contract: "full review", Justifies: []string{"AC-01"}, Deps: []string{"accept"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1},
	)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	stage(t, root, string(raw))
	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.Where == "accept" && strings.Contains(f.Msg, "full review upstream") {
			return
		}
	}
	t.Fatalf("acceptance with only subset review upstream must be refused: %v", findings)
}

// recordFixtureDecision writes fixtureRoot's plan a decisions file with the
// one entry happyProposalCiting resolves against — kept separate from
// fixtureRoot itself so decisions_test.go's own SyncDesignDecisions
// assertions (which count entries the sync APPENDS) see an empty file to
// start from.
func recordFixtureDecision(t *testing.T, root string) {
	t.Helper()
	planDir := filepath.Join(root, "Plans", "SamplePlan")
	entries := []decisions.Entry{{ID: fixtureDecisionID(), Date: "2026-08-01", Statement: fixtureDecisionStatement}}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisions.PathFor(planDir), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureDecisionID is fixtureDecisionStatement's content-addressed id.
func fixtureDecisionID() string { return decisions.IDFor(fixtureDecisionStatement) }

func stage(t *testing.T, root, payload string) {
	t.Helper()
	if _, err := proposal.Stage(filepath.Join(root, "Plans", "SamplePlan"), []byte(payload)); err != nil {
		t.Fatalf("stage: %v", err)
	}
}

// happyProposal's citation "D-0001" is a literal marker, not a resolvable
// id: decisions_test.go (Designs/PlanDecisions' own tests) pattern-matches
// the exact substring `"justifies": ["FR-01", "D-0001"]` to substitute a
// real recorded plan-decision id via strings.Replace, so this text must stay
// byte-for-byte stable. Tests in THIS file that need it to actually resolve
// perform the same substitution themselves (see happyProposalCiting).
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

// happyProposalCiting returns happyProposal with its "D-0001" marker
// resolved to id — the same substitution decisions_test.go performs inline.
func happyProposalCiting(id string) string {
	return strings.Replace(happyProposal, `"justifies": ["FR-01", "D-0001"]`, `"justifies": ["FR-01", "`+id+`"]`, 1)
}

func TestCompileHappyPathEmbedsFingerprintsAndConsumes(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", "SamplePlan"))
	if _, err := gstore.Update(graphPath, func(g *model.Graph) error {
		g.RevisionLineage = map[string]string{"1111111111111111111111111111111111111111": "2222222222222222222222222222222222222222"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	recordFixtureDecision(t, root)
	stage(t, root, happyProposalCiting(fixtureDecisionID()))

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
	if g.RevisionLineage["1111111111111111111111111111111111111111"] != "2222222222222222222222222222222222222222" {
		t.Fatalf("compile discarded revision lineage: %+v", g.RevisionLineage)
	}
	implFR := g.NodeByID("impl-fr")
	if implFR == nil || implFR.IntentHashes["FR-01"] == "" ||
		!strings.HasPrefix(implFR.IntentHashes["FR-01"], "sha256:") {
		t.Fatalf("FR-01 fingerprint not embedded: %+v", implFR)
	}
	if h := implFR.IntentHashes[fixtureDecisionID()]; h == "" || !strings.HasPrefix(h, "sha256:") {
		t.Fatal("plan-decision citations resolve and are fingerprinted like any other requirement")
	}
	if g.NodeByID("impl-ac2").IntentHashes["DD-1"] == "" {
		t.Fatal("DD fingerprints must embed from the related design")
	}
	if _, err := os.Stat(res.Consumed); !os.IsNotExist(err) {
		t.Fatalf("the compiled proposal must be consumed: %s", res.Consumed)
	}

	// Views rendered alongside the graph: one phase doc (the nodes carry no
	// label, so they group as Ungrouped) plus the README projection.
	if len(res.Views) != 2 {
		t.Fatalf("expected 2 rendered views (phase doc + README), got %v", res.Views)
	}
	doc, err := os.ReadFile(filepath.Join(root, "Plans", "SamplePlan", "01-Ungrouped.md"))
	if err != nil {
		t.Fatalf("phase view missing: %v", err)
	}
	for _, want := range []string{
		"GENERATED VIEW", "type: phase", "plan: \"SamplePlan\"", "tasks: []",
		"### impl-fr", "- Gate: review — full", "Pending — not complete.",
	} {
		if !strings.Contains(string(doc), want) {
			t.Fatalf("phase view missing %q:\n%s", want, doc)
		}
	}
	readme, err := os.ReadFile(filepath.Join(root, "Plans", "SamplePlan", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Graph View", `doc: "01-Ungrouped.md"`, "## Non-Goals",
	} {
		if !strings.Contains(string(readme), want) {
			t.Fatalf("README projection missing %q:\n%s", want, readme)
		}
	}

	// Idempotence: re-rendering the unchanged graph — with the same derive
	// inputs the compile projected — writes nothing.
	g2, err := gstore.Load(res.GraphPath)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := identifierSources(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	st, closed := deriveClosure(root, sources, NewInputResolver(root, root))(g2)
	again, err := renderViews(root, "SamplePlan", "", g2, st, closed)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("re-render of an unchanged graph must be a no-op, rewrote %v", again)
	}
}

// TestCompileResolvesEvidenceRepoOnce (task 4): Run's preflight (over the
// preview graph) and its write-pass render (over the graph as written) must
// share ONE resolved evidence repo, not resolve (and so, per closed phase,
// potentially probe) it twice. detectVCS is the seam that proves resolution
// itself happens once, independent of revExistsMemoRepo's own per-revision
// memoization (which only proves stability WITHIN one resolved repo).
func TestCompileResolvesEvidenceRepoOnce(t *testing.T) {
	dir, rev := realGitRepo(t)
	root := fixtureRoot(t, fixtureSpec)

	// Pre-seed an already-closed phase (labeled "existing", distinct from
	// the proposal's own Ungrouped phase) whose checkpoint matches the real
	// git repo's HEAD, so the write-pass render actually renders a frozen
	// phase and so actually resolves+probes an evidence repo — proving the
	// count below is not vacuously zero.
	frHash := intent.Items(rules.CommentStripped(fixtureSpec))["FR-01"].Hash
	if frHash == "" {
		t.Fatal("fixture spec must define FR-01")
	}
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", "SamplePlan"))
	if _, err := gstore.Update(graphPath, func(g *model.Graph) error {
		g.Nodes = append(g.Nodes, model.Node{
			ID: "existing-work", Contract: "already done", Phase: "existing",
			Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "TestExistingWork", File: "existing_test.go"}}}, Hazards: model.Hazards{}, Estimate: 1,
			Justifies: []string{"FR-01"}, IntentHashes: map[string]string{"FR-01": frHash},
			Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean,
				Provenance: &model.Provenance{Kind: "git", Revision: rev}},
		}, model.Node{
			ID: "existing-review", Contract: "reviewed", Phase: "existing", Deps: []string{"existing-work"},
			Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1,
			Justifies: []string{"FR-01"}, IntentHashes: map[string]string{"FR-01": frHash},
			Verification: &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean,
				Provenance: &model.Provenance{Kind: "git", Revision: rev}},
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	recordFixtureDecision(t, root)
	stage(t, root, happyProposalCiting(fixtureDecisionID()))

	var calls int
	old := detectVCS
	detectVCS = func(root string) vcs.Repo {
		calls++
		return old(root)
	}
	t.Cleanup(func() { detectVCS = old })

	if _, findings, err := Run(root, dir, "SamplePlan"); err != nil || len(findings) != 0 {
		t.Fatalf("compile: %v %v", err, findings)
	}
	doc, err := os.ReadFile(filepath.Join(root, "Plans", "SamplePlan", "01-existing.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), frozenViewMarker) {
		t.Fatalf("fixture must render a frozen phase (the case that actually probes):\n%s", doc)
	}
	if calls != 1 {
		t.Fatalf("resolveEvidenceRepo resolved %d times, want 1 (shared across preflight and the write-pass render)", calls)
	}
}

// TestRenderRefusalLeavesGraphAndPayloadUntouched: a target file without the
// generated marker (a hand-authored or frozen v1 document) refuses the whole
// compile BEFORE the graph write.
func TestRenderRefusalLeavesGraphAndPayloadUntouched(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	recordFixtureDecision(t, root)
	planDir := filepath.Join(root, "Plans", "SamplePlan")
	if err := os.WriteFile(filepath.Join(planDir, "01-Ungrouped.md"),
		[]byte("---\ntitle: \"Hand-authored\"\ntype: phase\n---\n\n# Not a view\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stage(t, root, happyProposalCiting(fixtureDecisionID()))
	graphBefore, _ := os.ReadFile(gstore.PathFor(planDir))

	_, findings, err := Run(root, root, "SamplePlan")
	if err == nil || !strings.Contains(err.Error(), "not a generated view") {
		t.Fatalf("compile must refuse to overwrite a non-generated document: err=%v findings=%v", err, findings)
	}
	graphAfter, _ := os.ReadFile(gstore.PathFor(planDir))
	if string(graphBefore) != string(graphAfter) {
		t.Fatal("a view refusal must leave the graph untouched (preflight runs before the write)")
	}
	entries, _ := os.ReadDir(proposal.FragmentsDir(planDir))
	staged := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			staged++
		}
	}
	if staged != 1 {
		t.Fatal("a view refusal must leave the payload staged")
	}
}

var goldenDateRe = regexp.MustCompile(`(?m)^(created|updated): \d{4}-\d{2}-\d{2}$`)

func normalizeDates(b []byte) []byte {
	return goldenDateRe.ReplaceAll(b, []byte("$1: DATE"))
}

// TestGoldenTriple freezes the payload -> graph -> rendered-views pipeline:
// the filled template exemplar compiles into byte-stable goldens (dates
// normalized). Regenerate deliberately with UPDATE_GOLDENS=1. This is the
// frozen-golden pattern the plan names; the goldens live in this package's
// testdata rather than tools/regression because they freeze a pipeline, not
// a validator rule example — recorded as a deviation in the task notes.
func TestGoldenTriple(t *testing.T) {
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

	if _, findings, err := Run(root, root, "SamplePlan"); err != nil || len(findings) != 0 {
		t.Fatalf("compile: %v %v", err, findings)
	}

	planDir := filepath.Join(root, "Plans", "SamplePlan")
	outputs := map[string]string{
		"payload.json":  filled,
		"graph.json":    readAsString(t, gstore.PathFor(planDir)),
		"01-example.md": readAsString(t, filepath.Join(planDir, "01-example.md")),
		"README.md":     readAsString(t, filepath.Join(planDir, "README.md")),
	}
	goldenDir := filepath.Join("testdata", "golden")
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, content := range outputs {
			if err := os.WriteFile(filepath.Join(goldenDir, name),
				normalizeDates([]byte(content)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Log("goldens updated")
		return
	}
	for name, content := range outputs {
		want, err := os.ReadFile(filepath.Join(goldenDir, name))
		if err != nil {
			t.Fatalf("golden %s missing (regenerate with UPDATE_GOLDENS=1): %v", name, err)
		}
		got := normalizeDates([]byte(content))
		if string(got) != strings.ReplaceAll(string(want), "\r\n", "\n") {
			t.Errorf("golden %s drifted:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
		}
	}
}

func readAsString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestRenderedViewsValidateStructurally: the rendered plan scope carries
// zero Error-severity findings under the real validator.
func TestRenderedViewsValidateStructurally(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	recordFixtureDecision(t, root)
	stage(t, root, happyProposalCiting(fixtureDecisionID()))
	if _, findings, err := Run(root, root, "SamplePlan"); err != nil || len(findings) != 0 {
		t.Fatalf("compile: %v %v", err, findings)
	}
	loaded, err := rules.LoadRootRepo(root, root)
	if err != nil {
		t.Fatal(err)
	}
	loaded = rules.ScopeToPlan(loaded, rules.PlanRelOf("Plans/SamplePlan/README.md"))
	var errs []string
	for _, d := range rules.RunWithWaivers(loaded) {
		if d.Severity == rules.Error && strings.HasPrefix(d.Path, "Plans/SamplePlan") {
			errs = append(errs, fmt.Sprintf("%s %s:%d: %s", d.Code, d.Path, d.Line, d.Message))
		}
	}
	if len(errs) > 0 {
		t.Fatalf("rendered views must validate structurally clean:\n%s", strings.Join(errs, "\n"))
	}
}

// TestRewrapDoesNotChangeFingerprints is DD-4's contractual property at the
// compile level: a whitespace-only spec rewrap must embed the same hash.
func TestRewrapDoesNotChangeFingerprints(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	recordFixtureDecision(t, root)
	stage(t, root, happyProposalCiting(fixtureDecisionID()))
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
			return &model.Claim{By: by, LeaseExpires: "2099-01-01T00:00:00Z"}
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
    {"id": "bad-cites", "contract": "c", "justifies": ["AC-99", "pd-deadbeef"],
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
		`bad-cites: cites "AC-99", which resolves in no related spec, design`,
		`bad-cites: cites "pd-deadbeef", which no plan under the planning root records`,
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
	recordFixtureDecision(t, root)
	planDir := filepath.Join(root, "Plans", "SamplePlan")

	// Nothing staged: could-not-run, pointing at the authoring flow.
	_, _, err := Run(root, root, "SamplePlan")
	if err == nil || !strings.Contains(err.Error(), "sdd graph propose") {
		t.Fatalf("empty staging must point at propose: %v", err)
	}

	// Two fragments: points at assemble.
	stage(t, root, happyProposalCiting(fixtureDecisionID()))
	stage(t, root, `{"version": 1, "nodes": [{"id": "extra", "contract": "c", "justifies": ["AC-01"], "gate": {"type": "tests", "tests": [{"id":"TestExtra","file":"extra_test.go"}]}, "hazards": []}]}`)
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
	stage(t, root, strings.Replace(happyProposalCiting(fixtureDecisionID()),
		`"deps": ["impl-ac1", "impl-ac2"]`,
		`"deps": ["impl-ac1", "impl-ac2", "extra"]`, 1))
	stage(t, root, `{"version": 1, "nodes": [{"id": "extra", "contract": "c", "justifies": ["AC-01"], "gate": {"type": "tests", "tests": [{"id":"TestExtra","file":"extra_test.go"}]}, "hazards": []}]}`)
	if _, _, err := proposal.Assemble(planDir); err != nil {
		t.Fatal(err)
	}
	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil || len(findings) != 0 {
		t.Fatalf("assembled compile: %v %v", err, findings)
	}
}

func TestCompileRefusesEmptyTestsAndObservedOwnOutputs(t *testing.T) {
	t.Run("empty tests", func(t *testing.T) {
		root := fixtureRoot(t, fixtureSpec)
		stage(t, root, `{"version":1,"nodes":[{"id":"empty","contract":"empty","justifies":["AC-01"],"gate":{"type":"tests"},"hazards":[]}]}`)
		_, findings, err := Run(root, root, "SamplePlan")
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprint(findings); !strings.Contains(got, "declares no tests") {
			t.Fatalf("missing empty-gate finding: %v", findings)
		}
	})
	t.Run("live graph input", func(t *testing.T) {
		root := fixtureRoot(t, fixtureSpec)
		stage(t, root, `{"version":1,"nodes":[{"id":"observed","contract":"observed","justifies":["AC-01"],"inputs":[{"root":"planning","path":"Plans/SamplePlan/SamplePlan-Graph.json"}],"gate":{"type":"tests","evidence":"observed-v1","execution":{"adapter":"go-test-v1","timeout_seconds":30},"tests":[{"id":"TestObserved","file":"observed_test.go"}]},"hazards":[],"artifacts":["observed_test.go"]}]}`)
		_, findings, err := Run(root, root, "SamplePlan")
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprint(findings); !strings.Contains(got, "live graph") {
			t.Fatalf("missing own-output finding: %v", findings)
		}
	})
}

func TestObservedOwnOutputGuardUsesFilesystemIdentity(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	sources, err := identifierSources(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	planDir := filepath.Join(root, "Plans", "SamplePlan")
	graphPath := gstore.PathFor(planDir)

	t.Run("normal unrelated json input allowed", func(t *testing.T) {
		path := filepath.Join(root, "fixture.json")
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if observedOwnOutput(model.Input{Root: model.InputRootPlanning, Path: "fixture.json"}, sources) {
			t.Fatal("unrelated JSON was blanket-refused")
		}
	})

	t.Run("symlink alias", func(t *testing.T) {
		alias := filepath.Join(root, "graph-alias.json")
		if err := os.Symlink(graphPath, alias); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if !observedOwnOutput(model.Input{Root: model.InputRootPlanning, Path: "graph-alias.json"}, sources) {
			t.Fatal("physical alias of the live graph was accepted")
		}
	})

	t.Run("windows case alias", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("Windows case-alias behavior")
		}
		rel, err := filepath.Rel(root, graphPath)
		if err != nil {
			t.Fatal(err)
		}
		alias := strings.ToUpper(filepath.ToSlash(rel))
		if !observedOwnOutput(model.Input{Root: model.InputRootPlanning, Path: alias}, sources) {
			t.Fatalf("case alias of live graph was accepted: %s", alias)
		}
	})

	t.Run("pending evidence path under mapped target", func(t *testing.T) {
		mapped := filepath.Join(root, "mapped")
		sources.inputRepoRoot = mapped
		path := "Plans/SamplePlan/.graph/test-evidence/work/pending.json"
		if !observedOwnOutput(model.Input{Root: model.InputRootRepository, Path: path}, sources) {
			t.Fatal("normalized pending evidence path under mapped target was accepted")
		}
	})
}

// TestValidateFlagsMissingAndPartialFingerprints: the transition gate flags a
// stored node that cites a currently fingerprintable requirement with no
// embedded hash (missing entirely, or present-but-empty) and names the source
// plus the repair path — a plan-decision citation is no exception, since a
// recorded decision is just as fingerprintable as an AC/FR.
func TestValidateFlagsMissingAndPartialFingerprints(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	recordFixtureDecision(t, root)
	planDir := filepath.Join(root, "Plans", "SamplePlan")
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes,
			model.Node{ID: "missing", Contract: "c", Justifies: []string{"AC-01"},
				Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1},
			model.Node{ID: "partial", Contract: "c", Justifies: []string{"AC-01", "FR-01"},
				IntentHashes: map[string]string{"AC-01": "sha256:aaaa"},
				Gate:         model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1},
			model.Node{ID: "empty", Contract: "c", Justifies: []string{"AC-01"},
				IntentHashes: map[string]string{"AC-01": ""},
				Gate:         model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1},
			model.Node{ID: "pd-only", Contract: "c", Justifies: []string{fixtureDecisionID()},
				Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1},
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	findings, err := Validate(root, root, "SamplePlan", g)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, f := range findings {
		joined += f.String() + "\n"
	}
	for _, want := range []string{
		`missing: cites "AC-01" (defined in Specs/Sample/README.md) with no embedded intent fingerprint`,
		`partial: cites "FR-01" (defined in Specs/Sample/README.md) with no embedded intent fingerprint`,
		`empty: cites "AC-01" (defined in Specs/Sample/README.md) with no embedded intent fingerprint`,
		`pd-only: cites "` + fixtureDecisionID() + `" (defined in Plans/SamplePlan/SamplePlan-Decisions.json) with no embedded intent fingerprint`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing finding %q in:\n%s", want, joined)
		}
	}
	// The already-hashed citation must NOT be flagged.
	if strings.Contains(joined, `partial: cites "AC-01"`) {
		t.Errorf("a present hash must not be flagged:\n%s", joined)
	}
	// The finding names the supported repair path.
	if !strings.Contains(joined, "sdd graph repair-intent") {
		t.Errorf("the finding must name the repair path:\n%s", joined)
	}
}

func TestLaneAwareness(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	// One work node, one subset-lane checkpoint over it, one typo'd lane —
	// and NO full gate anywhere: the subset gate must not confer coverage.
	stage(t, root, `{
  "version": 1,
  "nodes": [
    {"id": "w", "contract": "work lands", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "t_w", "file": "f.ext"}]}, "hazards": []},
    {"id": "checkpoint", "contract": "light check holds", "justifies": ["AC-01"], "deps": ["w"],
     "gate": {"type": "review", "lanes": ["review_quality", "review_speling"]}, "hazards": []}
  ]
}
`)
	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("compile must refuse with findings, not fail: %v", err)
	}
	joined := ""
	for _, f := range findings {
		joined += f.String() + "\n"
	}
	for _, want := range []string{
		`checkpoint: names unknown review lane "review_speling"`,
		`w: covered by no full review gate`,
		`checkpoint: covered by no full review gate`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing finding %q in:\n%s", want, joined)
		}
	}
	// The lane vocabulary is named in the refusal so the fix is copyable.
	if !strings.Contains(joined, "review_blind_spots, review_plan_drift, review_quality, review_spec_compliance") {
		t.Errorf("the refusal must name the four lanes:\n%s", joined)
	}
}

// TestFrozenViewLifecycle: a view rendered while every node in it derives
// closed carries the frozen marker and status complete; byte-identical
// re-renders stay no-ops; a render that would CHANGE the frozen view is
// refused; deleting the file explicitly is the sanctioned escape.
func TestFrozenViewLifecycle(t *testing.T) {
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	readme := "---\ntitle: \"P\"\ntype: plan\nstatus: draft\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags: []\nrelated: []\nphases: []\n---\n\n# P\n"
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}

	pass := func(seq int) *model.Verification {
		return &model.Verification{Result: model.ResultPass, Seq: seq, Isolation: model.IsolationClean}
	}
	g := &model.Graph{Version: 1, SeqCounter: 2, Nodes: []model.Node{
		{ID: "a", Contract: "works", Gate: model.Gate{Type: model.GateTests},
			Hazards: model.Hazards{}, Estimate: 1, Verification: pass(1)},
		{ID: "g1", Contract: "reviewed", Deps: []string{"a"}, Gate: model.Gate{Type: model.GateReview},
			Hazards: model.Hazards{}, Estimate: 1, Verification: pass(2)},
	}}
	st := map[string]states.NodeState{
		"a":  {ID: "a", State: states.Green},
		"g1": {ID: "g1", State: states.Green},
	}
	closed := map[string]bool{"a": true, "g1": true}

	written, err := renderViews(root, "P", "", g, st, closed)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("expected phase doc + README, wrote %v", written)
	}
	doc, err := os.ReadFile(filepath.Join(planDir, "01-Ungrouped.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		frozenViewMarker, "status: complete",
		"- [x] Every node in this phase is truly closed",
		"**closed** — GREEN and covered by a passing frozen full review gate",
	} {
		if !strings.Contains(string(doc), want) {
			t.Fatalf("frozen view missing %q:\n%s", want, doc)
		}
	}

	// Byte-identical re-render: no-op, no refusal — compile stays runnable
	// on a completed plan.
	again, err := renderViews(root, "P", "", g, st, closed)
	if err != nil {
		t.Fatalf("identical re-render must not refuse: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("identical re-render must write nothing: %v", again)
	}

	// The graph moves under the frozen view (a contract edit, a demotion,
	// anything content-changing): the render is refused, naming the escape.
	g.Nodes[0].Contract = "works differently now"
	_, err = renderViews(root, "P", "", g, st, closed)
	if err == nil || !strings.Contains(err.Error(), "frozen view") {
		t.Fatalf("a content-changing render of a frozen view must refuse: %v", err)
	}

	// The sanctioned escape: delete the frozen view explicitly, recompile.
	if err := os.Remove(filepath.Join(planDir, "01-Ungrouped.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := renderViews(root, "P", "", g, st, closed); err != nil {
		t.Fatalf("after the explicit delete, the render proceeds: %v", err)
	}
}

// TestFrozenViewReopenedThenReclosedReRenders (task 1): a legitimately
// reopened phase (a review finding demoted a node, so its verification
// moved and the phase briefly derived open) that re-verifies and re-closes
// must re-render as a no-op-shaped write — not refuse with "delete the
// frozen view file explicitly". The only difference between the old frozen
// view and the new rendering is the per-node `- Observation: ...` line(s);
// everything else about the projection is unchanged, and the new rendering
// is itself frozen.
func TestFrozenViewReopenedThenReclosedReRenders(t *testing.T) {
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	readme := "---\ntitle: \"P\"\ntype: plan\nstatus: draft\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags: []\nrelated: []\nphases: []\n---\n\n# P\n"
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}

	pass := func(seq int) *model.Verification {
		return &model.Verification{Result: model.ResultPass, Seq: seq, Isolation: model.IsolationClean}
	}
	g := &model.Graph{Version: 1, SeqCounter: 2, Nodes: []model.Node{
		{ID: "a", Contract: "works", Gate: model.Gate{Type: model.GateTests},
			Hazards: model.Hazards{}, Estimate: 1, Verification: pass(1)},
		{ID: "g1", Contract: "reviewed", Deps: []string{"a"}, Gate: model.Gate{Type: model.GateReview},
			Hazards: model.Hazards{}, Estimate: 1, Verification: pass(2)},
	}}
	st := map[string]states.NodeState{
		"a":  {ID: "a", State: states.Green},
		"g1": {ID: "g1", State: states.Green},
	}
	closed := map[string]bool{"a": true, "g1": true}

	if _, err := renderViews(root, "P", "", g, st, closed); err != nil {
		t.Fatalf("initial close render: %v", err)
	}
	frozen := rendererRead(t, filepath.Join(planDir, "01-Ungrouped.md"))
	if !strings.Contains(frozen, frozenViewMarker) {
		t.Fatalf("fixture must render frozen: %s", frozen)
	}

	// Reopen: node "a" is demoted (a review finding), so it is no longer
	// closed — the phase genuinely opens. It must still refuse here, same
	// as before, because deleting the frozen view is the sanctioned way to
	// author the reopened content while the file on disk still claims the
	// old frozen history.
	reopenedClosed := map[string]bool{"g1": true} // "a" no longer closed
	if _, err := renderViews(root, "P", "", g, st, reopenedClosed); err == nil {
		t.Fatal("a genuinely reopened phase (a node no longer closed) must still refuse without deleting the frozen view")
	}
	if rendererRead(t, filepath.Join(planDir, "01-Ungrouped.md")) != frozen {
		t.Fatal("a genuine-reopen refusal must not rewrite the file")
	}

	// Reclose: node "a" re-verifies (a fresh observation, seq advances) and
	// the phase re-closes. The graph itself proves this is a legitimate
	// reclose (every node closed again); the only projected difference is
	// the Observation line. This must re-render, not refuse.
	g.Nodes[0].Verification = pass(3)
	st["a"] = states.NodeState{ID: "a", State: states.Green}
	reclosed := map[string]bool{"a": true, "g1": true}
	written, err := renderViews(root, "P", "", g, st, reclosed)
	if err != nil {
		t.Fatalf("a reopened-then-reclosed phase must re-render, not refuse: %v", err)
	}
	if len(written) == 0 {
		t.Fatal("expected the reclosed phase doc to be rewritten")
	}
	reclosedDoc := rendererRead(t, filepath.Join(planDir, "01-Ungrouped.md"))
	if !strings.Contains(reclosedDoc, frozenViewMarker) {
		t.Fatalf("reclosed re-render must still be frozen:\n%s", reclosedDoc)
	}
	if !strings.Contains(reclosedDoc, "at seq 3") {
		t.Fatalf("reclosed re-render must carry the fresh observation:\n%s", reclosedDoc)
	}

	// An unrelated hand edit to a frozen view (not shaped like a
	// reopen/reclose — a genuine content change alongside a fresh
	// observation) must still be refused: reclosedNoOtherChange must not
	// paper over a real change riding along with the seq bump.
	g.Nodes[0].Contract = "works differently now"
	g.Nodes[0].Verification = pass(4)
	if _, err := renderViews(root, "P", "", g, st, reclosed); err == nil {
		t.Fatal("a genuine content change alongside a reclose must still refuse")
	}
	if rendererRead(t, filepath.Join(planDir, "01-Ungrouped.md")) != reclosedDoc {
		t.Fatal("refused render must not rewrite the file")
	}
}

// TestFrozenViewUnrelatedHandEditStillRefuses (task 1): a frozen view edited
// by hand — no graph-side reopen/reclose signal at all, the graph is
// unchanged — must still refuse exactly as before; reclosedNoOtherChange
// must never authorize an edit the graph itself does not explain.
func TestFrozenViewUnrelatedHandEditStillRefuses(t *testing.T) {
	root, planDir := evidencePlanFixture(t)
	g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
	closed := map[string]bool{"work": true}
	if _, err := renderViews(root, "P", "", g, nil, closed); err != nil {
		t.Fatal(err)
	}
	phaseDoc := filepath.Join(planDir, "01-core.md")
	frozen := rendererRead(t, phaseDoc)

	hand := strings.Replace(frozen, "- Estimate: 1", "- Estimate: 99", 1)
	rendererWrite(t, phaseDoc, hand)

	// Re-render against the SAME (unchanged) graph: the on-disk file now
	// disagrees with the graph for a reason the graph cannot explain (no
	// node moved), so it must refuse.
	if _, err := renderViews(root, "P", "", g, nil, closed); err == nil {
		t.Fatal("an unrelated hand edit to a frozen view must still refuse")
	}
	if rendererRead(t, phaseDoc) != hand {
		t.Fatal("a refused render must not rewrite the hand-edited file")
	}
}

// TestReadmeHalfWrittenGraphViewSectionRepairs (task 2): a README carrying
// exactly one `graph-view:begin` marker and NO `graph-view:end` marker — the
// shape a previous (buggy) run left behind after a partial write — must be
// repaired in place: the span from the begin marker to the next depth<=2
// heading (or EOF) is replaced, and the result carries exactly one
// well-formed begin/end pair. The operator must not need to restore the
// README from git first.
func TestReadmeHalfWrittenGraphViewSectionRepairs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{{ID: "work", Contract: "works", Phase: "01-core", Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1}}}
	readme := filepath.Join(dir, "README.md")
	// Simulate a half-written section: a begin marker followed by stale
	// table content, but no end marker before the next heading — the shape
	// a previous (buggy) run left behind.
	half := graphViewBegin + "\n\n## Graph View\n\nstale content, no end marker\n"
	src := "---\ntitle: \"P\"\ntype: plan\nstatus: active\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags: []\nrelated: []\nphases: []\n---\n\n# P\n\n## Overview\n\nKeep identity prose exactly.\n\n" +
		half + "\n## Plan Completion Evidence\n\nPending — not complete.\n"
	rendererWrite(t, readme, src)

	out, changed, err := planReadmeUpdate(dir, "P", groupPhases(g, "P"), nil)
	if err != nil {
		t.Fatalf("half-written section must repair, not refuse: %v", err)
	}
	if !changed {
		t.Fatal("expected the half-written section to be rewritten")
	}
	if strings.Count(out, graphViewBegin) != 1 || strings.Count(out, graphViewEnd) != 1 {
		t.Fatalf("expected exactly one begin and one end marker after repair:\n%s", out)
	}
	if strings.Contains(out, "stale content, no end marker") {
		t.Fatalf("stale half-written content must be replaced:\n%s", out)
	}
	if !strings.Contains(out, "## Plan Completion Evidence\n\nPending — not complete.\n") {
		t.Fatalf("content after the section (next heading) must survive:\n%s", out)
	}

	// Idempotent: a second render against the repaired README is a
	// byte-identical no-op.
	rendererWrite(t, readme, out)
	out2, changed2, err := planReadmeUpdate(dir, "P", groupPhases(g, "P"), nil)
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if changed2 {
		t.Fatalf("second render must be idempotent:\n%s", out2)
	}
}

// TestReadmeMultipleBeginMarkersStillRefuses (task 2): two or more
// `graph-view:begin` markers is genuinely ambiguous (which span is the real
// section?) and must still refuse explicitly, naming the cause.
func TestReadmeMultipleBeginMarkersStillRefuses(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{{ID: "work", Contract: "works", Phase: "01-core", Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1}}}
	readme := filepath.Join(dir, "README.md")
	doubled := graphViewBegin + "\n\n## Graph View\n\nfirst\n\n" + graphViewEnd + "\n\n" +
		graphViewBegin + "\n\n## Graph View\n\nsecond\n\n" + graphViewEnd + "\n"
	src := "---\ntitle: \"P\"\ntype: plan\nstatus: active\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags: []\nrelated: []\nphases: []\n---\n\n# P\n\n## Overview\n\nKeep identity prose exactly.\n\n" + doubled
	rendererWrite(t, readme, src)

	_, _, err := planReadmeUpdate(dir, "P", groupPhases(g, "P"), nil)
	if err == nil {
		t.Fatal("multiple begin markers must be refused")
	}
	if !strings.Contains(err.Error(), "multiple begin markers") {
		t.Fatalf("refusal must name multiple begin markers: %v", err)
	}
}

// TestACCoverageScopedToDirectSpecs (Plans/SddGraph 5.5, filed from the
// self-hosting pilot's finding F-01): coverage is an exit code over the
// plan's OWN requirement surface (DD-4). A spec reachable only transitively
// — through a design's background citation — does not put its acceptance
// criteria on this plan's coverage demand; its ids remain citable and
// fingerprinted. Without the scoping, every graph plan in a multi-plan root
// is refused for ACs owned by other plans' completed specs.
func TestACCoverageScopedToDirectSpecs(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	// The design now cites a foreign spec (another plan's requirement
	// surface) with a live, unchecked AC and a citable FR.
	foreign := `---
title: "Foreign Spec"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# Foreign Spec

## Functional Requirements

- **FR-90**: Something another plan implements.

## Acceptance Criteria

- [ ] **AC-90**: The other plan's criterion.
`
	if err := os.WriteFile(filepath.Join(root, "Specs", "Foreign", "README.md"), nil, 0o644); err == nil {
		_ = os.Remove(filepath.Join(root, "Specs", "Foreign", "README.md"))
	}
	if err := os.MkdirAll(filepath.Join(root, "Specs", "Foreign"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Specs", "Foreign", "README.md"), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	design := `---
title: "Sample Design"
type: design
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [design]
related: [Specs/Foreign]
---

# Sample Design

## Design Decisions

- **DD-1**: Strict decoding.
  Context: silent drops. Decision: refuse unknown keys. Rationale: drift.
`
	if err := os.WriteFile(filepath.Join(root, "Designs", "Sample", "README.md"), []byte(design), 0o644); err != nil {
		t.Fatal(err)
	}

	// Payload covers the plan's OWN ACs; one node cites the foreign FR to
	// prove transitive ids stay citable.
	stage(t, root, `{
  "version": 1,
  "nodes": [
    {"id": "own-1", "contract": "own AC-01 satisfied", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "t1", "file": "f.ext"}]}, "hazards": []},
    {"id": "own-2", "contract": "own AC-02 satisfied", "justifies": ["AC-02", "FR-90"], "deps": ["own-1"],
     "gate": {"type": "tests", "tests": [{"id": "t2", "file": "f.ext"}]}, "hazards": []},
    {"id": "gate-final", "contract": "plan survives full review", "justifies": ["AC-01", "AC-02"], "deps": ["own-2"],
     "gate": {"type": "review", "lanes": "full"}, "hazards": []}
  ]
}
`)
	res, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	joined := ""
	for _, f := range findings {
		joined += f.String() + "\n"
	}
	if strings.Contains(joined, "AC-90") {
		t.Fatalf("a transitively-reachable spec's AC must not demand coverage here:\n%s", joined)
	}
	if strings.Contains(joined, `cites "FR-90"`) {
		t.Fatalf("transitive ids must stay citable:\n%s", joined)
	}
	if len(findings) != 0 {
		t.Fatalf("expected a clean compile, got:\n%s", joined)
	}
	// The foreign FR's fingerprint embedded — citability is not just
	// non-refusal, the intent hash rides along.
	g, err := gstore.Load(res.GraphPath)
	if err != nil {
		t.Fatal(err)
	}
	if g.NodeByID("own-2").IntentHashes["FR-90"] == "" {
		t.Fatal("the transitively-cited FR must be fingerprinted")
	}
}

// TestGraphViewSectionInsertsBeforeEvidence: the README's Plan Completion
// Evidence section is replaced wholesale by evidence writers, and its extent
// runs to the next depth<=2 heading — the graph-view BEGIN marker is a
// comment, not a heading, so a section appended after the evidence section
// gets its begin marker swallowed by the next evidence write (the exact
// defect repaired in cd95ec8). The renderer therefore inserts the section
// BEFORE the evidence heading; only a README without one appends at EOF.
func TestGraphViewSectionInsertsBeforeEvidence(t *testing.T) {
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	readme := "---\ntitle: \"P\"\ntype: plan\nstatus: active\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags: []\nrelated: []\nphases: []\n---\n\n# P\n\n## Overview\n\nProse.\n\n## Plan Completion Evidence\n\nPending — not complete.\n"
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{
		{ID: "a", Contract: "works", Gate: model.Gate{Type: model.GateTests},
			Hazards: model.Hazards{}, Estimate: 1},
	}}
	if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
		t.Fatalf("render: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	begin := strings.Index(s, "<!-- graph-view:begin")
	evidence := strings.Index(s, "## Plan Completion Evidence")
	if begin < 0 || evidence < 0 {
		t.Fatalf("both the section and the evidence heading must exist:\n%s", s)
	}
	if begin > evidence {
		t.Fatalf("the graph-view section must be inserted BEFORE the evidence section, not after it:\n%s", s)
	}
	// The upsert path replaces in place: a second render must not duplicate
	// or relocate the section.
	if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
		t.Fatalf("re-render: %v", err)
	}
	out2, _ := os.ReadFile(filepath.Join(planDir, "README.md"))
	if c := strings.Count(string(out2), "<!-- graph-view:begin"); c != 1 {
		t.Fatalf("exactly one graph-view section after re-render, got %d", c)
	}
}

// twoSpecRoot extends the fixture with a second related spec that shares id
// ranges with the first (FR-01, AC-01) — the cross-spec collision surface
// reported against 2.8.3.
func twoSpecRoot(t *testing.T) string {
	root := fixtureRoot(t, fixtureSpec)
	other := `---
title: "Other Spec"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# Other Spec

## Functional Requirements

- **FR-01**: The other plan's requirement one.

## Acceptance Criteria

- [ ] **AC-01**: The other spec's criterion one.
`
	if err := os.MkdirAll(filepath.Join(root, "Specs", "Other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Specs", "Other", "README.md"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := os.ReadFile(filepath.Join(root, "Plans", "SamplePlan", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(plan), "related: [Specs/Sample, Designs/Sample]",
		"related: [Specs/Sample, Specs/Other, Designs/Sample]", 1)
	if updated == string(plan) {
		t.Fatalf("fixture plan related line not found:\n%s", plan)
	}
	if err := os.WriteFile(filepath.Join(root, "Plans", "SamplePlan", "README.md"), []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestAmbiguousBareCitationRefused: a bare id defined by two related specs
// must refuse with the qualified spellings, never first-wins-resolve.
func TestAmbiguousBareCitationRefused(t *testing.T) {
	root := twoSpecRoot(t)
	stage(t, root, `{
  "version": 1,
  "nodes": [
    {"id": "w", "contract": "works", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "t", "file": "f.ext"}]}, "hazards": []}
  ]
}
`)
	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("compile must refuse with findings, not fail: %v", err)
	}
	joined := ""
	for _, f := range findings {
		joined += f.String() + "\n"
	}
	if !strings.Contains(joined, `cites "AC-01", which is defined by more than one related source`) {
		t.Fatalf("ambiguous bare citation must refuse:\n%s", joined)
	}
	if !strings.Contains(joined, "Specs/Other:AC-01") || !strings.Contains(joined, "Specs/Sample:AC-01") {
		t.Fatalf("the refusal must name the qualified spellings:\n%s", joined)
	}
}

// transitiveAmbiguityRoot reproduces the TestSuiteReliability shape (P-01):
// the plan relates ONLY Designs/A directly; Designs/A relates Specs/A and
// Designs/B; Specs/A and Designs/B both define names (FR-01, DD-1) that
// collide with what the plan's own direct source, Designs/A, defines. An
// unqualified citation must resolve against the plan's directly related
// source (Designs/A) rather than refuse as ambiguous against the
// transitively reached collision.
func transitiveAmbiguityRoot(t *testing.T) string {
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
	write("Specs/A/README.md", `---
title: "Spec A"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# Spec A

## Requirements

- **FR-01**: Spec A's requirement one.

## Acceptance Criteria

- [ ] **AC-01**: Spec A's criterion one.
`)
	write("Designs/B/README.md", `---
title: "Design B"
type: design
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [design]
related: []
---

# Design B

## Design Decisions

- **DD-1**: Design B's decision.
  Context: b context. Decision: b decision. Rationale: b rationale.
`)
	write("Designs/A/README.md", `---
title: "Design A"
type: design
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [design]
related: [Specs/A, Designs/B]
---

# Design A

## Design Decisions

- **DD-1**: Design A's own decision.
  Context: a context. Decision: a decision. Rationale: a rationale.

- **FR-01**: Design A also defines something that happens to share the
  FR-01 spelling with Spec A, discovered only transitively.
`)
	write("Plans/SamplePlan/README.md", `---
title: "Sample Plan"
type: plan
status: draft
created: 2026-08-01
updated: 2026-08-01
tags: []
related: [Designs/A]
phases: []
---

# Sample Plan

## Overview

A fixture plan.

## Non-Goals

None.

## Architecture

Simple.

## Key Decisions

None.

## Dependencies

None.

## Plan Completion Evidence

Pending — not complete.
`)
	planDir := filepath.Join(root, "Plans", "SamplePlan")
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestUnqualifiedCitationPrefersDirectSource: when the plan relates only one
// source directly, and that source's own transitive related graph reaches
// other sources that happen to define the same bare id NAME, an unqualified
// citation resolves against the direct source, not against the transitive
// collision — genuine ties among direct sources still refuse (P-01).
func TestUnqualifiedCitationPrefersDirectSource(t *testing.T) {
	root := transitiveAmbiguityRoot(t)
	stage(t, root, `{
  "version": 1,
  "nodes": [
    {"id": "w", "contract": "works", "justifies": ["FR-01", "DD-1"],
     "gate": {"type": "tests", "tests": [{"id": "t", "file": "f.ext"}]}, "hazards": []},
    {"id": "gate-final", "contract": "reviewed", "justifies": ["FR-01"], "deps": ["w"],
     "gate": {"type": "review", "lanes": "full"}, "hazards": []}
  ]
}
`)
	result, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if len(findings) > 0 {
		joined := ""
		for _, f := range findings {
			joined += f.String() + "\n"
		}
		t.Fatalf("unqualified citations resolvable against the plan's direct source must compile:\n%s", joined)
	}
	hashes := result.Hashes["w"]
	if hashes["FR-01"] == "" {
		t.Fatalf("FR-01 must embed an intent fingerprint (resolved to Designs/A): %v", hashes)
	}
	if hashes["DD-1"] == "" {
		t.Fatalf("DD-1 must embed an intent fingerprint (resolved to Designs/A): %v", hashes)
	}

	sources, err := identifierSources(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	// Both FR-01 and DD-1 are also defined by transitively related sources
	// (Specs/A and Designs/B respectively), but the plan directly relates
	// only Designs/A — the direct source must win.
	hit, _, ok := sources.resolveItem("FR-01")
	if !ok || hit.SourceRel != "Designs/A/README.md" {
		t.Fatalf("FR-01 must resolve to Designs/A (the plan's direct source), got %+v ok=%v", hit, ok)
	}
	hit, _, ok = sources.resolveItem("DD-1")
	if !ok || hit.SourceRel != "Designs/A/README.md" {
		t.Fatalf("DD-1 must resolve to Designs/A (the plan's direct source), got %+v ok=%v", hit, ok)
	}

	rep, err := Audit(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range rep.Coverage {
		if sc.Source != "Designs/A/README.md" {
			continue
		}
		for _, fc := range sc.Families {
			if (fc.Family == "FR" || fc.Family == "DD") && fc.Covered != 1 {
				t.Fatalf("audit coverage must agree the citation resolved to Designs/A: %+v", fc)
			}
		}
	}
}

// TestPerSpecACCoverageAndQualifiedCitations: qualified citations resolve,
// coverage is per spec (one spec's citation never satisfies the other
// spec's same-numbered criterion), and the qualified fingerprint embeds
// under the citation as written.
func TestPerSpecACCoverageAndQualifiedCitations(t *testing.T) {
	root := twoSpecRoot(t)
	stage(t, root, `{
  "version": 1,
  "nodes": [
    {"id": "w1", "contract": "sample ones", "justifies": ["Specs/Sample:AC-01", "Sample:FR-01"],
     "gate": {"type": "tests", "tests": [{"id": "t1", "file": "f.ext"}]}, "hazards": []},
    {"id": "w2", "contract": "sample twos", "justifies": ["AC-02"], "deps": ["w1"],
     "gate": {"type": "tests", "tests": [{"id": "t2", "file": "f.ext"}]}, "hazards": []},
    {"id": "gate-final", "contract": "reviewed", "justifies": ["Specs/Sample:AC-01"], "deps": ["w2"],
     "gate": {"type": "review", "lanes": "full"}, "hazards": []}
  ]
}
`)
	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("compile must refuse with findings, not fail: %v", err)
	}
	joined := ""
	for _, f := range findings {
		joined += f.String() + "\n"
	}
	if !strings.Contains(joined, "Specs/Other:AC-01 has no covering node") {
		t.Fatalf("the other spec's same-numbered criterion must stay uncovered:\n%s", joined)
	}
	if strings.Contains(joined, `cites "Specs/Sample:AC-01"`) || strings.Contains(joined, `cites "Sample:FR-01"`) {
		t.Fatalf("qualified citations must resolve:\n%s", joined)
	}

	// Cover the other spec explicitly: the compile greens and the
	// fingerprints embed under the citations as written.
	stage(t, root, `{
  "version": 1,
  "nodes": [
    {"id": "w3", "contract": "other ones", "justifies": ["Other:AC-01"], "deps": ["w2"],
     "gate": {"type": "tests", "tests": [{"id": "t3", "file": "f.ext"}]}, "hazards": []},
    {"id": "gate-2", "contract": "other reviewed", "justifies": ["Other:AC-01"], "deps": ["w3"],
     "gate": {"type": "review", "lanes": "full"}, "hazards": []}
  ]
}
`)
	// Re-stage the full set: the first refusal left everything staged, so
	// assemble both fragments into one proposal.
	if _, _, err := proposal.Assemble(filepath.Join(root, "Plans", "SamplePlan")); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	res, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected clean compile, got:\n%v", findings)
	}
	g, err := gstore.Load(res.GraphPath)
	if err != nil {
		t.Fatal(err)
	}
	w1 := g.NodeByID("w1")
	if !strings.HasPrefix(w1.IntentHashes["Specs/Sample:AC-01"], "sha256:") {
		t.Fatalf("qualified citation must fingerprint under its written spelling: %+v", w1.IntentHashes)
	}
	if !strings.HasPrefix(g.NodeByID("w3").IntentHashes["Other:AC-01"], "sha256:") {
		t.Fatalf("basename-qualified citation must fingerprint: %+v", g.NodeByID("w3").IntentHashes)
	}
}

// TestDesignDiscoveryIsDirectNotViaSpecBacklink pins the design-discovery
// policy behind the cross-spec design-reference report: a plan cites a
// design's DD ids only when the design is on the plan's own `related`
// chain. A spec's back-link to the design that realizes it is NOT a
// discovery hop — following it would let every spec's realizing design
// (and every spec those designs relate) silently widen the citation
// registry and the coverage demand. Representation A (design related
// directly) resolves qualified DDs across two overlapping specs;
// representation B (design reachable only through the spec's back-link)
// refuses with a hint naming the fix; an unknown DD and an unrelated
// design refuse without one; every refusal leaves the graph untouched.
func TestDesignDiscoveryIsDirectNotViaSpecBacklink(t *testing.T) {
	proposalJSON := `{
  "version": 1,
  "nodes": [
    {"id": "w1", "contract": "sample ones", "justifies": ["Specs/Sample:AC-01", "Sample:FR-01", "Designs/Sample:DD-1"],
     "gate": {"type": "tests", "tests": [{"id": "t1", "file": "f.ext"}]}, "hazards": []},
    {"id": "w2", "contract": "sample twos", "justifies": ["AC-02", "Sample:DD-1"], "deps": ["w1"],
     "gate": {"type": "tests", "tests": [{"id": "t2", "file": "f.ext"}]}, "hazards": []},
    {"id": "w3", "contract": "other ones", "justifies": ["Other:AC-01", "Other:FR-01"], "deps": ["w1"],
     "gate": {"type": "tests", "tests": [{"id": "t3", "file": "f.ext"}]}, "hazards": []},
    {"id": "gate-final", "contract": "reviewed", "justifies": ["Specs/Sample:AC-01"], "deps": ["w2", "w3"],
     "gate": {"type": "review", "lanes": "full"}, "hazards": []}
  ]
}
`
	rewritePlanRelated := func(t *testing.T, root, to string) {
		t.Helper()
		p := filepath.Join(root, "Plans", "SamplePlan", "README.md")
		plan, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		updated := strings.Replace(string(plan), "related: [Specs/Sample, Specs/Other, Designs/Sample]", to, 1)
		if updated == string(plan) {
			t.Fatalf("fixture related line not found:\n%s", plan)
		}
		if err := os.WriteFile(p, []byte(updated), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	compile := func(t *testing.T, root string) string {
		t.Helper()
		planDir := filepath.Join(root, "Plans", "SamplePlan")
		before, err := os.ReadFile(gstore.PathFor(planDir))
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
		if joined != "" {
			after, err := os.ReadFile(gstore.PathFor(planDir))
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("a refused compile must not touch the graph")
			}
		}
		return joined
	}

	t.Run("A: design related directly resolves qualified DDs", func(t *testing.T) {
		root := twoSpecRoot(t)
		stage(t, root, proposalJSON)
		if joined := compile(t, root); joined != "" {
			t.Fatalf("representation A must compile clean:\n%s", joined)
		}
	})

	t.Run("B: design reachable only via spec back-link refuses with hint", func(t *testing.T) {
		root := twoSpecRoot(t)
		// The spec back-links to its design; the plan no longer relates it.
		specPath := filepath.Join(root, "Specs", "Sample", "README.md")
		spec, err := os.ReadFile(specPath)
		if err != nil {
			t.Fatal(err)
		}
		linked := strings.Replace(string(spec), "related: []", "related: [Designs/Sample]", 1)
		if linked == string(spec) {
			t.Fatalf("fixture spec related line not found:\n%s", spec)
		}
		if err := os.WriteFile(specPath, []byte(linked), 0o644); err != nil {
			t.Fatal(err)
		}
		rewritePlanRelated(t, root, "related: [Specs/Sample, Specs/Other]")
		stage(t, root, proposalJSON)
		joined := compile(t, root)
		for _, want := range []string{
			`w1: cites "Designs/Sample:DD-1", which resolves in no related spec, design; Designs/Sample/README.md defines it but is not reachable through the plan's ` + "`related`" + ` graph — relate it directly from Plans/SamplePlan/README.md`,
			`w2: cites "Sample:DD-1", which resolves in no related spec, design; Designs/Sample/README.md defines it`,
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("missing %q in:\n%s", want, joined)
			}
		}
		// The spec citations still resolve: only the design is unreachable.
		for _, forbidden := range []string{`"Sample:FR-01"`, `"Other:FR-01"`, `"Specs/Sample:AC-01"`} {
			if strings.Contains(joined, forbidden) {
				t.Errorf("spec citation %s must still resolve:\n%s", forbidden, joined)
			}
		}
	})

	t.Run("unknown DD and undefined qualifier refuse without a hint", func(t *testing.T) {
		root := twoSpecRoot(t)
		bad := strings.Replace(proposalJSON, `"Designs/Sample:DD-1"`, `"Designs/Sample:DD-9"`, 1)
		bad = strings.Replace(bad, `"Sample:DD-1"`, `"Designs/Ghost:DD-1"`, 1)
		stage(t, root, bad)
		joined := compile(t, root)
		for _, want := range []string{
			`w1: cites "Designs/Sample:DD-9", which resolves in no related spec, design` + "\n",
			`w2: cites "Designs/Ghost:DD-1", which resolves in no related spec, design` + "\n",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("missing %q in:\n%s", want, joined)
			}
		}
	})
}
