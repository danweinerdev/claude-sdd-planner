// TestGraphReverify_* exercise `sdd graph reverify`'s exit-code contract: a
// node refused only because its declared tests never appeared in this run
// (the normal mid-walk state) must exit 0 and report "skipped: ...", while a
// genuine failure still exits non-zero.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func reverifyFixture(t *testing.T, nodes ...model.Node) string {
	t.Helper()
	root := chdirTemp(t)
	planDir := filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeArtifact(t, root, "Plans/Demo", "README.md", `---
title: "Demo"
type: plan
status: active
created: 2026-08-01
updated: 2026-08-01
tags: [x]
related: []
phases: []
---

# Demo
`)
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes, nodes...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func reverifyTestsNode(id string, testIDs ...string) model.Node {
	var tests []model.Test
	for _, id := range testIDs {
		tests = append(tests, model.Test{ID: id, File: "pkg_test.go"})
	}
	return model.Node{ID: id, Contract: "c", Gate: model.Gate{Type: model.GateTests, Tests: tests},
		Hazards: model.Hazards{}, Estimate: 1}
}

// TestGraphReverify_DeclaredTestsNeverRan_ExitsZero: a node whose declared
// tests simply were not reached by this run is reported "skipped:", not
// "refused:", and the command exits 0.
func TestGraphReverify_DeclaredTestsNeverRan_ExitsZero(t *testing.T) {
	unreached := reverifyTestsNode("unreached", "test_unreached")
	reverifyFixture(t, unreached)

	report := `<testsuite><testcase name="test_other"/></testsuite>`
	reportPath := filepath.Join(t.TempDir(), "r.xml")
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}

	c := graphReverifyCmd()
	c.SetArgs([]string{"--plan", "Demo", "--report", reportPath})
	out, err := captureStdout(t, func() error { return c.Execute() })
	if err != nil {
		t.Fatalf("reverify must exit 0 when the only outcome is declared-tests-never-ran: %v\n%s", err, out)
	}
	if !strings.Contains(out, "unreached") || !strings.Contains(out, "skipped:") {
		t.Fatalf("the unreached node must be reported as skipped, got: %q", out)
	}
	if strings.Contains(out, "0 refused") == false {
		t.Fatalf("the summary must report zero refusals, got: %q", out)
	}
}

// TestGraphReverify_GenuineRefusal_ExitsNonZero: a node whose declared
// tests DID run (a real red-before-green refusal, not mid-walk absence)
// still exits non-zero.
func TestGraphReverify_GenuineRefusal_ExitsNonZero(t *testing.T) {
	hazard := reverifyTestsNode("hazard", "test_hazard")
	hazard.Hazards = model.Hazards{"external-format"}
	hazard.Gate.Tests[0].Satisfies = []string{"external-format"}
	reverifyFixture(t, hazard)

	report := `<testsuite><testcase name="test_hazard"/></testsuite>`
	reportPath := filepath.Join(t.TempDir(), "r.xml")
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}

	c := graphReverifyCmd()
	c.SetArgs([]string{"--plan", "Demo", "--report", reportPath})
	out, err := captureStdout(t, func() error { return c.Execute() })
	if err == nil {
		t.Fatalf("a genuine red-before-green refusal must exit non-zero, got nil error; output: %q", out)
	}
	if !strings.Contains(out, "refused:") {
		t.Fatalf("the genuine refusal must be reported as refused, got: %q", out)
	}
}
