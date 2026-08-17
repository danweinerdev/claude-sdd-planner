package rules

import (
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/schema"
)

// Family (k): Validator._ledger — SDD110-118, the decision-log entry schema.
//
// Python dispatches _ledger from _artifact only when artifact.kind is
// "decision-log", so every rule here guards on that kind and stays silent for
// every other artifact type.
//
// Two ordering details are load-bearing for parity. SDD110 `return`s rather
// than falling through, so a non-list `decisions` suppresses SDD111-118
// entirely. And within an entry, a non-mapping `continue`s at SDD111, so the
// remaining seven checks never see it — but a mapping that fails SDD112 still
// runs every later check, because Python only warns per missing field and
// keeps going.

// decisionEntries returns the raw `decisions` value of a decision-log artifact
// and whether the artifact is one at all. The bool distinguishes "not a
// decision-log, stay silent" from "a decision-log whose decisions are absent".
func decisionEntries(a *Artifact) (any, bool) {
	if a.Meta == nil || a.Kind() != "decision-log" {
		return nil, false
	}
	return a.Meta["decisions"], true
}

// eachDecision runs fn over every entry that is a mapping, mirroring the
// SDD110 early return and the SDD111 continue. Rules SDD112-118 all sit behind
// exactly this guard, so they share it rather than each re-deriving it.
func eachDecision(a *Artifact, fn func(entry map[string]any, id string)) {
	raw, isLedger := decisionEntries(a)
	if !isLedger {
		return
	}
	entries, ok := raw.([]any)
	if !ok {
		return // SDD110 already fired; Python returns before the loop.
	}
	for _, e := range entries {
		m := planEntry(e)
		if m == nil {
			continue // SDD111 already fired; Python continues past it.
		}
		// Python builds the id with str(entry.get("id", "")), so a missing id
		// becomes the empty string and still labels the later messages.
		fn(m, metaStr(m, "id"))
	}
}

// The facts these rules enforce — which fields are required, the id pattern,
// the closed value sets, and the two conditional rules — are declared in
// internal/schema/decision-log.json under `decisions.entry`, not written out
// here. Adding a decision status or a new required field is a schema edit; the
// checks below follow automatically.
//
// Diagnostic text is still reproduced from the Python validator verbatim, so
// the differential oracle in tools/parity keeps comparing message strings.

// decisionEntrySchema returns the declared shape of one decisions[] entry.
// It is loaded once: the schema is embedded, so a failure here is a build
// defect rather than a user error.
var decisionEntrySchema = func() *schema.Entry {
	s, err := schema.Load("decision-log")
	if err != nil {
		panic("rules: loading decision-log schema: " + err.Error())
	}
	f := s.Field("decisions")
	if f == nil || f.Entry == nil {
		panic("rules: decision-log schema declares no decisions entry shape")
	}
	return f.Entry
}()

// entryField returns one declared field of a decisions[] entry, panicking if
// the schema stopped declaring it — a rule keyed to a field the schema no
// longer has would otherwise go silently dead.
func entryField(key string) *schema.EntryField {
	f := decisionEntrySchema.Field(key)
	if f == nil {
		panic("rules: decision-log schema declares no `" + key + "` field")
	}
	return f
}

// requiredDecisionFields is the declared required fields, in schema order.
// SDD112 emits one diagnostic per missing field, so the order is the schema's.
func requiredDecisionFields() []string {
	var out []string
	for _, f := range decisionEntrySchema.Fields {
		if f.Required {
			out = append(out, f.Key)
		}
	}
	return out
}

// enumAllows reports whether a declared enum field permits a value.
func enumAllows(key, value string) bool {
	for _, v := range entryField(key).Enum {
		if v == value {
			return true
		}
	}
	return false
}

// ledgerFixture is a decision log carrying one entry that satisfies every
// SDD110-118 check, used as the Good example baseline each rule perturbs.
const ledgerFixture = `
  - id: D-0001
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: We chose the thing.
    rationale: It was the better thing.
`

func init() {
	Register(&Rule{
		Code: "SDD110", Severity: Error, PyFunc: "_ledger",
		What: "a decision log's `decisions` is not a list",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			raw, isLedger := decisionEntries(a)
			if !isLedger {
				return
			}
			if _, ok := raw.([]any); ok {
				return
			}
			emit(Diagnostic{
				Code: "SDD110", Severity: Error, Path: a.Rel, Line: 1,
				Message:    "`decisions` must be a list.",
				Correction: "Use `decisions: []` when empty.",
			})
		},
		Bad: []Example{{Name: "decisions-not-a-list", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(" {}"),
		}}},
		Good: []Example{{Name: "decisions-is-a-list", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(ledgerFixture),
		}}},
	})

	Register(&Rule{
		Code: "SDD111", Severity: Error, PyFunc: "_ledger",
		What: "a decision-log entry is not a mapping",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			raw, isLedger := decisionEntries(a)
			if !isLedger {
				return
			}
			entries, ok := raw.([]any)
			if !ok {
				return
			}
			for _, e := range entries {
				if planEntry(e) != nil {
					continue
				}
				emit(Diagnostic{
					Code: "SDD111", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "A decision is not a mapping.",
					Correction: "Use the decision entry schema.",
				})
			}
		},
		Bad: []Example{{Name: "scalar-entry", Files: map[string]string{
			"Decisions/decisions.md": decisionLog("\n  - just a string\n"),
		}}},
		Good: []Example{{Name: "mapping-entry", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(ledgerFixture),
		}}},
	})

	Register(&Rule{
		Code: "SDD112", Severity: Error, PyFunc: "_ledger",
		What: "a decision entry omits a required field",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			eachDecision(a, func(entry map[string]any, id string) {
				for _, field := range requiredDecisionFields() {
					// Python's test is `entry.get(field) in (None, "")`, so a
					// present-but-empty value counts as missing, and any other
					// falsy value (0, false) does not.
					if v, present := entry[field]; present && v != nil && v != "" {
						continue
					}
					// Name the entry. Without the id, two entries missing the
					// same field produced two byte-identical lines at line 1,
					// which reads as the validator repeating itself rather
					// than as two findings, and leaves the reader no way to
					// tell which entries to fix. It also lets scoped
					// validation decide whether the finding is relevant.
					subject := "Decision"
					if id != "" {
						subject = "Decision `" + id + "`"
					}
					emit(Diagnostic{
						Code: "SDD112", Severity: Error, Path: a.Rel, Line: 1,
						Message:    subject + " is missing `" + field + "`.",
						Correction: "Add a nonempty `" + field + "`.",
					})
				}
			})
		},
		Bad: []Example{{Name: "missing-rationale", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: We chose the thing.
`),
		}}},
		Good: []Example{{Name: "all-fields-present", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(ledgerFixture),
		}}},
	})

	Register(&Rule{
		Code: "SDD113", Severity: Error, PyFunc: "_ledger",
		What: "a decision id is not of the form `D-NNNN`",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			eachDecision(a, func(_ map[string]any, id string) {
				if entryField("id").CompiledPattern().MatchString(id) {
					return
				}
				emit(Diagnostic{
					Code: "SDD113", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Invalid decision id `" + id + "`.",
					Correction: "Use `D-NNNN`.",
				})
			})
		},
		Bad: []Example{{Name: "short-id", Files: map[string]string{
			"Decisions/decisions.md": replaceFirst(
				decisionLog(ledgerFixture), "id: D-0001", "id: D-1"),
		}}},
		Good: []Example{{Name: "well-formed-id", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(ledgerFixture),
		}}},
	})

	Register(&Rule{
		Code: "SDD114", Severity: Error, PyFunc: "_ledger",
		What: "a decision declares an unrecognized `kind`",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			eachDecision(a, func(entry map[string]any, id string) {
				if enumAllows("kind", metaStr(entry, "kind")) {
					return
				}
				emit(Diagnostic{
					Code: "SDD114", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Decision `" + id + "` has invalid kind.",
					Correction: "Use an allowed decision kind.",
				})
			})
		},
		Bad: []Example{{Name: "unknown-kind", Files: map[string]string{
			"Decisions/decisions.md": replaceFirst(
				decisionLog(ledgerFixture), "kind: decision", "kind: guess"),
		}}},
		Good: []Example{{Name: "allowed-kind", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(ledgerFixture),
		}}},
	})

	Register(&Rule{
		Code: "SDD115", Severity: Error, PyFunc: "_ledger",
		What: "a decision declares an unrecognized `status`",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			eachDecision(a, func(entry map[string]any, id string) {
				if enumAllows("status", metaStr(entry, "status")) {
					return
				}
				emit(Diagnostic{
					Code: "SDD115", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Decision `" + id + "` has invalid status.",
					Correction: "Use an allowed decision status.",
				})
			})
		},
		Bad: []Example{{Name: "unknown-status", Files: map[string]string{
			"Decisions/decisions.md": replaceFirst(
				decisionLog(ledgerFixture), "status: accepted", "status: maybe"),
		}}},
		Good: []Example{{Name: "allowed-status", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(ledgerFixture),
		}}},
	})

	Register(&Rule{
		Code: "SDD116", Severity: Error, PyFunc: "_ledger",
		What: "an answered-question decision records no `question`",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			eachDecision(a, func(entry map[string]any, id string) {
				get := func(k string) string { return metaStr(entry, k) }
				cond := entryField("question").RequiredWhen
				if cond == nil || !cond.Holds(get) {
					return
				}
				if metaStr(entry, "question") != "" {
					return
				}
				emit(Diagnostic{
					Code: "SDD116", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Answered question `" + id + "` lacks `question`.",
					Correction: "Record the question.",
				})
			})
		},
		Bad: []Example{{Name: "answered-without-question", Files: map[string]string{
			"Decisions/decisions.md": replaceFirst(
				decisionLog(ledgerFixture), "kind: decision", "kind: answered-question"),
		}}},
		Good: []Example{{Name: "answered-with-question", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: answered-question
    status: accepted
    date: 2024-01-01
    decided_by: user
    question: Which thing?
    statement: The first thing.
    rationale: It was the better thing.
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD117", Severity: Error, PyFunc: "_ledger",
		What: "a decision declares an unrecognized `decided_by`",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			eachDecision(a, func(entry map[string]any, id string) {
				if enumAllows("decided_by", metaStr(entry, "decided_by")) {
					return
				}
				emit(Diagnostic{
					Code: "SDD117", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Decision `" + id + "` has invalid `decided_by`.",
					Correction: "Use agent, user, or user-approved as allowed by lifecycle status.",
				})
			})
		},
		Bad: []Example{{Name: "unknown-decided-by", Files: map[string]string{
			"Decisions/decisions.md": replaceFirst(
				decisionLog(ledgerFixture), "decided_by: user", "decided_by: committee"),
		}}},
		Good: []Example{{Name: "allowed-decided-by", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(ledgerFixture),
		}}},
	})

	Register(&Rule{
		Code: "SDD118", Severity: Error, PyFunc: "_ledger",
		What: "a decision attributed to `agent` is not `proposed`",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			eachDecision(a, func(entry map[string]any, id string) {
				get := func(k string) string { return metaStr(entry, k) }
				cond := entryField("decided_by").ForbiddenWhen
				if cond == nil || metaStr(entry, "decided_by") != cond.Value {
					return
				}
				if !cond.Holds(get) {
					return
				}
				emit(Diagnostic{
					Code: "SDD118", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Decision `" + id + "` attributes a non-proposed entry to agent.",
					Correction: "Use agent only for unconfirmed proposals; user acceptance changes provenance to user-approved.",
				})
			})
		},
		Bad: []Example{{Name: "agent-accepted", Files: map[string]string{
			"Decisions/decisions.md": replaceFirst(
				decisionLog(ledgerFixture), "decided_by: user", "decided_by: agent"),
		}}},
		Good: []Example{{Name: "agent-proposed", Files: map[string]string{
			"Decisions/decisions.md": replaceFirst(
				replaceFirst(decisionLog(ledgerFixture), "decided_by: user", "decided_by: agent"),
				"status: accepted", "status: proposed"),
		}}},
	})
}
