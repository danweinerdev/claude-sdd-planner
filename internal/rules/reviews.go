package rules

import (
	"regexp"
	"strings"
)

// Family (i cont'd): Validator._review and _review_supersession, beyond the
// SDD080/081/082 shape checks in specific.go.
//
// Findings:    SDD086 (status not allowed), SDD087 (no `### F-NN` body),
//              SDD088 (terminal finding with no Resolution Log entry),
//              SDD090 (duplicate id), SDD091 (resolved review, open findings),
//              SDD098 (deferred finding neither tracked nor citing a task).
// Follow-ups:  SDD092 (not a mapping / missing field), SDD093 (bad id),
//              SDD094 (unknown finding), SDD095 (floating), SDD097 (dup id).
// Supersession: SDD099 (status/link disagree), SDD100 (does not resolve),
//              SDD101 (not reciprocated), SDD102 (different target, or the
//              replaced review is not superseded).
//
// Still out of scope: SDD083-085, SDD089, SDD096.

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

// normalizedValue ports normalized(): lowercase, with runs of whitespace
// collapsed to single spaces and the ends trimmed. SDD102 compares two
// reviews' `review_of` through it, so "Specs/Sample/README.md" and
// "specs/sample/readme.md" name the same target.
func normalizedValue(v any) string {
	s, _ := v.(string)
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// supersessionFields is Python's (field, reverse) pairing: each link must be
// answered by its opposite on the target.
var supersessionFields = [2][2]string{
	{"supersedes", "superseded_by"},
	{"superseded_by", "supersedes"},
}

// reciprocates reports whether target's `reverse` field points back at src.
// Python accepts the path with or without its `.md` suffix, so a ledger that
// writes "Retro/old-review" and one that writes "Retro/old-review.md" are both
// valid back-links.
func reciprocates(target *Artifact, reverse, srcRel string) bool {
	got := metaStr(target.Meta, reverse)
	return got == srcRel || got == strings.TrimSuffix(srcRel, ".md")
}

func init() {
	Register(&Rule{
		Code: "SDD099", Severity: Error, PyFunc: "_review_supersession",
		What: "a review's `superseded_by` and its status disagree",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			status := metaStr(a.Meta, "status")
			supersededBy := metaStr(a.Meta, "superseded_by")
			if status == "superseded" && supersededBy == "" {
				emit(Diagnostic{
					Code: "SDD099", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Superseded review lacks `superseded_by`.",
					Correction: "Link the replacing review.",
				})
			}
			if supersededBy != "" && status != "superseded" {
				emit(Diagnostic{
					Code: "SDD099", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Review with `superseded_by` has status `" + status + "`.",
					Correction: "Set its artifact status to `superseded`.",
				})
			}
		},
		Bad: []Example{{Name: "superseded-without-link", Files: map[string]string{
			"Retro/sample-review.md": replaceFirst(
				reviewWithBlocks("", "", "", ""),
				"status: open\ncreated:", "status: superseded\ncreated:"),
		}}},
		Good: []Example{{Name: "open-review", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks("", "", "", ""),
		}}},
	})

	Register(&Rule{
		Code: "SDD100", Severity: Error, PyFunc: "_review_supersession",
		What: "a review's `supersedes`/`superseded_by` does not resolve to a review",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "review" {
					continue
				}
				for _, pair := range supersessionFields {
					field := pair[0]
					value := metaStr(a.Meta, field)
					if value == "" {
						continue
					}
					target := resolveRelated(r, value)
					if target != nil && target.Kind() == "review" {
						continue
					}
					emit(Diagnostic{
						Code: "SDD100", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Review `" + field + "` `" + value + "` does not resolve.",
						Correction: "Point it at an existing review.",
					})
				}
			}
		},
		Bad: []Example{{Name: "unresolved-supersedes", Files: map[string]string{
			"Retro/sample-review.md": replaceFirst(
				reviewWithBlocks("", "", "", ""),
				"rev: 1", "rev: 1\nsupersedes: \"Retro/nope.md\""),
		}}},
		Good: []Example{{Name: "resolved-supersedes", Files: map[string]string{
			"Retro/new-review.md": replaceFirst(
				reviewWithBlocks("", "", "", ""),
				"rev: 1", "rev: 1\nsupersedes: \"Retro/old-review.md\""),
			"Retro/old-review.md": replaceFirst(
				replaceFirst(reviewWithBlocks("", "", "", ""),
					"status: open\ncreated:", "status: superseded\ncreated:"),
				"rev: 1", "rev: 1\nsuperseded_by: \"Retro/new-review.md\""),
		}}},
	})

	Register(&Rule{
		Code: "SDD101", Severity: Error, PyFunc: "_review_supersession",
		What: "a review supersession link is not reciprocated by its target",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "review" {
					continue
				}
				for _, pair := range supersessionFields {
					field, reverse := pair[0], pair[1]
					value := metaStr(a.Meta, field)
					if value == "" {
						continue
					}
					target := resolveRelated(r, value)
					// Python's elif chain: SDD100 having fired suppresses this.
					if target == nil || target.Kind() != "review" {
						continue
					}
					if reciprocates(target, reverse, a.Rel) {
						continue
					}
					emit(Diagnostic{
						Code: "SDD101", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Review `" + field + "` link is not reciprocated.",
						Correction: "Add matching `" + reverse + "`.",
					})
				}
			}
		},
		Bad: []Example{{Name: "unreciprocated-link", Files: map[string]string{
			"Retro/new-review.md": replaceFirst(
				reviewWithBlocks("", "", "", ""),
				"rev: 1", "rev: 1\nsupersedes: \"Retro/old-review.md\""),
			"Retro/old-review.md": replaceFirst(reviewWithBlocks("", "", "", ""),
				"status: open\ncreated:", "status: superseded\ncreated:"),
		}}},
		Good: []Example{{Name: "reciprocated-link", Files: map[string]string{
			"Retro/new-review.md": replaceFirst(
				reviewWithBlocks("", "", "", ""),
				"rev: 1", "rev: 1\nsupersedes: \"Retro/old-review.md\""),
			"Retro/old-review.md": replaceFirst(
				replaceFirst(reviewWithBlocks("", "", "", ""),
					"status: open\ncreated:", "status: superseded\ncreated:"),
				"rev: 1", "rev: 1\nsuperseded_by: \"Retro/new-review.md\""),
		}}},
	})

	Register(&Rule{
		Code: "SDD102", Severity: Error, PyFunc: "_review_supersession",
		What: "a review supersession links a different target, or leaves the replaced review non-superseded",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "review" {
					continue
				}
				for _, pair := range supersessionFields {
					field, reverse := pair[0], pair[1]
					value := metaStr(a.Meta, field)
					if value == "" {
						continue
					}
					target := resolveRelated(r, value)
					if target == nil || target.Kind() != "review" {
						continue // SDD100
					}
					if !reciprocates(target, reverse, a.Rel) {
						continue // SDD101
					}
					if normalizedValue(target.Meta["review_of"]) != normalizedValue(a.Meta["review_of"]) {
						emit(Diagnostic{
							Code: "SDD102", Severity: Error, Path: a.Rel, Line: 1,
							Message:    "Review `" + field + "` links reviews of different targets.",
							Correction: "Link only reviews of the same normalized `review_of` target.",
						})
						continue
					}
					// Only the forward direction carries this check: a review
					// that claims to supersede another asserts that the other
					// is retired, and Python verifies the claim.
					if field != "supersedes" {
						continue
					}
					if targetStatus := metaStr(target.Meta, "status"); targetStatus != "superseded" {
						emit(Diagnostic{
							Code: "SDD102", Severity: Error, Path: a.Rel, Line: 1,
							Message:    "Superseded review `" + value + "` still has status `" + targetStatus + "`.",
							Correction: "Set the replaced review status to `superseded`.",
						})
					}
				}
			}
		},
		Bad: []Example{{Name: "supersedes-different-target", Files: map[string]string{
			"Retro/new-review.md": replaceFirst(
				reviewWithBlocks("", "", "", ""),
				"rev: 1", "rev: 1\nsupersedes: \"Retro/old-review.md\""),
			"Retro/old-review.md": replaceFirst(
				replaceFirst(
					replaceFirst(reviewWithBlocks("", "", "", ""),
						"status: open\ncreated:", "status: superseded\ncreated:"),
					"rev: 1", "rev: 1\nsuperseded_by: \"Retro/new-review.md\""),
				`review_of: "Specs/Sample/README.md"`, `review_of: "Specs/Other/README.md"`),
		}}},
		Good: []Example{{Name: "supersedes-same-target", Files: map[string]string{
			"Retro/new-review.md": replaceFirst(
				reviewWithBlocks("", "", "", ""),
				"rev: 1", "rev: 1\nsupersedes: \"Retro/old-review.md\""),
			"Retro/old-review.md": replaceFirst(
				replaceFirst(reviewWithBlocks("", "", "", ""),
					"status: open\ncreated:", "status: superseded\ncreated:"),
				"rev: 1", "rev: 1\nsuperseded_by: \"Retro/new-review.md\""),
		}}},
	})
}

// artifactsConnected ports Validator._artifacts_connected: a breadth-first
// walk over `related` links from left, bounded to depth 2, asking whether
// right is reachable. The bound is Python's and is load-bearing — it keeps a
// densely cross-linked planning root from making every artifact "related" to
// every other.
func artifactsConnected(r *Root, left, right *Artifact) bool {
	type step struct {
		a     *Artifact
		depth int
	}
	frontier := []step{{left, 0}}
	seen := map[string]bool{left.Rel: true}
	for len(frontier) > 0 {
		current := frontier[0]
		frontier = frontier[1:]
		if current.a.Rel == right.Rel {
			return true
		}
		if current.depth >= 2 {
			continue
		}
		if current.a.Meta == nil {
			continue
		}
		for _, ref := range asAnyList(current.a.Meta["related"]) {
			s, ok := ref.(string)
			if !ok {
				continue
			}
			target := resolveRelated(r, s)
			if target == nil || seen[target.Rel] {
				continue
			}
			seen[target.Rel] = true
			frontier = append(frontier, step{target, current.depth + 1})
		}
	}
	return false
}

// candidatePlanNames ports Validator._candidate_plan_names: the plans an
// artifact might belong to. A direct name wins outright; otherwise every plan
// connected to what this artifact reviews or relates to is a candidate.
//
// Returning a set rather than one name is deliberate — an ambiguous answer is
// what SDD096 reports, so collapsing it early would hide the defect.
func candidatePlanNames(r *Root, a *Artifact) map[string]bool {
	if direct := planNameFor(a); direct != "" {
		return map[string]bool{direct: true}
	}
	var targets []*Artifact
	if reviewOf, ok := a.Meta["review_of"].(string); ok {
		if t := resolveRelated(r, reviewOf); t != nil {
			targets = append(targets, t)
		}
	}
	for _, ref := range asAnyList(a.Meta["related"]) {
		if s, ok := ref.(string); ok {
			if t := resolveRelated(r, s); t != nil {
				targets = append(targets, t)
			}
		}
	}
	out := map[string]bool{}
	for _, plan := range r.Artifacts {
		if plan.Meta == nil || plan.Kind() != "plan" {
			continue
		}
		name := planNameFor(plan)
		if name == "" {
			continue
		}
		for _, t := range targets {
			if plan.Rel == t.Rel || artifactsConnected(r, plan, t) || artifactsConnected(r, t, plan) {
				out[name] = true
				break
			}
		}
	}
	return out
}

// tasksByPlan indexes every phase's declared tasks by (plan name, task id),
// mirroring how Validator.tasks is populated: from each phase's own `plan`
// field, first writer winning a collision (SDD031 reports the duplicate).
func tasksByPlan(r *Root) map[[2]string]bool {
	out := map[[2]string]bool{}
	for _, a := range r.Artifacts {
		if a.Meta == nil || a.Kind() != "phase" {
			continue
		}
		planName := metaStr(a.Meta, "plan")
		for _, t := range asAnyList(a.Meta["tasks"]) {
			m := planEntry(t)
			if m == nil {
				continue
			}
			id, ok := m["id"].(string)
			if !ok {
				continue
			}
			out[[2]string{planName, id}] = true
		}
	}
	return out
}

// taskCitationRe matches a bare `N.N` task reference in a resolution entry.
var taskCitationRe = regexp.MustCompile(`\b\d+\.\d+\b`)

// resolutionEntryFor ports resolution_entry(): the Resolution Log text from a
// finding's `### F-NN` heading up to the next `### F-NN`, or the end.
func resolutionEntryFor(log, findingID string) string {
	head := regexp.MustCompile(`(?m)^###\s+` + regexp.QuoteMeta(findingID) + `\b.*$`)
	loc := head.FindStringIndex(log)
	if loc == nil {
		return ""
	}
	rest := log[loc[1]:]
	if next := regexp.MustCompile(`(?m)^###\s+F-\d+\b`).FindStringIndex(rest); next != nil {
		return log[loc[0] : loc[1]+next[0]]
	}
	return log[loc[0]:]
}

func init() {
	Register(&Rule{
		Code: "SDD098", Severity: Error, PyFunc: "_review",
		What: "a deferred finding is neither tracked by a follow-up nor cites an existing task",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			tasks := tasksByPlan(r)
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "review" {
					continue
				}
				entries, _ := reviewFindings(a)
				if len(entries) == 0 {
					continue
				}
				planNames := candidatePlanNames(r, a)

				// A follow-up whose tracked_in names a real task in a
				// candidate plan discharges its finding. Python only counts a
				// follow-up that resolved unambiguously (exactly one match).
				tracked := map[string]bool{}
				for _, f := range asAnyList(a.Meta["followups"]) {
					m := planEntry(f)
					if m == nil {
						continue
					}
					trackedIn := metaStr(m, "tracked_in")
					if trackedIn == "" {
						continue
					}
					matches := 0
					for plan := range planNames {
						if tasks[[2]string{plan, trackedIn}] {
							matches++
						}
					}
					if matches == 1 {
						tracked[metaStr(m, "finding")] = true
					}
				}

				resolution := sections(a, 2)["Resolution Log"].Body
				for _, m := range entries {
					id := metaStr(m, "id")
					if metaStr(m, "status") != "deferred" || tracked[id] {
						continue
					}
					entry := resolutionEntryFor(resolution, id)
					cited := false
					for _, taskID := range taskCitationRe.FindAllString(entry, -1) {
						for plan := range planNames {
							if tasks[[2]string{plan, taskID}] {
								cited = true
								break
							}
						}
						if cited {
							break
						}
					}
					if cited {
						continue
					}
					emit(Diagnostic{
						Code: "SDD098", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Deferred finding `" + id + "` is untracked.",
						Correction: "Cite an existing task in the reviewed plan or add a tracked follow-up.",
					})
				}
			}
		},
		Bad: []Example{{Name: "untracked-deferred-finding", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: deferred\n",
				"", "### F-01 — one\n\nText.\n", "### F-01 — deferred\n\n2024-01-01. Later.\n"),
		}}},
		Good: []Example{{Name: "deferred-finding-cites-task", Files: map[string]string{
			"Retro/sample-review.md": replaceFirst(
				reviewWithBlocks(
					"\n  - id: F-01\n    severity: major\n    title: One\n    status: deferred\n",
					"", "### F-01 — one\n\nText.\n",
					"### F-01 — deferred\n\n2024-01-01. Tracked as 1.1.\n"),
				`review_of: "Specs/Sample/README.md"`, `review_of: "Plans/Sample/README.md"`),
			"Plans/Sample/README.md": validPlan(false),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`, false, true),
		}}},
	})
}

var findingIDPattern = regexp.MustCompile(`^F-\d{2,}$`)

var findingSeverities = map[string]bool{
	"critical": true, "major": true, "minor": true, "question": true,
}

// requiredFindingFields is Python's tuple, in order: SDD083 emits one
// diagnostic per missing field, so the order is the output order.
var requiredFindingFields = [4]string{"id", "severity", "title", "status"}

// pyStrList renders a []string the way Python renders a list in an f-string:
// ['a', 'b']. SDD096's ambiguity message interpolates one directly, and the
// oracle compares message text.
func pyStrList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, s := range items {
		quoted = append(quoted, "'"+strings.ReplaceAll(s, "'", "\\'")+"'")
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func init() {
	Register(&Rule{
		Code: "SDD083", Severity: Error, PyFunc: "_review",
		What: "a review finding is not a mapping, or omits a required field",
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
					emit(Diagnostic{
						Code: "SDD083", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "A finding is not a mapping.",
						Correction: "Add id, severity, title, and status.",
					})
					continue
				}
				for _, field := range requiredFindingFields {
					// Python's `in (None, "")`: present-but-empty counts as
					// missing, other falsy values do not.
					if v, present := m[field]; present && v != nil && v != "" {
						continue
					}
					emit(Diagnostic{
						Code: "SDD083", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Finding is missing `" + field + "`.",
						Correction: "Add a nonempty `" + field + "`.",
					})
				}
			}
		},
		Bad: []Example{{Name: "finding-missing-title", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    status: open\n",
				"", "### F-01 — one\n\nText.\n", ""),
		}}},
		Good: []Example{{Name: "complete-finding", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
				"", "### F-01 — one\n\nText.\n", ""),
		}}},
	})

	Register(&Rule{
		Code: "SDD084", Severity: Error, PyFunc: "_review",
		What: "a review finding id is not of the form `F-NN`",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			entries, _ := reviewFindings(a)
			for _, m := range entries {
				id := metaStr(m, "id")
				if findingIDPattern.MatchString(id) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD084", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Invalid finding id `" + id + "`.",
					Correction: "Use `F-NN`.",
				})
			}
		},
		Bad: []Example{{Name: "short-finding-id", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-1\n    severity: major\n    title: One\n    status: open\n",
				"", "### F-1 — one\n\nText.\n", ""),
		}}},
		Good: []Example{{Name: "well-formed-finding-id", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
				"", "### F-01 — one\n\nText.\n", ""),
		}}},
	})

	Register(&Rule{
		Code: "SDD085", Severity: Error, PyFunc: "_review",
		What: "a review finding's `severity` is not an allowed value",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			entries, _ := reviewFindings(a)
			for _, m := range entries {
				if findingSeverities[metaStr(m, "severity")] {
					continue
				}
				emit(Diagnostic{
					Code: "SDD085", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Finding `" + metaStr(m, "id") + "` has invalid severity.",
					Correction: "Use critical, major, minor, or question.",
				})
			}
		},
		Bad: []Example{{Name: "unknown-severity", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: catastrophic\n    title: One\n    status: open\n",
				"", "### F-01 — one\n\nText.\n", ""),
		}}},
		Good: []Example{{Name: "allowed-severity", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
				"", "### F-01 — one\n\nText.\n", ""),
		}}},
	})

	Register(&Rule{
		Code: "SDD089", Severity: Error, PyFunc: "_review",
		What: "a terminal finding's status disagrees with its Resolution Log disposition",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			resolution := sections(a, 2)["Resolution Log"].Body
			entries, _ := reviewFindings(a)
			for _, m := range entries {
				status := metaStr(m, "status")
				if status == "open" {
					continue
				}
				id := metaStr(m, "id")
				match := resolutionEntryRe(id).FindStringSubmatch(resolution)
				if match == nil {
					continue // SDD088 owns the missing-entry case.
				}
				if match[1] == status {
					continue
				}
				emit(Diagnostic{
					Code: "SDD089", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Finding `" + id + "` status disagrees with its resolution.",
					Correction: "Make both dispositions agree.",
				})
			}
		},
		Bad: []Example{{Name: "status-disagrees-with-resolution", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: fixed\n",
				"", "### F-01 — one\n\nText.\n", "### F-01 — rejected\n\n2024-01-01. Nope.\n"),
		}}},
		Good: []Example{{Name: "status-agrees-with-resolution", Files: map[string]string{
			"Retro/sample-review.md": reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: fixed\n",
				"", "### F-01 — one\n\nText.\n", "### F-01 — fixed\n\n2024-01-01. Done.\n"),
		}}},
	})

	Register(&Rule{
		Code: "SDD096", Severity: Error, PyFunc: "_review",
		What: "a follow-up's `tracked_in` names no task, or an ambiguous one across plans",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			tasks := tasksByPlan(r)
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "review" {
					continue
				}
				followups, ok := a.Meta["followups"].([]any)
				if !ok {
					continue
				}
				planNames := candidatePlanNames(r, a)
				for _, f := range followups {
					m := planEntry(f)
					if m == nil {
						continue
					}
					tracked := metaStr(m, "tracked_in")
					if tracked == "" {
						continue // SDD095 owns the floating case.
					}
					var matches []string
					for plan := range planNames {
						if tasks[[2]string{plan, tracked}] {
							matches = append(matches, plan)
							continue
						}
						// Graph plans anchor follow-ups in the committed
						// graph: live node ids and the retired register
						// (the append-only tombstone place), with the
						// convert spelling accepted both ways (3.3 <->
						// task-3-3). A frozen review is immutable, so an
						// in-place rebuild that supersedes the tracked v1
						// task records the old id via `sdd graph retire`
						// rather than editing the review.
						if planArt, ok := r.ByPath["Plans/"+plan+"/README.md"]; ok {
							if ids, ok := planGraphIDs(planArt); ok {
								converted := "task-" + strings.ReplaceAll(tracked, ".", "-")
								if ids[tracked] || ids[converted] {
									matches = append(matches, plan)
								}
							}
						}
					}
					id := metaStr(m, "id")
					switch {
					case len(matches) == 0:
						emit(Diagnostic{
							Code: "SDD096", Severity: Error, Path: a.Rel, Line: 1,
							Message:    "Follow-up `" + id + "` points to unknown task `" + tracked + "`.",
							Correction: "Reference an existing task in a related plan, a graph node id, or record the superseded v1 id in the plan graph's retired register (`sdd graph retire`).",
						})
					case len(matches) > 1:
						// Python interpolates the list built by iterating
						// plan_names, a set. Sorting makes the rendering
						// deterministic; a set's order is arbitrary anyway.
						sortStrings(matches)
						emit(Diagnostic{
							Code: "SDD096", Severity: Error, Path: a.Rel, Line: 1,
							Message:    "Follow-up `" + id + "` task `" + tracked + "` is ambiguous across plans " + pyStrList(matches) + ".",
							Correction: "Link the review to one plan or use an unambiguous tracked task.",
						})
					}
				}
			}
		},
		Bad: []Example{{Name: "followup-unknown-task", Files: map[string]string{
			"Retro/sample-review.md": replaceFirst(
				reviewWithBlocks(
					"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
					"\n  - id: FU-01\n    finding: F-01\n    summary: S.\n    tracked_in: \"9.9\"\n",
					"### F-01 — one\n\nText.\n", ""),
				`review_of: "Specs/Sample/README.md"`, `review_of: "Plans/Sample/README.md"`),
			"Plans/Sample/README.md": validPlan(false),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`, false, true),
		}}},
		Good: []Example{{Name: "followup-retired-in-graph", Files: map[string]string{
			// The frozen-review escape: the tracked v1 task was superseded
			// by a graph rebuild, and its id lives in the committed graph's
			// retired register (`sdd graph retire`) — the follow-up keeps
			// resolving without editing the immutable review.
			"Retro/graph-review.md": replaceFirst(
				reviewWithBlocks(
					"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
					"\n  - id: FU-01\n    finding: F-01\n    summary: S.\n    tracked_in: \"3.3\"\n",
					"### F-01 — one\n\nText.\n", ""),
				`review_of: "Specs/Sample/README.md"`, `review_of: "Plans/Sample/README.md"`),
			"Plans/Sample/README.md":         validPlan(false),
			"Plans/Sample/Sample-Graph.json": `{"version":1,"seq_counter":0,"nodes":[],"retired":["task-3-3"]}`,
		}}, {Name: "followup-known-task", Files: map[string]string{
			"Retro/sample-review.md": replaceFirst(
				reviewWithBlocks(
					"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
					"\n  - id: FU-01\n    finding: F-01\n    summary: S.\n    tracked_in: \"1.1\"\n",
					"### F-01 — one\n\nText.\n", ""),
				`review_of: "Specs/Sample/README.md"`, `review_of: "Plans/Sample/README.md"`),
			"Plans/Sample/README.md": validPlan(false),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`, false, true),
		}}},
	})
}
