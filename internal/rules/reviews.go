package rules

import "regexp"

// Family (i cont'd): Validator._review's finding/follow-up shape checks not
// yet ported by specific.go's SDD080/081/082 — SDD086 (a finding's `status`
// is not an allowed value), SDD087 (a finding has no `### F-NN` body
// section), SDD088 (a terminal finding has no Resolution Log entry), and
// SDD095 (a follow-up has no `tracked_in`, i.e. is floating). SDD083-085,
// SDD089-094, SDD096-098 are out of scope for this pass.

var findingStatusValues = map[string]bool{"open": true, "fixed": true, "deferred": true, "rejected": true, "answered": true}

func findingHeadingRe(id string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^###\s+` + regexp.QuoteMeta(id) + `\b`)
}

func resolutionEntryRe(id string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^###\s+` + regexp.QuoteMeta(id) + `\s+—\s+([a-z-]+)\b`)
}

// reviewWithBlocks returns a minimal, structurally valid review document
// whose `findings:`/`followups:` frontmatter are the caller-supplied raw YAML
// block sequences (as decisionLog's decisionsBlock is), with caller-supplied
// Findings/Resolution Log section bodies.
func reviewWithBlocks(findingsBlock, followupsBlock, findingsBody, resolutionBody string) string {
	if findingsBlock == "" {
		findingsBlock = " []"
	}
	if followupsBlock == "" {
		followupsBlock = " []"
	}
	if findingsBody == "" {
		findingsBody = "None.\n"
	}
	if resolutionBody == "" {
		resolutionBody = "None.\n"
	}
	return `---
title: Sample Review
type: review
status: open
created: 2024-01-01
updated: 2024-01-01
review_of: "Specs/Sample/README.md"
rev: 1
findings:` + findingsBlock + `
followups:` + followupsBlock + `
---

## Findings

` + findingsBody + `
## Resolution Log

` + resolutionBody + `
`
}

func init() {
	Register(&Rule{
		Code: "SDD086", Severity: Error, PyFunc: "_review",
		What: "a review finding's `status` is not an allowed value",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			findings, ok := a.Meta["findings"].([]any)
			if !ok {
				return
			}
			for _, f := range findings {
				m := planEntry(f)
				if m == nil {
					continue
				}
				status, isStr := m["status"].(string)
				if isStr && findingStatusValues[status] {
					continue
				}
				display := status
				if !isStr {
					display = "None"
				}
				emit(Diagnostic{
					Code: "SDD086", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Finding `" + metaStr(m, "id") + "` has invalid status `" + display + "`.",
					Correction: "Use an allowed finding status.",
				})
			}
		},
		Bad: []Example{{Name: "invalid-status", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: Something\n    status: bogus\n", "", "", ""),
		}}},
		Good: []Example{{Name: "valid-status", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: Something\n    status: open\n", "", "", ""),
		}}},
	})

	Register(&Rule{
		Code: "SDD087", Severity: Error, PyFunc: "_review",
		What: "a review finding has no `### F-NN` body section",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			findings, ok := a.Meta["findings"].([]any)
			if !ok {
				return
			}
			for _, f := range findings {
				m := planEntry(f)
				if m == nil {
					continue
				}
				id := metaStr(m, "id")
				if findingHeadingRe(id).MatchString(a.Body) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD087", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Finding `" + id + "` has no body section.",
					Correction: "Add `### " + id + " — ...`.",
				})
			}
		},
		Bad: []Example{{Name: "missing-body-section", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: Something\n    status: open\n", "", "", ""),
		}}},
		Good: []Example{{Name: "has-body-section", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: Something\n    status: open\n", "",
				"### F-01 — Something\n\nDetail.\n", ""),
		}}},
	})

	Register(&Rule{
		Code: "SDD088", Severity: Error, PyFunc: "_review",
		What: "a terminal (non-open) review finding has no Resolution Log entry",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			findings, ok := a.Meta["findings"].([]any)
			if !ok {
				return
			}
			resolution := sections(a, 2)["Resolution Log"].Body
			for _, f := range findings {
				m := planEntry(f)
				if m == nil {
					continue
				}
				status, _ := m["status"].(string)
				if status == "open" {
					continue
				}
				id := metaStr(m, "id")
				if resolutionEntryRe(id).MatchString(resolution) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD088", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Terminal finding `" + id + "` has no resolution entry.",
					Correction: "Append a dated Resolution Log entry.",
				})
			}
		},
		Bad: []Example{{Name: "missing-resolution-entry", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: Something\n    status: fixed\n", "",
				"### F-01 — Something\n\nDetail.\n", ""),
		}}},
		Good: []Example{{Name: "has-resolution-entry", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: Something\n    status: fixed\n", "",
				"### F-01 — Something\n\nDetail.\n",
				"### F-01 — fixed\n\n2024-01-02: Fixed it.\n"),
		}}},
	})

	Register(&Rule{
		Code: "SDD095", Severity: Error, PyFunc: "_review",
		What: "a review follow-up has no `tracked_in`, i.e. is floating",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			followups, ok := a.Meta["followups"].([]any)
			if !ok {
				return
			}
			for _, f := range followups {
				m := planEntry(f)
				if m == nil {
					continue
				}
				if !isEmptyMeta(m["tracked_in"]) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD095", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Follow-up `" + metaStr(m, "id") + "` is floating.",
					Correction: "Create a plan task and set `tracked_in`.",
				})
			}
		},
		Bad: []Example{{Name: "floating-followup", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks("", "\n  - id: FU-01\n    finding: F-01\n    summary: Do it later.\n    tracked_in: \"\"\n", "", ""),
		}}},
		Good: []Example{{Name: "tracked-followup", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks("", "\n  - id: FU-01\n    finding: F-01\n    summary: Do it later.\n    tracked_in: \"1.1\"\n", "", ""),
		}}},
	})
}
