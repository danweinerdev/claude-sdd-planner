package rules

import (
	"fmt"
	"sort"
	"strings"
)

// Accepted exceptions ("waivers").
//
// A gate that cannot be excepted gets bypassed some other way: by loosening the
// rule for everyone, by hand-editing artifacts until the check passes, or by
// dropping the validator from CI. Each of those trades a narrow, attributable
// exception for a broad, silent one. A waiver makes the exception the *cheapest
// honest option* and leaves it in the record.
//
// The design constraint is that waiving must never be quieter than fixing:
//
//   - A waived diagnostic is still reported, as WAIVED, carrying its rationale.
//     It stops making the root invalid; it does not stop being visible.
//   - Waivers live in the frontmatter of the artifact whose finding they
//     excuse — attributable to a commit and an author, greppable, reviewable in
//     the diff that introduces them. There is deliberately no config-file or
//     command-line form, because either would let a pipeline silence findings
//     without changing a tracked artifact.
//   - A waiver must state a reason. An unexplained exception is the thing this
//     mechanism exists to prevent, so a missing or placeholder reason makes the
//     waiver itself invalid and leaves the finding an error (SDD176).
//   - A waiver that no longer matches anything is reported (SDD177), because
//     stale exceptions accumulate into a standing suppression list that nobody
//     re-reads.
//
// `sdd validate --no-waivers` ignores the whole mechanism, so CI can always ask
// what the unexcused state is.

// unwaivableCodes are the findings a waiver may not cover.
//
// These are the parse-stage failures: the validator could not model the file at
// all. Every other rule's finding is a claim *about* a document the validator
// understood, and a human can reasonably judge that claim inapplicable. A parse
// failure is not a judgment call — waiving it would assert that an unreadable
// or malformed artifact is acceptable, and every downstream rule that silently
// did not run on it stays silently not-run.
var unwaivableCodes = map[string]string{
	"SDD002": "the artifact could not be read",
	"SDD003": "the artifact has CRLF line endings",
	"SDD004": "the artifact has no opening frontmatter delimiter",
	"SDD005": "the artifact has no closing frontmatter delimiter",
	"SDD006": "the artifact's frontmatter is not valid YAML",
	"SDD007": "the artifact's frontmatter is not a mapping",
}

// Waiver is one accepted exception declared in an artifact's frontmatter.
type Waiver struct {
	Code     string
	Reason   string
	Accepted string // optional ISO date, informational

	// Line is the frontmatter line the waiver was declared on, so a diagnostic
	// about the waiver itself points at the waiver rather than the artifact.
	Line int
	// Used records whether this waiver matched a diagnostic during Run, which
	// is what SDD177 reports on.
	Used bool
	// Invalid, when nonempty, is why this waiver does not excuse anything.
	Invalid string
}

// placeholderReasons are values that parse as a reason but state nothing. A
// waiver is a written justification; text that was never written does not
// become one by sitting in the reason field.
var placeholderReasons = map[string]bool{
	"":         true,
	"tbd":      true,
	"todo":     true,
	"n/a":      true,
	"na":       true,
	"none":     true,
	"-":        true,
	"xxx":      true,
	"fixme":    true,
	"reason":   true,
	"waived":   true,
	"accepted": true,
}

// minReasonWords is the shortest reason that can carry an argument. "Not
// applicable" and "known issue" are assertions, not reasons; the threshold
// forces at least a sentence fragment naming why.
const minReasonWords = 4

// Waivers parses an artifact's `waivers:` frontmatter into declared exceptions.
// Malformed entries are returned too, carrying Invalid, so SDD176 can report
// them rather than have them vanish.
func Waivers(a *Artifact) []Waiver {
	raw, ok := a.Meta["waivers"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return []Waiver{{
			Line:    a.Line("waivers:", false),
			Invalid: "`waivers` must be a list of entries, each with `code` and `reason`",
		}}
	}
	var out []Waiver
	for i, item := range items {
		w := Waiver{Line: a.Line("waivers:", false)}
		m, ok := item.(map[string]any)
		if !ok {
			w.Invalid = fmt.Sprintf("waiver %d is not a mapping with `code` and `reason`", i+1)
			out = append(out, w)
			continue
		}
		w.Code, _ = m["code"].(string)
		w.Reason, _ = m["reason"].(string)
		w.Accepted, _ = m["accepted"].(string)
		w.Code = strings.ToUpper(strings.TrimSpace(w.Code))
		w.Reason = strings.TrimSpace(w.Reason)
		if w.Code != "" {
			// Point at the code itself when we can, so the correction lands on
			// the offending line rather than the block header.
			if ln := a.Line("code: "+w.Code, false); ln > 1 {
				w.Line = ln
			}
		}

		switch {
		case w.Code == "":
			w.Invalid = fmt.Sprintf("waiver %d declares no `code`", i+1)
		case !knownCode(w.Code):
			w.Invalid = fmt.Sprintf("`%s` is not a diagnostic code this validator emits", w.Code)
		case unwaivableCodes[w.Code] != "":
			w.Invalid = fmt.Sprintf("`%s` cannot be waived: %s, so no rule below it ran",
				w.Code, unwaivableCodes[w.Code])
		case placeholderReasons[strings.ToLower(strings.Trim(w.Reason, ". "))]:
			w.Invalid = fmt.Sprintf("waiver for `%s` states no reason", w.Code)
		case len(strings.Fields(w.Reason)) < minReasonWords:
			w.Invalid = fmt.Sprintf("waiver for `%s` states a reason too short to justify it (%q)",
				w.Code, w.Reason)
		}
		out = append(out, w)
	}
	return out
}

// knownCode reports whether code is one this validator can emit. A waiver for
// an unknown code is always a mistake — a typo, or a rule that has since been
// renamed — and silently keeping it would leave a waiver that can never match.
func knownCode(code string) bool {
	for _, r := range All() {
		if r.Code == code {
			return true
		}
	}
	// Codes emitted outside the rule registry still count as known.
	return code == "SDD000" || strings.HasPrefix(code, "DLG")
}

// applyWaivers marks diagnostics excused by their artifact's declared waivers
// and returns the diagnostics that report on the waivers themselves.
//
// Matching is per-artifact and by code: a waiver in a phase document excuses
// that code's findings on that document only. It deliberately cannot reach
// another artifact, because an exception's blast radius should be visible from
// the file it is written in.
func applyWaivers(r *Root, diags []Diagnostic) []Diagnostic {
	byPath := map[string][]Waiver{}
	for _, a := range r.Artifacts {
		if ws := Waivers(a); len(ws) > 0 {
			byPath[a.Rel] = ws
		}
	}
	if len(byPath) == 0 {
		return nil
	}

	for i := range diags {
		d := &diags[i]
		if d.Severity != Error {
			continue
		}
		for j := range byPath[d.Path] {
			w := &byPath[d.Path][j]
			if w.Invalid != "" || w.Code != d.Code {
				continue
			}
			w.Used = true
			d.Severity = Waived
			d.WaivedReason = w.Reason
		}
	}

	var out []Diagnostic
	for _, path := range sortedKeys(byPath) {
		for _, w := range byPath[path] {
			switch {
			case w.Invalid != "":
				out = append(out, Diagnostic{
					Code: "SDD176", Severity: Error, Path: path, Line: w.Line,
					Message: "Accepted exception is malformed: " + w.Invalid + ".",
					Correction: "Give the waiver a `code` this validator emits and a `reason` " +
						"stating why the finding does not apply here; an exception nobody can " +
						"evaluate is the thing waivers exist to avoid. Remove it to restore the gate.",
				})
			case !w.Used:
				out = append(out, Diagnostic{
					Code: "SDD177", Severity: Error, Path: path, Line: w.Line,
					Message: fmt.Sprintf("Accepted exception for `%s` matches no finding on this artifact.", w.Code),
					Correction: "Remove the waiver. The condition it excused no longer occurs, and a " +
						"standing exception that matches nothing will silently excuse the finding " +
						"if it ever returns.",
				})
			}
		}
	}
	return out
}

// waiverDiagnostics emits the waiver-bookkeeping findings for one code.
//
// Registering SDD176/177 as ordinary rules keeps them in `--explain` and
// subject to the registry meta-test's example requirement, which is what proves
// each one actually fires and actually stays quiet.
//
// It must not call Run: these rules are invoked *by* Run, so doing so would
// recurse forever. Instead it evaluates the rules a second time through
// runBare, which is the same rule sweep with the waiver rules themselves
// excluded — enough to know which codes fire on which artifacts, which is all
// staleness (SDD177) depends on. Malformedness (SDD176) needs no findings at
// all, only the waiver text.
func waiverDiagnostics(r *Root, code string, emit func(Diagnostic)) {
	for _, d := range applyWaivers(r, bareOnce(r)) {
		if d.Code == code {
			emit(d)
		}
	}
}

// bareOnce is runBare memoized per Root. Both waiver rules need the same
// sweep, and each recomputing it made `sdd validate` evaluate every rule three
// times over (and a lifecycle transition six times) on a root where one sweep
// already dominates the runtime.
//
// Safe because a Root is immutable once loaded: LoadRoot/LoadRootRepo build it
// and the rules only read from it. Callers that need fresh results build a new
// Root, which is exactly what the transition verbs already do.
func bareOnce(r *Root) []Diagnostic {
	if !r.bareComputed {
		r.bareDiagnostics = runBare(r)
		r.bareComputed = true
	}
	return r.bareDiagnostics
}

// waiverRuleCodes are the rules implemented by waiverDiagnostics. runBare skips
// them to break the recursion described above.
var waiverRuleCodes = map[string]bool{"SDD176": true, "SDD177": true}

// runBare is Run without the waiver-bookkeeping rules.
func runBare(r *Root) []Diagnostic {
	var out []Diagnostic
	emit := func(d Diagnostic) { out = append(out, d) }
	for _, rule := range All() {
		if rule.CheckRoot != nil && !waiverRuleCodes[rule.Code] {
			rule.CheckRoot(r, emit)
		}
	}
	for _, a := range r.Artifacts {
		for _, rule := range All() {
			if rule.Check != nil && !waiverRuleCodes[rule.Code] {
				rule.Check(a, emit)
			}
		}
	}
	return out
}

func init() {
	Register(&Rule{
		Code: "SDD176", Severity: Error, Native: true,
		What: "an accepted exception (`waivers`) is malformed, names an unknown or unwaivable code, or states no reason",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			waiverDiagnostics(r, "SDD176", emit)
		},
		Good: []Example{{
			Name: "well-formed-waiver",
			Files: map[string]string{
				"Research/topic.md": "---\n" +
					"title: \"Topic\"\ntype: research\nstatus: draft\n" +
					"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\ntags: [\"a\"]\nrelated: []\n" +
					"waivers:\n" +
					"  - code: SDD020\n" +
					"    reason: \"This research artifact predates the tagging convention and is archived.\"\n" +
					"---\n\n# Topic\n\n## Summary\n\nText.\n",
			},
		}},
		Bad: []Example{{
			Name: "waiver-without-reason",
			Files: map[string]string{
				"Research/topic.md": "---\n" +
					"title: \"Topic\"\ntype: research\nstatus: draft\n" +
					"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\ntags: [\"a\"]\nrelated: []\n" +
					"waivers:\n" +
					"  - code: SDD020\n" +
					"    reason: \"TBD\"\n" +
					"---\n\n# Topic\n\n## Summary\n\nText.\n",
			},
		}, {
			Name: "waiver-for-unwaivable-parse-code",
			Files: map[string]string{
				"Research/topic.md": "---\n" +
					"title: \"Topic\"\ntype: research\nstatus: draft\n" +
					"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\ntags: [\"a\"]\nrelated: []\n" +
					"waivers:\n" +
					"  - code: SDD006\n" +
					"    reason: \"The frontmatter is intentionally malformed for this fixture.\"\n" +
					"---\n\n# Topic\n\n## Summary\n\nText.\n",
			},
		}},
	})

	Register(&Rule{
		Code: "SDD177", Severity: Error, Native: true,
		What: "an accepted exception (`waivers`) matches no finding on its artifact and has gone stale",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			waiverDiagnostics(r, "SDD177", emit)
		},
		Bad: []Example{{
			Name: "stale-waiver",
			Files: map[string]string{
				"Research/topic.md": "---\n" +
					"title: \"Topic\"\ntype: research\nstatus: draft\n" +
					"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\ntags: [\"a\"]\nrelated: []\n" +
					"waivers:\n" +
					"  - code: SDD020\n" +
					"    reason: \"Kept after the underlying finding was fixed, so it now matches nothing.\"\n" +
					"---\n\n# Topic\n\n## Summary\n\nText.\n" +
					"\n## Context\n\nText.\n\n## Findings\n\nText.\n" +
					"\n## Analysis\n\nText.\n\n## Open Questions\n\nText.\n",
			},
		}},
		Good: []Example{{
			Name: "no-waivers-declared",
			Files: map[string]string{
				"Research/topic.md": "---\n" +
					"title: \"Topic\"\ntype: research\nstatus: draft\n" +
					"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\ntags: [\"a\"]\nrelated: []\n" +
					"---\n\n# Topic\n\n## Summary\n\nText.\n" +
					"\n## Context\n\nText.\n\n## Findings\n\nText.\n" +
					"\n## Analysis\n\nText.\n\n## Open Questions\n\nText.\n",
			},
		}},
	})
}

func sortedKeys(m map[string][]Waiver) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
