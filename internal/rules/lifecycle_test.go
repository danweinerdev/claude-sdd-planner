package rules

import "testing"

const intentPhase = `---
title: Sample Phase
type: phase
status: %STATUS%
created: 2024-01-01
updated: %UPDATED%
plan: Sample
phase: "1"
deliverable: %DELIVERABLE%
tasks:
  - id: "1.1"
    title: First
    status: %TASKSTATUS%
---

## Overview

Text.

## 1.1: First

### Subtasks

- [%BOX%] Step.

### Notes

None.

### Completion Evidence

%EVIDENCE%

## Acceptance Criteria

- [%BOX%] Works.

## Phase Completion Evidence

%EVIDENCE%
`

func render(status, updated, deliverable, taskStatus, box, evidence string) string {
	s := intentPhase
	for from, to := range map[string]string{
		"%STATUS%": status, "%UPDATED%": updated, "%DELIVERABLE%": deliverable,
		"%TASKSTATUS%": taskStatus, "%BOX%": box, "%EVIDENCE%": evidence,
	} {
		s = replaceAll(s, from, to)
	}
	return s
}

func replaceAll(s, from, to string) string {
	for {
		next := replaceFirst(s, from, to)
		if next == s {
			return s
		}
		s = next
	}
}

// TestLifecycleNormalizationIgnoresBookkeeping: everything a phase legitimately
// accrues while being completed must normalize away, or SDD173 would report a
// changed intent every time a phase closed.
func TestLifecycleNormalizationIgnoresBookkeeping(t *testing.T) {
	before := render("in-progress", "2024-01-01", "A thing.", "planned", " ", "Pending — not complete.")
	after := render("complete", "2024-06-30", "A thing.", "complete", "x",
		"- Verified: 2024-06-30\n- Repository: .\n- VCS: git\n- Revision / checkpoint: abc\n- Identity recheck: matched")

	a, err := lifecycleNormalizedArtifact(before, "phase")
	if err != nil {
		t.Fatalf("before: %v", err)
	}
	b, err := lifecycleNormalizedArtifact(after, "phase")
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	if a != b {
		t.Errorf("lifecycle bookkeeping changed canonical intent:\n--- before ---\n%s\n--- after ---\n%s", a, b)
	}
}

// TestLifecycleNormalizationDetectsScopeChange: the counterpart — a rewritten
// deliverable is a real change of intent and must survive normalization.
func TestLifecycleNormalizationDetectsScopeChange(t *testing.T) {
	before := render("complete", "2024-01-01", "A thing.", "complete", "x", "Pending — not complete.")
	after := render("complete", "2024-01-01", "A DIFFERENT thing.", "complete", "x", "Pending — not complete.")

	a, _ := lifecycleNormalizedArtifact(before, "phase")
	b, _ := lifecycleNormalizedArtifact(after, "phase")
	if a == b {
		t.Error("a rewritten deliverable normalized away; SDD173 would miss a post-review scope change")
	}
}

// TestLifecycleNormalizationRefusesFlowStyle: a flow-style lifecycle node
// cannot be excised by source span, so normalization refuses rather than
// guessing. The caller reports an inability to compare, not an equality.
func TestLifecycleNormalizationRefusesFlowStyle(t *testing.T) {
	src := `---
title: T
type: phase
status: complete
created: 2024-01-01
updated: 2024-01-01
tasks: [{id: "1.1", status: complete}]
---

## Overview
`
	if _, err := lifecycleNormalizedArtifact(src, "phase"); err == nil {
		t.Error("expected a refusal for a flow-style lifecycle node")
	}
}

// TestPlaceholderLaneEvidenceIsRefused: `sdd review scaffold` writes an
// unfilled `<REPLACE: ...>` marker for each lane, and Python's
// useful_lane_evidence accepted it — it has enough non-generic words to pass
// the word-count heuristic. That would have let a phase close on a review
// nobody performed, which is the exact failure the four-lane gate exists to
// prevent. A deliberate divergence from the Python, tightening only.
func TestPlaceholderLaneEvidenceIsRefused(t *testing.T) {
	refused := []string{
		"<REPLACE: what this lane inspected and observed>",
		"<TODO fill in>",
		"checked <something> in store/atomic.go",
	}
	for _, s := range refused {
		if usefulLaneEvidence(s) {
			t.Errorf("%q was accepted as lane evidence; an unfilled placeholder is not an observation", s)
		}
	}
	accepted := []string{
		"Checked the migration ordering in store/atomic.go.",
		"Read cmd/sdd/apply.go and confirmed the digest guard rejects a stale write.",
	}
	for _, s := range accepted {
		if !usefulLaneEvidence(s) {
			t.Errorf("%q was refused; a concrete observation must be accepted", s)
		}
	}
}
