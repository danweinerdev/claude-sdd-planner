package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func TestCLIResolvesAndAppliesGeneralDesignReviewWithoutLaneRows(t *testing.T) {
	root, planDir, frozen, _ := amendPathFixture(t)
	rel := "Plans/Demo/reviews/design-review.md"
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "---\n" +
		"title: \"Design Review\"\n" +
		"type: review\n" +
		"status: open\n" +
		"review_of: \"Plans/Demo/README.md\"\n" +
		"frozen: false\n" +
		"rev: \"" + frozen + "\"\n" +
		"verdict: Amend\n" +
		"review_mode: single-agent\n" +
		"lane_results: []\n" +
		"findings:\n" +
		"  - id: F-01\n" +
		"    severity: major\n" +
		"    title: \"design requires a stronger contract\"\n" +
		"    status: open\n" +
		"    action: revise\n" +
		"    nodes: [impl]\n" +
		"    revise:\n" +
		"      contract: \"does the work under the amended design\"\n" +
		"followups: []\n" +
		"---\n\n# Design Review\n\n### F-01 — design requires a stronger contract\n\nThe design review found a contract gap.\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	resolveOut, err := captureStdout(t, func() error {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"review", "resolve", rel})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("resolve general design review: %v\n%s", err, resolveOut)
	}
	resolved := readFile(t, path)
	if !strings.Contains(resolved, "\nstatus: resolved\n") || !strings.Contains(resolved, "\nfrozen: true\n") || !strings.Contains(resolved, "lane_results: []") {
		t.Fatalf("supported resolve must freeze the zero-lane graph review without inventing lane rows:\n%s", resolved)
	}

	previewOut, err := captureStdout(t, func() error {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"graph", "review", "--plan", "Demo", "--node", "gate", "--artifact", rel})
		return cmd.Execute()
	})
	if err == nil {
		t.Fatalf("graph review previews open findings as a refused mutation, got success:\n%s", previewOut)
	}
	if _, ok := err.(*refusedError); !ok || !strings.Contains(previewOut, "graph amend") {
		t.Fatalf("graph review must return the supported amendment preview: %T %v\n%s", err, err, previewOut)
	}

	dryOut, err := captureStdout(t, func() error {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"graph", "amend", "--plan", "Demo", "--node", "gate", "--from-review", rel, "--dry-run"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("direct graph amend dry run: %v\n%s", err, dryOut)
	}
	expectDigest, expectReportDigest := "", ""
	fields := strings.Fields(previewOut)
	for i, field := range fields {
		if i+1 >= len(fields) {
			continue
		}
		switch field {
		case "--expect-digest":
			expectDigest = fields[i+1]
		case "--expect-report-digest":
			expectReportDigest = fields[i+1]
		}
	}
	if expectDigest == "" || expectReportDigest == "" {
		t.Fatalf("graph review preview must print both publication fences:\n%s", previewOut)
	}

	applyOut, err := captureStdout(t, func() error {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"graph", "amend", "--plan", "Demo", "--node", "gate", "--from-review", rel,
			"--expect-digest", expectDigest, "--expect-report-digest", expectReportDigest})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("apply general design review amendment: %v\n%s", err, applyOut)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if g.NodeByID("impl").Contract != "does the work under the amended design" || g.NodeByID("impl").EffectiveContractRev() != 2 {
		t.Fatalf("CLI amendment did not revise the design contract: %+v", g.NodeByID("impl"))
	}
}
