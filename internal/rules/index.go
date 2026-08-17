package rules

import (
	"regexp"
	"sort"
)

// Family (d): Validator._index — duplicate-identifier detection across a
// spec's own body, a plan's phase task ids, and the decision ledger.

var specDefinitionRe = map[string]*regexp.Regexp{
	"FR":  regexp.MustCompile(`(?m)^\s*-\s+\*\*(FR-\d{2,})\*\*\s*:`),
	"NFR": regexp.MustCompile(`(?m)^\s*-\s+\*\*(NFR-\d{2,})\*\*\s*:`),
	"AC":  regexp.MustCompile(`(?m)^\s*-\s+\[[ xX]\]\s+\*\*(AC-\d{2,})\*\*\s*:`),
}

// specDefinedIDs returns the id sets a spec artifact declares per family,
// deduplicated. Shared with the citations family (h), which needs the same
// index without re-deriving it per document.
func specDefinedIDs(spec *Artifact) map[string]map[string]bool {
	// Memoized on the artifact: SDD122 asks this for every related spec, of
	// every family, of every citing artifact, and the answer is a pure
	// function of the spec's body. On a root with a couple of hundred
	// artifacts the repeated full-body regex scans dominated `sdd validate`.
	// Artifacts are immutable for the lifetime of a Root, so one scan serves
	// every caller.
	if spec.definedIDs != nil {
		return spec.definedIDs
	}
	body := noComments(spec.Body)
	out := map[string]map[string]bool{}
	for _, family := range []string{"FR", "NFR", "AC"} {
		set := map[string]bool{}
		for _, m := range specDefinitionRe[family].FindAllStringSubmatch(body, -1) {
			set[m[1]] = true
		}
		out[family] = set
	}
	spec.definedIDs = out
	return out
}

// stringDuplicates returns the values that occur more than once in values,
// mirroring sdd_validate.py's duplicates().
func stringDuplicates(values []string) []string {
	seen := map[string]bool{}
	repeatedSet := map[string]bool{}
	for _, v := range values {
		if seen[v] {
			repeatedSet[v] = true
		}
		seen[v] = true
	}
	out := make([]string, 0, len(repeatedSet))
	for v := range repeatedSet {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func init() {
	Register(&Rule{
		Code: "SDD030", Severity: Error, PyFunc: "_index",
		What: "a spec defines the same FR/NFR/AC id more than once",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "spec" {
					continue
				}
				body := noComments(a.Body)
				for _, family := range []string{"FR", "NFR", "AC"} {
					var found []string
					for _, m := range specDefinitionRe[family].FindAllStringSubmatch(body, -1) {
						found = append(found, m[1])
					}
					for _, value := range stringDuplicates(found) {
						emit(Diagnostic{
							Code: "SDD030", Severity: Error, Path: a.Rel, Line: a.Line(value, true),
							Message:    "Duplicate `" + value + "` in its owning spec.",
							Correction: "Assign a new append-only id and update citations.",
						})
					}
				}
			}
		},
		Bad: []Example{{Name: "duplicate-fr", Files: map[string]string{
			"Specs/Sample/README.md": `---
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
- **FR-01**: Does it again.

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
`,
		}}},
		Good: []Example{{Name: "unique-ids", Files: map[string]string{
			"Specs/Sample/README.md": validSpecTemplate,
		}}},
	})

	Register(&Rule{
		Code: "SDD031", Severity: Error, PyFunc: "_index",
		What: "the same task id is declared by more than one phase in a plan",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			type key struct{ plan, id string }
			seen := map[key]bool{}
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "phase" {
					continue
				}
				tasks, ok := a.Meta["tasks"].([]any)
				if !ok {
					continue
				}
				planName, _ := a.Meta["plan"].(string)
				for _, t := range tasks {
					tm, ok := t.(map[string]any)
					if !ok {
						continue
					}
					id, ok := tm["id"].(string)
					if !ok {
						continue
					}
					k := key{planName, id}
					if seen[k] {
						emit(Diagnostic{
							Code: "SDD031", Severity: Error, Path: a.Rel, Line: 1,
							Message:    "Duplicate task id `" + id + "` in plan `" + planName + "`.",
							Correction: "Assign a unique append-only id within the plan and update references.",
						})
						continue
					}
					seen[k] = true
				}
			}
		},
		Bad: []Example{{Name: "duplicate-task-id", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
  - id: "1.1"
    title: Second
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
		Good: []Example{{Name: "unique-task-ids", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD032", Severity: Error, PyFunc: "_index",
		What: "the same decision id is declared more than once in the ledger",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			seen := map[string]bool{}
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "decision-log" {
					continue
				}
				entries, ok := a.Meta["decisions"].([]any)
				if !ok {
					continue
				}
				for _, e := range entries {
					em, ok := e.(map[string]any)
					if !ok {
						continue
					}
					id, ok := em["id"].(string)
					if !ok {
						continue
					}
					if seen[id] {
						emit(Diagnostic{
							Code: "SDD032", Severity: Error, Path: a.Rel, Line: 1,
							Message:    "Duplicate decision id `" + id + "`.",
							Correction: "Renumber the later entry and update all links.",
						})
						continue
					}
					seen[id] = true
				}
			}
		},
		Bad: []Example{{Name: "duplicate-decision-id", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    status: accepted
    question: Q1
    statement: S1
    scope: []
  - id: D-0001
    status: accepted
    question: Q2
    statement: S2
    scope: []
`),
		}}},
		Good: []Example{{Name: "unique-decision-ids", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    status: accepted
    question: Q1
    statement: S1
    scope: []
`),
		}}},
	})
}
