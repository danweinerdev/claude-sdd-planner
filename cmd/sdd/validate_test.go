package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "planning-config.json"), []byte(`{"planningRoot":"."}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// specDoc builds a minimally valid spec artifact, with overrides applied by
// section heading, so a test can break exactly one thing.
func specDoc(fm map[string]string, sections map[string]string) string {
	defFM := map[string]string{
		"title": `"Thing"`, "type": "spec", "status": "draft",
		"created": "2026-07-01", "updated": "2026-07-01",
		"tags": "[x]", "related": "[]",
	}
	for k, v := range fm {
		defFM[k] = v
	}
	fmOrder := []string{"title", "type", "status", "created", "updated", "tags", "related"}

	defSec := map[string]string{
		"## Overview":                     "A thing.",
		"## Goals":                        "- do it",
		"## Non-Goals":                    "- not that",
		"## Requirements":                 "",
		"### Functional Requirements":     "- **FR-01**: the thing works",
		"### Non-Functional Requirements": "- **NFR-01**: it is fast",
		"## User Stories":                 "- As a user, I want it.",
		"## Acceptance Criteria":          "- [ ] **AC-01**: it works",
		"## Constraints":                  "- none",
		"## Dependencies":                 "- none",
	}
	for k, v := range sections {
		defSec[k] = v
	}
	secOrder := []string{
		"## Overview", "## Goals", "## Non-Goals", "## Requirements",
		"### Functional Requirements", "### Non-Functional Requirements",
		"## User Stories", "## Acceptance Criteria", "## Constraints", "## Dependencies",
	}

	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range fmOrder {
		b.WriteString(fmt.Sprintf("%s: %s\n", k, defFM[k]))
	}
	b.WriteString("---\n\n# Thing\n\n")
	for _, h := range secOrder {
		if v, ok := defSec[h]; ok && v == "OMIT" {
			continue
		}
		b.WriteString(h + "\n" + defSec[h] + "\n\n")
	}
	return b.String()
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

// TestValidate_MissingRequiredHeading: a spec missing ## Non-Goals must emit
// SDD020 — this is one of the four codes whose meaning is pinned to the
// Python validator's actual output, so its severity and presence matter more
// than any of the VLD candidates.
func TestValidate_MissingRequiredHeading(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	writeArtifact(t, root, "Specs/Thing", "README.md",
		specDoc(nil, map[string]string{"## Non-Goals": "OMIT"}))

	arts, err := collectArtifacts(root)
	if err != nil {
		t.Fatal(err)
	}
	diags := runChecks(root, arts)
	if !hasDiag(diags, "SDD020", "error") {
		t.Errorf("expected SDD020/error, got %+v", diags)
	}
}

// TestValidate_UnresolvableRelated: a `related` entry naming a nonexistent
// artifact must emit SDD041.
func TestValidate_UnresolvableRelated(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	writeArtifact(t, root, "Specs/Thing", "README.md",
		specDoc(map[string]string{"related": "[Specs/Nonexistent]"}, nil))

	arts, err := collectArtifacts(root)
	if err != nil {
		t.Fatal(err)
	}
	diags := runChecks(root, arts)
	if !hasDiag(diags, "SDD041", "error") {
		t.Errorf("expected SDD041/error, got %+v", diags)
	}
}

func phaseDoc(planDir string, phaseNum int, taskID string) string {
	return fmt.Sprintf(`---
title: "Phase %d"
type: phase
status: planned
created: 2026-07-01
updated: 2026-07-01
plan: "%s"
phase: %d
deliverable: "something"
tasks:
  - id: "%s"
    title: "a task"
    status: planned
---

# Phase %d

## Overview
Overview.

## Acceptance Criteria
- it works

## Phase Completion Evidence
Pending — not complete.
`, phaseNum, planDir, phaseNum, taskID, phaseNum)
}

// TestValidate_BadTaskID: a task id that doesn't match <phase>.<digits>[a-z]?
// or that names a different phase than its own document must emit SDD064.
func TestValidate_BadTaskID(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	writeArtifact(t, root, "Plans/Thing", "01-First.md", phaseDoc("Thing", 1, "2.1"))

	arts, err := collectArtifacts(root)
	if err != nil {
		t.Fatal(err)
	}
	diags := runChecks(root, arts)
	if !hasDiag(diags, "SDD064", "error") {
		t.Errorf("expected SDD064/error for phase-mismatched task id, got %+v", diags)
	}
}

func TestValidate_BadTaskIDMalformed(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	writeArtifact(t, root, "Plans/Thing", "01-First.md", phaseDoc("Thing", 1, "not-an-id"))

	arts, err := collectArtifacts(root)
	if err != nil {
		t.Fatal(err)
	}
	diags := runChecks(root, arts)
	if !hasDiag(diags, "SDD064", "error") {
		t.Errorf("expected SDD064/error for malformed task id, got %+v", diags)
	}
}

// TestValidate_DanglingCitation: an FR/NFR/AC/D-NNNN-shaped token that
// resolves against no artifact's declared identifiers must emit SDD122.
func TestValidate_DanglingCitation(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	writeArtifact(t, root, "Specs/Thing", "README.md",
		specDoc(nil, map[string]string{"## Overview": "See FR-99 for details."}))

	arts, err := collectArtifacts(root)
	if err != nil {
		t.Fatal(err)
	}
	diags := runChecks(root, arts)
	if !hasDiag(diags, "SDD122", "error") {
		t.Errorf("expected SDD122/error for dangling citation, got %+v", diags)
	}
}

// TestValidate_IDGapIsCandidateNotError: FR-01 and FR-03 with no FR-02 is
// legal after a retirement, so it must be severity candidate, never error.
func TestValidate_IDGapIsCandidateNotError(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	writeArtifact(t, root, "Specs/Thing", "README.md",
		specDoc(nil, map[string]string{
			"### Functional Requirements": "- **FR-01**: first\n- **FR-03**: third",
		}))

	arts, err := collectArtifacts(root)
	if err != nil {
		t.Fatal(err)
	}
	diags := runChecks(root, arts)
	found := false
	for _, d := range diags {
		if d.Code == "VLD009" {
			found = true
			if d.Severity != "candidate" {
				t.Errorf("VLD009 severity = %q, want candidate", d.Severity)
			}
		}
		if d.Code == "VLD009" && d.Severity == "error" {
			t.Errorf("id gap must never be error severity")
		}
	}
	if !found {
		t.Errorf("expected a VLD009 gap diagnostic, got %+v", diags)
	}
}

func planReadme(phasesYAML string) string {
	return fmt.Sprintf(`---
title: "Thing"
type: plan
status: draft
created: 2026-07-01
updated: 2026-07-01
tags: [x]
related: []
phases:
%s
---

# Thing

## Overview
Overview.

## Non-Goals
None.

## Architecture
Architecture.

## Key Decisions
None.

## Dependencies
None.

## Plan Completion Evidence
Pending — not complete.
`, phasesYAML)
}

func minimalPhaseDoc(num int) string {
	return fmt.Sprintf(`---
title: "Phase %d"
type: phase
status: planned
created: 2026-07-01
updated: 2026-07-01
plan: "Thing"
phase: %d
deliverable: "something"
tasks:
  - id: "%d.1"
    title: "a task"
    status: planned
---

# Phase %d

## Overview
Overview.

## Acceptance Criteria
- it works

## Phase Completion Evidence
Pending — not complete.
`, num, num, num, num)
}

// TestValidate_DependencyCycle: two phases depending on each other must be
// caught as a cycle (VLD013), not silently accepted.
func TestValidate_DependencyCycle(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	phases := `  - id: 1
    title: "Phase 1"
    status: planned
    doc: "01-First.md"
    depends_on: [2]
  - id: 2
    title: "Phase 2"
    status: planned
    doc: "02-Second.md"
    depends_on: [1]`
	writeArtifact(t, root, "Plans/Thing", "README.md", planReadme(phases))
	writeArtifact(t, root, "Plans/Thing", "01-First.md", minimalPhaseDoc(1))
	writeArtifact(t, root, "Plans/Thing", "02-Second.md", minimalPhaseDoc(2))

	arts, err := collectArtifacts(root)
	if err != nil {
		t.Fatal(err)
	}
	diags := runChecks(root, arts)
	if !hasDiag(diags, "VLD013", "error") {
		t.Errorf("expected VLD013/error for a dependency cycle, got %+v", diags)
	}
}

func hasDiag(diags []Diagnostic, code, severity string) bool {
	for _, d := range diags {
		if d.Code == code && d.Severity == severity {
			return true
		}
	}
	return false
}

// TestValidate_DeterministicSort: the JSON diagnostics array must always come
// out ordered by (path, line, code), regardless of discovery order.
func TestValidate_DeterministicSort(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)
	// Two specs, each missing a different required heading and each carrying
	// a dangling citation, to produce several diagnostics across two paths.
	writeArtifact(t, root, "Specs/Bravo", "README.md",
		specDoc(nil, map[string]string{"## Non-Goals": "OMIT", "## Overview": "See FR-77."}))
	writeArtifact(t, root, "Specs/Alpha", "README.md",
		specDoc(nil, map[string]string{"## Constraints": "OMIT", "## Overview": "See FR-88."}))

	out, err := captureStdout(t, func() error {
		return cmdValidate([]string{"--root", root, "--format", "json"})
	})
	if err != nil {
		if _, ok := err.(*refusedError); !ok {
			t.Fatalf("cmdValidate: %v", err)
		}
	}
	var res struct {
		Diagnostics []Diagnostic `json:"diagnostics"`
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
		writeArtifact(t, root, "Specs/Thing", "README.md", specDoc(nil, nil))
		_, err := captureStdout(t, func() error {
			return cmdValidate([]string{"--root", root, "--format", "json"})
		})
		if err != nil {
			t.Errorf("expected nil error for a clean root, got %v", err)
		}
	})

	t.Run("errors present", func(t *testing.T) {
		root := t.TempDir()
		writeConfig(t, root)
		writeArtifact(t, root, "Specs/Thing", "README.md",
			specDoc(nil, map[string]string{"## Non-Goals": "OMIT"}))
		_, err := captureStdout(t, func() error {
			return cmdValidate([]string{"--root", root, "--format", "json"})
		})
		if _, ok := err.(*refusedError); !ok {
			t.Errorf("expected *refusedError, got %v (%T)", err, err)
		}
	})

	t.Run("could not run", func(t *testing.T) {
		_, err := captureStdout(t, func() error {
			return cmdValidate([]string{"--root", filepath.Join(t.TempDir(), "does-not-exist")})
		})
		if err == nil {
			t.Fatal("expected an error for a nonexistent root")
		}
		if _, ok := err.(*refusedError); ok {
			t.Errorf("a nonexistent root should be \"could not run\", not a refusal")
		}
	})
}
