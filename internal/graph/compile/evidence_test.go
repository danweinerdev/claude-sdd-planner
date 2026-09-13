package compile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

// evidenceReview writes a minimal, resolved/frozen/Aligned phase review
// artifact into planDir/reviews, targeting phaseDoc (plan-dir-relative) with
// the given rev.
func evidenceReview(t *testing.T, planDir, plan, phaseDoc, name, rev string) {
	t.Helper()
	dir := filepath.Join(planDir, "reviews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lane := func(l string) string {
		return "\n  - lane: " + l + "\n    result: PASS/Aligned\n    reviewed_identity: \"" + rev + "\"\n    evidence: Checked the diff for this scope directly.\n"
	}
	src := `---
title: "Phase review"
type: review
status: resolved
created: 2026-01-01
updated: 2026-01-01
tags: [review]
related: ["Plans/` + plan + `/` + phaseDoc + `"]
review_of: "Plans/` + plan + `/` + phaseDoc + `"
rev: "` + rev + `"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "1111111111111111111111111111111111111111"
review_mode: independent
lane_results:` + lane("review_plan_drift") + lane("review_quality") + lane("review_spec_compliance") + lane("review_blind_spots") + `
findings: []
followups: []
tags: []
related: []
---

## Findings

None.

## Resolution Log

None.
`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func evidencePlanFixture(t *testing.T) (root, planDir string) {
	t.Helper()
	root = t.TempDir()
	planDir = filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "---\ntitle: \"P\"\ntype: plan\nstatus: active\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags: []\nrelated: []\nphases: []\n---\n\n# P\n\n## Overview\n\nKeep identity prose exactly.\n\n## Plan Completion Evidence\n\nPending — not complete.\n"
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, planDir
}

const evidenceRev = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func closedNode(id, phase string) model.Node {
	return model.Node{
		ID: id, Contract: "works", Phase: phase,
		Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1,
		Verification: &model.Verification{
			Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean,
			Provenance: &model.Provenance{Kind: "git", Revision: evidenceRev},
		},
	}
}

// TestPhaseEvidenceCoveredByReview covers the case the renderer can fully
// satisfy: a closed phase whose checkpoint is covered by an on-disk
// resolved/frozen/Aligned phase review naming this exact phase doc.
func TestPhaseEvidenceCoveredByReview(t *testing.T) {
	root, planDir := evidencePlanFixture(t)
	g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
	evidenceReview(t, planDir, "P", "01-core.md", "review.md", evidenceRev)
	closed := map[string]bool{"work": true}

	if _, err := renderViews(root, "P", "", g, nil, closed); err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(planDir, "01-core.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(doc)

	for _, want := range []string{
		"- Verified: ",
		"- Repository: ",
		"- VCS: git\n",
		"- Revision / checkpoint: `" + evidenceRev + "`\n",
		"- Identity recheck: `git cat-file -e " + evidenceRev + "`",
		"### Completed task identities",
		"- Final aligned review: `Plans/P/reviews/review.md`; frozen: " + evidenceRev + "\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("phase evidence missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Pending — not complete.") {
		t.Fatalf("phase evidence still pending:\n%s", body)
	}

	readme, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	rbody := string(readme)
	if !strings.Contains(rbody, "### Completed phase identities") {
		t.Fatalf("README missing completed phase identities:\n%s", rbody)
	}
	if !strings.Contains(rbody, "- `1`: `"+evidenceRev+"`; review: `Plans/P/reviews/review.md`\n") {
		t.Fatalf("README missing phase identity entry:\n%s", rbody)
	}
}

// TestPhaseEvidenceWithoutCoveringReview covers the case the renderer cannot
// satisfy from rendered content alone: a closed phase with no on-disk review
// whose review_of names this exact phase doc. The renderer must still emit
// truthful derived evidence (never a fabricated Final aligned review line),
// and the README's plan-level evidence must stay untouched (Pending) since
// not every completed phase resolves a covering review.
func TestPhaseEvidenceWithoutCoveringReview(t *testing.T) {
	root, planDir := evidencePlanFixture(t)
	g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
	closed := map[string]bool{"work": true}

	if _, err := renderViews(root, "P", "", g, nil, closed); err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(planDir, "01-core.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(doc)
	if strings.Contains(body, "Final aligned review") {
		t.Fatalf("phase evidence fabricated a Final aligned review with no covering review artifact:\n%s", body)
	}
	if !strings.Contains(body, "- Revision / checkpoint: `"+evidenceRev+"`\n") {
		t.Fatalf("phase evidence missing derived revision:\n%s", body)
	}

	readme, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(readme), "### Completed phase identities") {
		t.Fatalf("README plan evidence completed without a covering review:\n%s", readme)
	}
}

// TestReadmePhaseEntryTitleMatchesDoc is SDD152's own invariant: the README
// phase entry's title tracks the phase doc's rendered (human) title, not a
// stale or raw phase-label spelling an earlier hand-authored README entry
// may have carried.
func TestReadmePhaseEntryTitleMatchesDoc(t *testing.T) {
	root, planDir := evidencePlanFixture(t)
	// Simulate a pre-existing README phase entry carrying the raw label
	// instead of the human title renderPhaseDoc/phaseTitle derive.
	src, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(src), "phases: []",
		"phases:\n  - id: 1\n    title: \"01-core\"\n    status: planned\n    doc: \"01-core.md\"", 1)
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	g := &model.Graph{Version: 1, Nodes: []model.Node{{ID: "work", Contract: "works", Phase: "01-core",
		Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1}}}
	// The phase doc does not exist yet on the first render (a brand new
	// phase never has a stale README entry to reconcile); a second render,
	// matching the real repro of a pre-existing generated phase doc against
	// a stale README-only title, is what exercises the title fix.
	if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), `title: "Core"`) {
		t.Fatalf("README phase title was not reconciled to the doc's human title:\n%s", readme)
	}
}
