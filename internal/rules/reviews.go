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

// findingIDPattern and followupIDPattern mirror the Python regexes: findings
// are `F-NN`, follow-ups `FU-NN`, both with at least two digits.
var followupIDPattern = regexp.MustCompile(`^FU-\d{2,}$`)

// reviewFindings returns a review's findings[] entries that are mappings, and
// the set of finding ids it declares. The id set is what SDD094 checks a
// follow-up's `finding` against.
func reviewFindings(a *Artifact) (entries []map[string]any, ids map[string]bool) {
	ids = map[string]bool{}
	raw, ok := a.Meta["findings"].([]any)
	if !ok {
		return nil, ids
	}
	for _, f := range raw {
		m := planEntry(f)
		if m == nil {
			continue
		}
		entries = append(entries, m)
		ids[metaStr(m, "id")] = true
	}
	return entries, ids
}

// duplicateValues ports duplicates(): the values appearing more than once.
// Python returns a set, whose iteration order is arbitrary; this returns them
// sorted so the emitted diagnostics are deterministic. Order does not affect
// parity, which keys diagnostics by (code, path, line).
func duplicateValues(values []string) []string {
	seen, repeated := map[string]bool{}, map[string]bool{}
	for _, v := range values {
		if seen[v] {
			repeated[v] = true
		}
		seen[v] = true
	}
	out := make([]string, 0, len(repeated))
	for v := range repeated {
		out = append(out, v)
	}
	sortStrings(out)
	return out
}

func init() {
	Register(&Rule{
		Code: "SDD090", Severity: Error, PyFunc: "_review",
		What: "a review declares the same finding id twice",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			entries, _ := reviewFindings(a)
			var ids []string
			for _, m := range entries {
				ids = append(ids, metaStr(m, "id"))
			}
			for _, dup := range duplicateValues(ids) {
				emit(Diagnostic{
					Code: "SDD090", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Duplicate finding id `" + dup + "`.",
					Correction: "Assign a new append-only id.",
				})
			}
		},
		Bad: []Example{{Name: "duplicate-finding-id", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n"+
					"  - id: F-01\n    severity: minor\n    title: Two\n    status: open\n",
				"", "### F-01 — one\n\nText.\n", ""),
		}}},
		Good: []Example{{Name: "unique-finding-ids", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n"+
					"  - id: F-02\n    severity: minor\n    title: Two\n    status: open\n",
				"", "### F-01 — one\n\nText.\n\n### F-02 — two\n\nText.\n", ""),
		}}},
	})

	Register(&Rule{
		Code: "SDD091", Severity: Error, PyFunc: "_review",
		What: "a review with status `resolved` still carries open findings",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			if metaStr(a.Meta, "status") != "resolved" {
				return
			}
			entries, _ := reviewFindings(a)
			for _, m := range entries {
				if metaStr(m, "status") != "open" {
					continue
				}
				emit(Diagnostic{
					Code: "SDD091", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Resolved review contains open findings.",
					Correction: "Resolve them or set review status to open.",
				})
				return // Python emits once for the review, not once per finding.
			}
		},
		Bad: []Example{{Name: "resolved-with-open", Files: map[string]string{
			"Retro/sample-review.md": replaceFirst(
				reviewWithBlocks("\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
					"", "### F-01 — one\n\nText.\n", ""),
				"status: open\ncreated:", "status: resolved\ncreated:"),
		}}},
		Good: []Example{{Name: "resolved-all-closed", Files: map[string]string{
			"Retro/sample-review.md": replaceFirst(
				reviewWithBlocks("\n  - id: F-01\n    severity: major\n    title: One\n    status: fixed\n",
					"", "### F-01 — one\n\nText.\n", "### F-01 — fixed\n\n2024-01-01. Done.\n"),
				"status: open\ncreated:", "status: resolved\ncreated:"),
		}}},
	})

	Register(&Rule{
		Code: "SDD092", Severity: Error, PyFunc: "_review",
		What: "a review follow-up is not a mapping, or omits a required field",
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
					emit(Diagnostic{
						Code: "SDD092", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "A follow-up is not a mapping.",
						Correction: "Add id, finding, summary, and tracked_in.",
					})
					continue
				}
				// Python tests `field not in followup` — presence only, so an
				// empty value satisfies this check (SDD095 covers empty
				// tracked_in separately).
				for _, field := range [4]string{"id", "finding", "summary", "tracked_in"} {
					if _, present := m[field]; present {
						continue
					}
					emit(Diagnostic{
						Code: "SDD092", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Follow-up is missing `" + field + "`.",
						Correction: "Add the `" + field + "` field.",
					})
				}
			}
		},
		Bad: []Example{{Name: "followup-missing-summary", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks("",
				"\n  - id: FU-01\n    finding: F-01\n    tracked_in: \"1.1\"\n", "", ""),
		}}},
		Good: []Example{{Name: "complete-followup", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks("",
				"\n  - id: FU-01\n    finding: F-01\n    summary: Do it later.\n    tracked_in: \"1.1\"\n", "", ""),
		}}},
	})

	Register(&Rule{
		Code: "SDD093", Severity: Error, PyFunc: "_review",
		What: "a review follow-up id is not of the form `FU-NN`",
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
					continue // SDD092 already reported it.
				}
				id := metaStr(m, "id")
				if followupIDPattern.MatchString(id) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD093", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Invalid follow-up id `" + id + "`.",
					Correction: "Use `FU-NN`.",
				})
			}
		},
		Bad: []Example{{Name: "short-followup-id", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks("",
				"\n  - id: FU-1\n    finding: F-01\n    summary: S.\n    tracked_in: \"1.1\"\n", "", ""),
		}}},
		Good: []Example{{Name: "well-formed-followup-id", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks("",
				"\n  - id: FU-01\n    finding: F-01\n    summary: S.\n    tracked_in: \"1.1\"\n", "", ""),
		}}},
	})

	Register(&Rule{
		Code: "SDD094", Severity: Error, PyFunc: "_review",
		What: "a review follow-up references a finding the review does not declare",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			followups, ok := a.Meta["followups"].([]any)
			if !ok {
				return
			}
			_, findingIDs := reviewFindings(a)
			for _, f := range followups {
				m := planEntry(f)
				if m == nil {
					continue
				}
				finding := metaStr(m, "finding")
				if findingIDs[finding] {
					continue
				}
				emit(Diagnostic{
					Code: "SDD094", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Follow-up `" + metaStr(m, "id") + "` references unknown `" + finding + "`.",
					Correction: "Reference a finding in this review.",
				})
			}
		},
		Bad: []Example{{Name: "followup-unknown-finding", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
				"\n  - id: FU-01\n    finding: F-99\n    summary: S.\n    tracked_in: \"1.1\"\n",
				"### F-01 — one\n\nText.\n", ""),
		}}},
		Good: []Example{{Name: "followup-known-finding", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
				"\n  - id: FU-01\n    finding: F-01\n    summary: S.\n    tracked_in: \"1.1\"\n",
				"### F-01 — one\n\nText.\n", ""),
		}}},
	})

	Register(&Rule{
		Code: "SDD097", Severity: Error, PyFunc: "_review",
		What: "a review declares the same follow-up id twice",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			followups, ok := a.Meta["followups"].([]any)
			if !ok {
				return
			}
			var ids []string
			for _, f := range followups {
				if m := planEntry(f); m != nil {
					ids = append(ids, metaStr(m, "id"))
				}
			}
			for _, dup := range duplicateValues(ids) {
				emit(Diagnostic{
					Code: "SDD097", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Duplicate follow-up id `" + dup + "`.",
					Correction: "Assign a new append-only id.",
				})
			}
		},
		Bad: []Example{{Name: "duplicate-followup-id", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
				"\n  - id: FU-01\n    finding: F-01\n    summary: A.\n    tracked_in: \"1.1\"\n"+
					"  - id: FU-01\n    finding: F-01\n    summary: B.\n    tracked_in: \"1.1\"\n",
				"### F-01 — one\n\nText.\n", ""),
		}}},
		Good: []Example{{Name: "unique-followup-ids", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
				"\n  - id: FU-01\n    finding: F-01\n    summary: A.\n    tracked_in: \"1.1\"\n"+
					"  - id: FU-02\n    finding: F-01\n    summary: B.\n    tracked_in: \"1.1\"\n",
				"### F-01 — one\n\nText.\n", ""),
		}}},
	})
}
