package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// amendPathFixture builds a git-backed planning root with a graph plan: an
// implementation node "impl" (pre-observed GREEN) feeding a full review gate
// "gate" (pre-observed GREEN, closed). It returns the root, the review's
// frozen `<base>..<endpoint>` range, and the phase-doc-shaped artifact the
// scaffold flow reviews.
func amendPathFixture(t *testing.T) (root, planDir, frozen, phase string) {
	t.Helper()
	root = t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile(filepath.Join(root, "planning-config.json"),
		[]byte(`{"planningRoot": "."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	planDir = filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte(`---
title: "Demo"
type: plan
status: active
created: 2026-08-01
updated: 2026-08-01
tags: []
related: []
phases: []
---

# Demo
`), 0o644); err != nil {
		t.Fatal(err)
	}
	phase = filepath.Join(planDir, "01-First-Phase.md")
	if err := os.WriteFile(phase, []byte("---\ntitle: \"First Phase\"\ntype: phase\nstatus: in-progress\n---\n\n# First Phase\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	impl := model.Node{ID: "impl", Contract: "does the work", Phase: "01-First-Phase",
		Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_impl", File: "t.ext"}}},
		Hazards: model.Hazards{}, Estimate: 1,
		Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}}
	gate := model.Node{ID: "gate", Contract: "impl survives review", Phase: "01-First-Phase",
		Deps: []string{"impl"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1,
		Verification: &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean}}
	g := &model.Graph{Version: model.SchemaVersion, SeqCounter: 2, Nodes: []model.Node{impl, gate}}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}

	gitOK(t, root, "init", "-q")
	gitOK(t, root, "add", ".")
	gitOK(t, root, "commit", "-q", "-m", "base")
	base := gitOK(t, root, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "work.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOK(t, root, "add", ".")
	gitOK(t, root, "commit", "-q", "-m", "work")
	endpoint := gitOK(t, root, "rev-parse", "HEAD")
	frozen = base + ".." + endpoint
	return root, planDir, frozen, phase
}

// graphStatusJSON runs `graph status --json` and decodes the per-node lines.
func graphStatusJSON(t *testing.T) map[string]struct {
	State       string `json:"state"`
	Closed      bool   `json:"closed"`
	ContractRev int    `json:"contract_rev"`
} {
	t.Helper()
	out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"graph", "status", "--plan", "Demo", "--json"})
		return root.Execute()
	})
	if err != nil {
		t.Fatalf("graph status --json: %v\n%s", err, out)
	}
	var decoded struct {
		Nodes []struct {
			ID          string `json:"id"`
			State       string `json:"state"`
			Closed      bool   `json:"closed"`
			ContractRev int    `json:"contract_rev"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decoding graph status JSON: %v\n%s", err, out)
	}
	byID := map[string]struct {
		State       string `json:"state"`
		Closed      bool   `json:"closed"`
		ContractRev int    `json:"contract_rev"`
	}{}
	for _, n := range decoded.Nodes {
		byID[n.ID] = struct {
			State       string `json:"state"`
			Closed      bool   `json:"closed"`
			ContractRev int    `json:"contract_rev"`
		}{n.State, n.Closed, n.ContractRev}
	}
	return byID
}

// TestAmendPathNonPassingLaneResultsDriveATruthfulAmend is the end-to-end
// regression for the bug report: before this fix, a scaffolded review could
// only ever be resolved with every lane falsely claiming PASS/Aligned, so
// there was no truthful path from a non-passing four-lane review to a graph
// amendment. This exercises the full CLI path: scaffold a review, record all
// four lanes with `--result changes-required` and concrete evidence, apply
// an Amend verdict with one open `revise` finding naming a node inside the
// gate's closure, resolve (must freeze), `graph review` (must print the
// amend command without recording a pass), `graph amend --dry-run` (no graph
// change), then apply and assert the revised node owes a fresh red and the
// review gate is not GREEN. It also asserts the refusals named in the bug
// report: an Aligned verdict with a non-passing lane, an Amend review with
// placeholder evidence, and resolving a review with no frozen range.
func TestAmendPathNonPassingLaneResultsDriveATruthfulAmend(t *testing.T) {
	_, planDir, frozen, phase := amendPathFixture(t)

	scaffoldOut, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"review", "scaffold", phase, "--frozen", frozen, "--mode", "independent"})
		return root.Execute()
	})
	if err != nil {
		t.Fatalf("review scaffold: %v\n%s", err, scaffoldOut)
	}
	review := filepath.Join(planDir, "reviews", "01-demo-code-review-"+frozen[len(frozen)-40:][:7]+".md")
	if _, err := os.Stat(review); err != nil {
		t.Fatalf("expected scaffolded review at %s: %v", review, err)
	}

	// Record all four lanes with --result changes-required and concrete
	// evidence — the truthful non-passing token, never a false PASS/Aligned.
	for _, lane := range reviewLaneIDs() {
		out, err := captureStdout(t, func() error {
			root := newRootCmd()
			root.SetArgs([]string{"review", "evidence", "set", review,
				"--lane", lane, "--result", "changes-required",
				"--evidence", "Inspected impl's contract against the frozen diff; it ignores archived rows"})
			return root.Execute()
		})
		if err != nil {
			t.Fatalf("review evidence set %s: %v\n%s", lane, err, out)
		}
	}
	src := readFile(t, review)
	if strings.Count(src, "CHANGES/Amend") != 4 {
		t.Fatalf("expected all four lanes to carry CHANGES/Amend; got:\n%s", src)
	}
	if strings.Contains(src, "PASS/Aligned") {
		t.Fatalf("no lane should still claim PASS/Aligned:\n%s", src)
	}

	// Apply an Amend verdict with one open `revise` finding naming a node
	// inside the gate's closure ("impl").
	src = strings.Replace(src, "verdict: Aligned", "verdict: Amend", 1)
	src = strings.Replace(src, "findings: []",
		"findings:\n  - id: F-01\n    severity: major\n    title: \"impl ignores archived rows\"\n    status: open\n    action: revise\n    nodes: [impl]\n    revise:\n      contract: \"does the work, and excludes archived rows\"", 1)
	src += "\n### F-01 — impl ignores archived rows\n\nArchived rows leak into results.\n"
	if err := os.WriteFile(review, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// resolve must freeze the Amend review with truthful non-passing lanes.
	resolveOut, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"review", "resolve", review})
		return root.Execute()
	})
	if err != nil {
		t.Fatalf("review resolve: %v\n%s", err, resolveOut)
	}
	resolved := readFile(t, review)
	if !strings.Contains(resolved, "\nfrozen: true\n") || !strings.Contains(resolved, "\nstatus: resolved\n") || !strings.Contains(resolved, "\nverdict: Amend\n") {
		t.Fatalf("Amend resolve must freeze and resolve with truthful lane results:\n%s", resolved)
	}
	if strings.Contains(resolved, "PASS/Aligned") {
		t.Fatalf("resolved Amend review must not carry any false PASS/Aligned lane:\n%s", resolved)
	}

	reviewRel := "Plans/Demo/reviews/" + filepath.Base(review)

	// `graph review` must print the amend command without recording a pass.
	beforeStatus := graphStatusJSON(t)
	if beforeStatus["gate"].State != "GREEN" || !beforeStatus["gate"].Closed {
		t.Fatalf("gate must start GREEN and closed from the pre-observed fixture: %+v", beforeStatus["gate"])
	}
	graphReviewOut, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"graph", "review", "--plan", "Demo", "--node", "gate", "--artifact", reviewRel})
		return root.Execute()
	})
	if err == nil {
		t.Fatalf("graph review with open findings must refuse (nothing recorded), got success:\n%s", graphReviewOut)
	}
	if _, ok := err.(*refusedError); !ok {
		t.Fatalf("expected *refusedError, got %T: %v", err, err)
	}
	if !strings.Contains(graphReviewOut, "graph amend") {
		t.Fatalf("graph review must print the amend command:\n%s", graphReviewOut)
	}
	afterGraphReview := graphStatusJSON(t)
	if afterGraphReview["gate"].State != beforeStatus["gate"].State || afterGraphReview["gate"].ContractRev != beforeStatus["gate"].ContractRev {
		t.Fatalf("graph review with open findings must not record a pass: before=%+v after=%+v",
			beforeStatus["gate"], afterGraphReview["gate"])
	}

	// Extract expect-digest / expect-report-digest from the printed
	// `sdd graph amend ...` command line so `graph amend` can be driven the
	// same way an agent would.
	expectDigest, expectReportDigest := "", ""
	fields := strings.Fields(graphReviewOut)
	for i, f := range fields {
		switch f {
		case "--expect-digest":
			if i+1 < len(fields) {
				expectDigest = fields[i+1]
			}
		case "--expect-report-digest":
			if i+1 < len(fields) {
				expectReportDigest = fields[i+1]
			}
		}
	}
	if expectDigest == "" || expectReportDigest == "" {
		t.Fatalf("graph review must print both digests for `graph amend`:\n%s", graphReviewOut)
	}

	// `graph amend --dry-run` must not change the graph.
	graphPath := gstore.PathFor(planDir)
	beforeGraph, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	dryOut, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"graph", "amend", "--plan", "Demo", "--node", "gate",
			"--from-review", reviewRel, "--expect-digest", expectDigest,
			"--expect-report-digest", expectReportDigest, "--dry-run"})
		return root.Execute()
	})
	if err != nil {
		t.Fatalf("graph amend --dry-run: %v\n%s", err, dryOut)
	}
	afterDryGraph, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterDryGraph) != string(beforeGraph) {
		t.Fatalf("graph amend --dry-run must not write the graph")
	}

	// Apply the amendment.
	applyOut, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"graph", "amend", "--plan", "Demo", "--node", "gate",
			"--from-review", reviewRel, "--expect-digest", expectDigest,
			"--expect-report-digest", expectReportDigest})
		return root.Execute()
	})
	if err != nil {
		t.Fatalf("graph amend: %v\n%s", err, applyOut)
	}

	finalStatus := graphStatusJSON(t)
	if finalStatus["impl"].ContractRev <= beforeStatus["impl"].ContractRev {
		t.Fatalf("revised node impl's contract_rev must advance: before=%d after=%d",
			beforeStatus["impl"].ContractRev, finalStatus["impl"].ContractRev)
	}
	if finalStatus["impl"].State == "GREEN" {
		t.Fatalf("revised node impl must derive as owing a fresh red, not GREEN: %+v", finalStatus["impl"])
	}
	if finalStatus["gate"].State == "GREEN" || finalStatus["gate"].Closed {
		t.Fatalf("review gate must not be GREEN/closed after the amendment: %+v", finalStatus["gate"])
	}

	// --- Refusals -----------------------------------------------------

	// Aligned verdict with a non-passing lane is refused.
	t.Run("aligned verdict with non-passing lane refuses", func(t *testing.T) {
		_, planDir2, frozen2, phase2 := amendPathFixture(t)
		if out, err := captureStdout(t, func() error {
			root := newRootCmd()
			root.SetArgs([]string{"review", "scaffold", phase2, "--frozen", frozen2, "--mode", "independent"})
			return root.Execute()
		}); err != nil {
			t.Fatalf("review scaffold: %v\n%s", out, err)
		}
		review2 := filepath.Join(planDir2, "reviews", "01-demo-code-review-"+frozen2[len(frozen2)-40:][:7]+".md")
		for _, lane := range reviewLaneIDs() {
			opts := []string{"review", "evidence", "set", review2, "--lane", lane,
				"--evidence", "Inspected impl's contract against the frozen diff; no drift found"}
			if lane == reviewLaneIDs()[0] {
				opts = append(opts, "--result", "changes-required")
			} else {
				opts = append(opts, "--result", "pass")
			}
			if out, err := captureStdout(t, func() error {
				root := newRootCmd()
				root.SetArgs(opts)
				return root.Execute()
			}); err != nil {
				t.Fatalf("review evidence set %s: %v\n%s", lane, err, out)
			}
		}
		out, err := captureStdout(t, func() error {
			root := newRootCmd()
			root.SetArgs([]string{"review", "resolve", review2})
			return root.Execute()
		})
		if err == nil {
			t.Fatalf("Aligned verdict with a non-passing lane must refuse, got success:\n%s", out)
		}
		if !strings.Contains(err.Error(), "PASS/Aligned") {
			t.Fatalf("refusal must name the lane result requirement: %v", err)
		}
	})

	// Amend with placeholder evidence is refused regardless of result.
	t.Run("amend with placeholder evidence refuses", func(t *testing.T) {
		_, planDir3, frozen3, phase3 := amendPathFixture(t)
		if out, err := captureStdout(t, func() error {
			root := newRootCmd()
			root.SetArgs([]string{"review", "scaffold", phase3, "--frozen", frozen3, "--mode", "independent"})
			return root.Execute()
		}); err != nil {
			t.Fatalf("review scaffold: %v\n%s", out, err)
		}
		review3 := filepath.Join(planDir3, "reviews", "01-demo-code-review-"+frozen3[len(frozen3)-40:][:7]+".md")
		// Only set --result, never --evidence, so the scaffold placeholder
		// evidence text survives untouched.
		out, err := captureStdout(t, func() error {
			root := newRootCmd()
			root.SetArgs([]string{"review", "evidence", "set", review3,
				"--lane", reviewLaneIDs()[0], "--result", "changes-required",
				"--evidence", laneEvidencePlaceholder})
			return root.Execute()
		})
		if err == nil {
			t.Fatalf("placeholder evidence must be refused at write time regardless of --result, got success:\n%s", out)
		}
	})

	// resolve refuses a review with no frozen range.
	t.Run("resolve refuses an unfrozen range", func(t *testing.T) {
		root4 := t.TempDir()
		t.Chdir(root4)
		if err := os.WriteFile(filepath.Join(root4, "planning-config.json"),
			[]byte(`{"planningRoot": "."}`), 0o644); err != nil {
			t.Fatal(err)
		}
		review4 := filepath.Join(root4, "Plans", "Demo", "reviews", "01-demo-code-review.md")
		if err := os.MkdirAll(filepath.Dir(review4), 0o755); err != nil {
			t.Fatal(err)
		}
		// Hand-authored review meant to gate something (carries `frozen:`)
		// but with no reviewed range at all — the exact dead end the bug
		// report names.
		src := "---\ntitle: \"Gate review\"\ntype: review\nstatus: open\n" +
			"review_of: \"Plans/Demo/01-First-Phase.md\"\nfrozen: false\nverdict: Aligned\n" +
			"rev: \"\"\nreview_mode: single-agent\nlane_results: []\nfindings: []\nfollowups: []\n" +
			"---\n\n# Gate review\n\nBody.\n"
		if err := os.WriteFile(review4, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := captureStdout(t, func() error {
			root := newRootCmd()
			root.SetArgs([]string{"review", "resolve", review4})
			return root.Execute()
		})
		if err == nil {
			t.Fatalf("resolve on a review with no frozen range must refuse, got success:\n%s", out)
		}
		if !strings.Contains(err.Error(), "frozen") {
			t.Fatalf("refusal must name what is missing (a frozen range): %v", err)
		}
		resolved4 := readFile(t, review4)
		if strings.Contains(resolved4, "status: resolved") {
			t.Fatalf("a refused resolve must not have written status: resolved:\n%s", resolved4)
		}
	})
}
