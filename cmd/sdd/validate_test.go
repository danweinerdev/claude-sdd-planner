package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// writeConfig and writeArtifact are shared test fixtures for cmdValidate,
// which resolves its default --root through planning-config.json exactly
// like every other subcommand.
func writeConfig(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "planning-config.json"), []byte(`{"planningRoot":"."}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeArtifact(t *testing.T, root, relDir, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// captureStdout redirects os.Stdout for the duration of f and returns
// whatever it wrote.
func captureStdout(t *testing.T, f func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	ferr := f()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String(), ferr
}

const validSpec = `---
title: Sample Spec
type: spec
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
---

## Overview

Text.

## Goals

Text.

## Non-Goals

Text.

## Requirements

- **FR-01**: Does a thing.

## User Stories

Text.

## Acceptance Criteria

- [ ] **AC-01**: Verifies the thing.

## Constraints

- **NFR-01**: Is fast.

## Dependencies

None.

## Open Questions

None.
`

// TestValidate_MissingRequiredHeading exercises cmdValidate end to end
// through the rules registry: a spec missing `## Non-Goals` must surface as
// SDD020/error, exactly as scripts/sdd_validate.py reports it.
func TestValidate_MissingRequiredHeading(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	broken := ""
	for _, line := range strings.Split(validSpec, "\n") {
		if line == "## Non-Goals" {
			continue
		}
		broken += line + "\n"
	}
	writeArtifact(t, root, "Specs/Thing", "README.md", broken)

	out, err := captureStdout(t, func() error {
		return cmdValidate(validateOpts{Root: root, Format: "json"})
	})
	if _, ok := err.(*refusedError); !ok {
		t.Fatalf("cmdValidate: %v", err)
	}
	if !jsonHasDiag(t, out, "SDD020", "error") {
		t.Errorf("expected SDD020/error, got: %s", out)
	}
}

// TestValidate_DanglingCitation: an FR-shaped token cited from a non-spec
// artifact with no related spec defining it must emit SDD122 (a spec's own
// citations of its own ids are exempt in sdd_validate.py, so this has to use
// a Research artifact rather than a Spec to exercise the rule at all).
func TestValidate_DanglingCitation(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	content := replaceOnce(validResearchDoc, "## Context\n\nText.", "## Context\n\nSee FR-99 for details.")
	writeArtifact(t, root, "Research", "note.md", content)

	out, err := captureStdout(t, func() error {
		return cmdValidate(validateOpts{Root: root, Format: "json"})
	})
	if _, ok := err.(*refusedError); !ok {
		t.Fatalf("cmdValidate: %v", err)
	}
	if !jsonHasDiag(t, out, "SDD122", "error") {
		t.Errorf("expected SDD122/error for dangling citation, got: %s", out)
	}
}

const validResearchDoc = `---
title: Sample Research
type: research
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
---

## Context

Text.

## Findings

Text.

## Analysis

Text.

## Open Questions

None.
`

// TestValidate_DeterministicSort: the JSON diagnostics array must always come
// out ordered by (path, line, code), regardless of discovery order.
func TestValidate_DeterministicSort(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	bravo := replaceOnce(validSpec, "## Non-Goals\n\nText.", "")
	alpha := replaceOnce(validSpec, "## Constraints\n\n- **NFR-01**: Is fast.", "")
	writeArtifact(t, root, "Specs/Bravo", "README.md", bravo)
	writeArtifact(t, root, "Specs/Alpha", "README.md", alpha)

	out, err := captureStdout(t, func() error {
		return cmdValidate(validateOpts{Root: root, Format: "json"})
	})
	if _, ok := err.(*refusedError); !ok {
		t.Fatalf("cmdValidate: %v", err)
	}
	var res struct {
		Diagnostics []outDiagnostic `json:"diagnostics"`
	}
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", jerr, out)
	}
	if len(res.Diagnostics) < 2 {
		t.Fatalf("expected multiple diagnostics, got %d", len(res.Diagnostics))
	}
	if !sort.SliceIsSorted(res.Diagnostics, func(i, j int) bool {
		a, b := res.Diagnostics[i], res.Diagnostics[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Code < b.Code
	}) {
		t.Errorf("diagnostics not sorted by (path, line, code): %+v", res.Diagnostics)
	}
}

// TestValidate_ExitCodeMapping: 0 for a clean root, 1 (refusedError) when an
// error-severity diagnostic exists, and a plain error (mapped to exit 2 by
// main) when the operation could not run at all.
func TestValidate_ExitCodeMapping(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		root := t.TempDir()
		writeConfig(t, root)
		writeArtifact(t, root, "Specs/Thing", "README.md", validSpec)
		_, err := captureStdout(t, func() error {
			return cmdValidate(validateOpts{Root: root, Format: "json"})
		})
		if err != nil {
			t.Errorf("expected nil error for a clean root, got %v", err)
		}
	})

	t.Run("errors present", func(t *testing.T) {
		root := t.TempDir()
		writeConfig(t, root)
		broken := replaceOnce(validSpec, "## Non-Goals\n\nText.", "")
		writeArtifact(t, root, "Specs/Thing", "README.md", broken)
		_, err := captureStdout(t, func() error {
			return cmdValidate(validateOpts{Root: root, Format: "json"})
		})
		if _, ok := err.(*refusedError); !ok {
			t.Errorf("expected *refusedError, got %v (%T)", err, err)
		}
	})

	t.Run("could not run", func(t *testing.T) {
		_, err := captureStdout(t, func() error {
			return cmdValidate(validateOpts{Root: filepath.Join(t.TempDir(), "does-not-exist")})
		})
		if err == nil {
			t.Fatal("expected an error for a nonexistent root")
		}
		if _, ok := err.(*refusedError); ok {
			t.Errorf("a nonexistent root should be \"could not run\", not a refusal")
		}
	})
}

// TestValidate_TextFormat spot-checks the text-format summary and per-finding
// block shape against scripts/sdd_validate.py's main().
func TestValidate_TextFormat(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	broken := replaceOnce(validSpec, "## Non-Goals\n\nText.", "")
	writeArtifact(t, root, "Specs/Thing", "README.md", broken)

	out, err := captureStdout(t, func() error {
		return cmdValidate(validateOpts{Root: root})
	})
	if _, ok := err.(*refusedError); !ok {
		t.Fatalf("cmdValidate: %v", err)
	}
	if !contains(out, "Invalid: ") {
		t.Errorf("expected an Invalid summary line, got: %s", out)
	}
	if !contains(out, "ERROR SDD020 ") {
		t.Errorf("expected an ERROR SDD020 line, got: %s", out)
	}
	if !contains(out, "  Required correction: ") {
		t.Errorf("expected a correction line, got: %s", out)
	}
}

func jsonHasDiag(t *testing.T, out, code, severity string) bool {
	t.Helper()
	var res struct {
		Diagnostics []rules.Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	for _, d := range res.Diagnostics {
		if d.Code == code && string(d.Severity) == severity {
			return true
		}
	}
	return false
}

func replaceOnce(s, old, new string) string {
	idx := indexOfStr(s, old)
	if idx < 0 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}

func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func contains(s, sub string) bool {
	return indexOfStr(s, sub) >= 0
}

// SDD192 implicates every plan party to a cross-plan supersession conflict,
// so a plan-scoped validate on any of them reports it (reviewer finding 2).
func TestScopedSelectionKeepsImplicatedCrossPlanConflict(t *testing.T) {
	d := rules.Diagnostic{
		Code: "SDD192", Severity: rules.Error, Path: "Plans/P/P-Decisions.json", Line: 1,
		Implicated: []string{"Plans/P/P-Decisions.json", "Plans/P/README.md", "Plans/Q/Q-Decisions.json", "Plans/Q/README.md", "Plans/R/R-Decisions.json", "Plans/R/README.md"},
	}
	for _, plan := range []string{"P", "Q", "R"} {
		got := selectInScope([]rules.Diagnostic{d}, "Plans/"+plan, []string{"Plans/" + plan + "/README.md", "Plans/" + plan + "/" + plan + "-Decisions.json"})
		if len(got) != 1 {
			t.Fatalf("scope Plans/%s must keep the conflict: got %d", plan, len(got))
		}
	}
	if got := selectInScope([]rules.Diagnostic{d}, "Plans/Z", []string{"Plans/Z/README.md"}); len(got) != 0 {
		t.Fatal("an uninvolved plan must not see it")
	}
}

// completeButUnclosedFixture builds a planning root with one committed graph
// plan whose README already claims `status: complete`. When open is true,
// node "a" carries no observation, so the graph does not actually close —
// SDD199's disagreement case.
func completeButUnclosedFixture(t *testing.T, root string, open bool) {
	t.Helper()
	writeArtifact(t, root, "Plans/Demo", "README.md", strings.Replace(nextPlanReadme("complete", ""), "phases:\n\n", "phases: []\n", 1))
	a := model.Node{ID: "a", Contract: "does a", Phase: "01-core",
		Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_a", File: "t.ext"}}},
		Hazards: model.Hazards{}, Estimate: 1}
	if !open {
		a.Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}
	}
	reviewA := model.Node{ID: "review-a", Contract: "reviews a", Phase: "01-core",
		Deps: []string{"a"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1}
	if !open {
		reviewA.Verification = &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean}
	}
	g := &model.Graph{Version: model.SchemaVersion, Nodes: []model.Node{a, reviewA}}
	planDir := filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}
}

// TestValidate_GraphCompleteButUnclosed is SDD199: a graph plan whose
// README says `status: complete` while the graph is not actually closed
// must surface a diagnostic naming the unclosed node, and validate must
// stay clean when the graph agrees with the README.
func TestValidate_GraphCompleteButUnclosed(t *testing.T) {
	t.Run("unclosed", func(t *testing.T) {
		root := t.TempDir()
		writeConfig(t, root)
		completeButUnclosedFixture(t, root, true)
		out, err := captureStdout(t, func() error {
			return cmdValidate(validateOpts{Root: root, Format: "json"})
		})
		if _, ok := err.(*refusedError); !ok {
			t.Fatalf("expected *refusedError, got %v (%T)", err, err)
		}
		if !strings.Contains(out, `"SDD199"`) {
			t.Fatalf("expected SDD199 in output:\n%s", out)
		}
		if !strings.Contains(out, "never closed by its own recorded observations: a, review-a") {
			t.Fatalf("expected the open node named in the diagnostic:\n%s", out)
		}
	})

	t.Run("closed", func(t *testing.T) {
		root := t.TempDir()
		writeConfig(t, root)
		completeButUnclosedFixture(t, root, false)
		out, err := captureStdout(t, func() error {
			return cmdValidate(validateOpts{Root: root, Format: "json"})
		})
		if err != nil {
			t.Fatalf("expected nil error for a closed graph, got %v\n%s", err, out)
		}
		if strings.Contains(out, `"SDD199"`) {
			t.Fatalf("a closed graph must not report SDD199:\n%s", out)
		}
	})
}

// TestValidate_GraphCompleteObservationClosedButLiveDrifted is SDD200: a
// complete graph plan whose graph was closed by its own recorded
// observations, but whose declared artifact has since changed on disk (an
// unrelated later commit touching a closed node's file), must not report
// SDD199 — observation-only closure still holds — and must report a single
// SDD200 informational diagnostic naming the drifted node.
func TestValidate_GraphCompleteObservationClosedButLiveDrifted(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	writeArtifact(t, root, "Plans/Demo", "README.md", strings.Replace(nextPlanReadme("complete", ""), "phases:\n\n", "phases: []\n", 1))
	artifactPath := filepath.Join(root, "impl.txt")
	if err := os.WriteFile(artifactPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := model.Node{ID: "a", Contract: "does a", Phase: "01-core",
		Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_a", File: "t.ext"}}},
		Hazards: model.Hazards{}, Estimate: 1, Artifacts: []string{"impl.txt"},
		Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}}
	reviewA := model.Node{ID: "review-a", Contract: "reviews a", Phase: "01-core",
		Deps: []string{"a"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1,
		Verification: &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean}}
	g := &model.Graph{Version: model.SchemaVersion, Nodes: []model.Node{a, reviewA},
		CompletedAt: &model.CompletedAt{Revision: "deadbeef", Seq: 2}}
	planDir := filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}

	// Post-completion maintenance touches the closed node's artifact.
	if err := os.WriteFile(artifactPath, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return cmdValidate(validateOpts{Root: root, Format: "json"})
	})
	if err != nil {
		t.Fatalf("post-completion drift must not fail validate: %v\n%s", err, out)
	}
	if strings.Contains(out, `"SDD199"`) {
		t.Fatalf("observation-only closure holds; SDD199 must not fire:\n%s", out)
	}
	if strings.Contains(out, `"SDD200"`) {
		t.Fatalf("unrelated file edits do not affect graph state:\n%s", out)
	}
}

// TestValidate_GraphCompleteReviewNeverPassed is SDD199: a graph plan whose
// implementation node is GREEN but whose review gate was never observed
// (or observed non-passing) fails observation-only closure regardless of
// live-digest agreement, and must still report SDD199 Error.
func TestValidate_GraphCompleteReviewNeverPassed(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	writeArtifact(t, root, "Plans/Demo", "README.md", strings.Replace(nextPlanReadme("complete", ""), "phases:\n\n", "phases: []\n", 1))
	a := model.Node{ID: "a", Contract: "does a", Phase: "01-core",
		Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_a", File: "t.ext"}}},
		Hazards: model.Hazards{}, Estimate: 1,
		Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}}
	// review-a's gate was never observed: no Verification at all.
	reviewA := model.Node{ID: "review-a", Contract: "reviews a", Phase: "01-core",
		Deps: []string{"a"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1}
	g := &model.Graph{Version: model.SchemaVersion, Nodes: []model.Node{a, reviewA}}
	planDir := filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return cmdValidate(validateOpts{Root: root, Format: "json"})
	})
	if _, ok := err.(*refusedError); !ok {
		t.Fatalf("expected *refusedError, got %v (%T)", err, err)
	}
	if !strings.Contains(out, `"SDD199"`) {
		t.Fatalf("expected SDD199 for a never-passed review gate:\n%s", out)
	}
	if strings.Contains(out, `"SDD200"`) {
		t.Fatalf("SDD199 and SDD200 are mutually exclusive per plan:\n%s", out)
	}
}

func mustDigest(t *testing.T, content string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}
