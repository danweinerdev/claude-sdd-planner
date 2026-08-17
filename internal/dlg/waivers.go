package dlg

import (
	"fmt"
	"strings"
)

// Ledger waivers — the compiler model's third level, after error and warning:
// a finding a human has explicitly and reasonedly silenced.
//
// This exists because a legacy ledger can carry conditions it is FORBIDDEN to
// repair. Append-only rules refuse renumbering and reordering, so DLG064 (an
// id gap) and DLG065 (out-of-order entries) inherited from before sequencing
// was enforced have no fix at all — the ledger would be permanently red with
// no supported remedy. Demoting them to warnings stops the gate; a waiver is
// how a maintainer records that the condition was seen, judged, and accepted.
//
// The discipline mirrors the artifact-side waivers in internal/rules: a waiver
// names a real diagnostic code, states a reason a human can evaluate, and is
// itself reported when malformed or stale. An exception nobody can assess is
// exactly what this mechanism exists to prevent.

// Waiver is one accepted exception declared in a ledger's frontmatter.
type Waiver struct {
	Code     string
	Reason   string
	Accepted string // optional YYYY-MM-DD the exception was accepted
	Line     int
	Used     bool
	Invalid  string // nonempty when this waiver excuses nothing, and why
}

// waivableCodes are the ledger diagnostics a waiver may silence.
//
// An allowlist rather than a denylist, deliberately. Supersession integrity,
// duplicate ids, and parse failures describe a ledger that cannot be read or
// trusted — silencing them would hide corruption rather than accept history.
// Only the two sequencing conditions qualify, because only they can be both
// genuinely wrong and genuinely unrepairable.
var waivableCodes = map[string]bool{
	"DLG064": true, // id sequence gap
	"DLG065": true, // entries out of ascending id order
}

// placeholderReasons parse as a reason while stating nothing.
var placeholderReasons = map[string]bool{
	"": true, "tbd": true, "todo": true, "n/a": true, "na": true,
	"none": true, "-": true, "xxx": true, "fixme": true,
	"reason": true, "waived": true, "accepted": true, "legacy": true,
}

// minReasonWords is the shortest reason that can carry an argument.
const minReasonWords = 4

// parseWaivers reads a ledger's `waivers:` frontmatter. Malformed entries are
// returned carrying Invalid rather than dropped, so DLG078 can report them.
func parseWaivers(l *Ledger) []Waiver {
	raw, ok := l.Meta["waivers"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return []Waiver{{
			Line:    l.Line("waivers:"),
			Invalid: "`waivers` must be a list of entries, each with `code` and `reason`",
		}}
	}
	var out []Waiver
	for i, item := range items {
		w := Waiver{Line: l.Line("waivers:")}
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
			if ln := l.Line("code: " + w.Code); ln > 1 {
				w.Line = ln
			}
		}

		switch {
		case w.Code == "":
			w.Invalid = fmt.Sprintf("waiver %d declares no `code`", i+1)
		case !waivableCodes[w.Code]:
			w.Invalid = fmt.Sprintf("`%s` cannot be waived; only %s may be, because only they "+
				"describe conditions append-only history can forbid repairing",
				w.Code, strings.Join(sortedCodes(waivableCodes), " and "))
		case placeholderReasons[strings.ToLower(w.Reason)]:
			w.Invalid = fmt.Sprintf("waiver for `%s` states no reason", w.Code)
		case len(strings.Fields(w.Reason)) < minReasonWords:
			w.Invalid = fmt.Sprintf("waiver for `%s` needs a reason that says why, not just that", w.Code)
		}
		out = append(out, w)
	}
	return out
}

func sortedCodes(set map[string]bool) []string {
	var out []string
	for c := range set {
		out = append(out, c)
	}
	sortStrings(out)
	return out
}

// applyWaivers re-tags each waived diagnostic and appends the bookkeeping
// findings: DLG078 for a malformed waiver, DLG079 for one that matched
// nothing. A stale waiver is reported because an exception outliving the
// condition it excused is how a silenced check quietly becomes permanent.
func applyWaivers(ledgers []*Ledger, diags []Diagnostic) []Diagnostic {
	var waivers []Waiver
	byLedger := map[string][]Waiver{}
	for _, l := range ledgers {
		ws := parseWaivers(l)
		byLedger[l.Path] = ws
		waivers = append(waivers, ws...)
	}
	if len(waivers) == 0 {
		return diags
	}

	for i := range diags {
		if !diags[i].Severity.Invalidating() && diags[i].Severity != Warning {
			continue
		}
		for _, ws := range byLedger[diags[i].Path] {
			if ws.Invalid != "" || ws.Code != diags[i].Code {
				continue
			}
			diags[i].Severity = Waived
			diags[i].Correction = "waived: " + ws.Reason
			break
		}
	}

	// Mark usage after re-tagging, so a waiver counts as used when a matching
	// finding exists at all.
	used := map[string]bool{}
	for _, d := range diags {
		if d.Severity == Waived {
			used[d.Path+"\x00"+d.Code] = true
		}
	}
	for _, l := range ledgers {
		for _, w := range byLedger[l.Path] {
			if w.Invalid != "" {
				diags = append(diags, diag(l, "DLG078",
					"Accepted exception is malformed: "+w.Invalid+".",
					"Give the waiver a waivable `code` and a `reason` stating why the finding does not "+
						"apply here; remove it to restore the check.", w.Line, "", Error))
				continue
			}
			if !used[l.Path+"\x00"+w.Code] {
				diags = append(diags, diag(l, "DLG079",
					"Accepted exception for `"+w.Code+"` matches no finding.",
					"Remove the stale waiver: the condition it excused is gone, and an exception that "+
						"outlives its cause silently disables a check.", w.Line, "", Warning))
			}
		}
	}
	return diags
}
