package rules

import (
	"os"
	"path/filepath"
	"strings"
)

// Family (j): Validator._decision_links — SDD145 (a decision's `scope` entry
// resolves to neither a known artifact nor an on-disk repository path) and
// SDD146 (an artifact a decision's scope resolves to, governed by an accepted
// decision, does not cite it). The full function also carries SDD140-144 and
// the candidate SDD147/148/149 pairwise checks; those are out of scope for
// this pass.

// decisionRecord is one decision-log entry plus the artifact that declared
// it, keyed by (repository, id) the way Validator.decisions is.
type decisionRecord struct {
	Artifact *Artifact
	Entry    map[string]any
}

type decisionKey struct {
	repo string
	id   string
}

// repoDecisions mirrors the population of Validator.decisions: every
// decision-log entry across the root, keyed by (repository, id) — the first
// entry wins a collision, since SDD032 already flags the duplicate.
func repoDecisions(r *Root) map[decisionKey]decisionRecord {
	out := map[decisionKey]decisionRecord{}
	for _, a := range r.Artifacts {
		if a.Meta == nil || a.Kind() != "decision-log" {
			continue
		}
		repoKey := r.RepoForArtifact(a.Rel)
		for _, e := range asAnyList(a.Meta["decisions"]) {
			m := planEntry(e)
			if m == nil {
				continue
			}
			id, ok := m["id"].(string)
			if !ok {
				continue
			}
			k := decisionKey{repoKey, id}
			if _, exists := out[k]; exists {
				continue
			}
			out[k] = decisionRecord{Artifact: a, Entry: m}
		}
	}
	return out
}

func init() {
	Register(&Rule{
		Code: "SDD145", Severity: Error, PyFunc: "_decision_links",
		What: "a decision's `scope` entry resolves to neither a known artifact nor an on-disk repository path",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for key, rec := range repoDecisions(r) {
				scope, ok := rec.Entry["scope"].([]any)
				if !ok {
					continue
				}
				for _, s := range scope {
					reference, ok := s.(string)
					if !ok {
						continue
					}
					if resolveRelated(r, reference) != nil {
						continue
					}
					filesystemPath := filepath.Clean(filepath.Join(key.repo, reference))
					if _, err := os.Stat(filesystemPath); err == nil {
						continue
					}
					emit(Diagnostic{
						Code: "SDD145", Severity: Error, Path: rec.Artifact.Rel, Line: 1,
						Message:    "Decision `" + key.id + "` scope `" + reference + "` does not resolve.",
						Correction: "Point it at an existing artifact or repository path.",
					})
				}
			}
		},
		Bad: []Example{{Name: "unresolved-scope", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    status: accepted
    question: Q
    statement: S
    scope: ["Research/nope.md"]
`),
		}}},
		Good: []Example{{Name: "resolved-scope", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    status: accepted
    question: Q
    statement: S
    scope: ["Research/ok.md"]
`),
			"Research/ok.md": strings.Replace(validResearch, "## Context\n\nText.", "## Context\n\nGoverned by D-0001.", 1),
		}}},
	})

	Register(&Rule{
		Code: "SDD146", Severity: Error, PyFunc: "_decision_links",
		What: "an artifact governed by an accepted decision does not cite it",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for key, rec := range repoDecisions(r) {
				scope, ok := rec.Entry["scope"].([]any)
				if !ok {
					continue
				}
				if metaStr(rec.Entry, "status") != "accepted" {
					continue
				}
				for _, s := range scope {
					reference, ok := s.(string)
					if !ok {
						continue
					}
					target := resolveRelated(r, reference)
					if target == nil || strings.Contains(target.Source, key.id) {
						continue
					}
					emit(Diagnostic{
						Code: "SDD146", Severity: Error, Path: target.Rel, Line: 1,
						Message:    "Artifact is governed by `" + key.id + "` but does not cite it.",
						Correction: "Cite `" + key.id + "` or narrow its scope.",
					})
				}
			}
		},
		Bad: []Example{{Name: "uncited-scope", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    status: accepted
    question: Q
    statement: S
    scope: ["Research/ok.md"]
`),
			"Research/ok.md": validResearch,
		}}},
		Good: []Example{{Name: "cited-scope", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    status: accepted
    question: Q
    statement: S
    scope: ["Research/ok.md"]
`),
			"Research/ok.md": strings.Replace(validResearch, "## Context\n\nText.", "## Context\n\nGoverned by D-0001.", 1),
		}}},
	})
}

// decisionSupersessionFields is Python's (field, reverse) pairing for the
// decision ledger, the same shape reviews use.
var decisionSupersessionFields = [2][2]string{
	{"supersedes", "superseded_by"},
	{"superseded_by", "supersedes"},
}

func init() {
	Register(&Rule{
		Code: "SDD140", Severity: Error, PyFunc: "_decision_links",
		What: "a superseded decision does not name what replaced it",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for key, rec := range repoDecisions(r) {
				if metaStr(rec.Entry, "status") != "superseded" {
					continue
				}
				if metaStr(rec.Entry, "superseded_by") != "" {
					continue
				}
				emit(Diagnostic{
					Code: "SDD140", Severity: Error, Path: rec.Artifact.Rel, Line: 1,
					Message:    "Superseded `" + key.id + "` lacks `superseded_by`.",
					Correction: "Link the accepted replacement.",
				})
			}
		},
		Bad: []Example{{Name: "superseded-without-link", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: superseded
    date: 2024-01-01
    decided_by: user
    statement: S
    rationale: R
`),
		}}},
		Good: []Example{{Name: "superseded-with-link", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: superseded
    date: 2024-01-01
    decided_by: user
    statement: S
    rationale: R
    superseded_by: D-0002
  - id: D-0002
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: S2
    rationale: R2
    supersedes: D-0001
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD141", Severity: Error, PyFunc: "_decision_links",
		What: "a decision's `supersedes`/`superseded_by` names an id no ledger declares",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			decisions := repoDecisions(r)
			for key, rec := range decisions {
				for _, pair := range decisionSupersessionFields {
					field := pair[0]
					value := metaStr(rec.Entry, field)
					if value == "" {
						continue
					}
					// Lookup is scoped to the same repository: two repos may
					// each carry a D-0001 that mean different things.
					if _, ok := decisions[decisionKey{key.repo, value}]; ok {
						continue
					}
					emit(Diagnostic{
						Code: "SDD141", Severity: Error, Path: rec.Artifact.Rel, Line: 1,
						Message:    "Decision `" + key.id + "` " + field + " unknown `" + value + "`.",
						Correction: "Reference an existing decision.",
					})
				}
			}
		},
		Bad: []Example{{Name: "supersedes-unknown", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: S
    rationale: R
    supersedes: D-9999
`),
		}}},
		Good: []Example{{Name: "supersedes-known", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: superseded
    date: 2024-01-01
    decided_by: user
    statement: S
    rationale: R
    superseded_by: D-0002
  - id: D-0002
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: S2
    rationale: R2
    supersedes: D-0001
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD142", Severity: Error, PyFunc: "_decision_links",
		What: "a decision supersession link is not reciprocated by its target",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			decisions := repoDecisions(r)
			for key, rec := range decisions {
				for _, pair := range decisionSupersessionFields {
					field, reverse := pair[0], pair[1]
					value := metaStr(rec.Entry, field)
					if value == "" {
						continue
					}
					target, ok := decisions[decisionKey{key.repo, value}]
					if !ok {
						continue // SDD141 already reported it.
					}
					if metaStr(target.Entry, reverse) == key.id {
						continue
					}
					emit(Diagnostic{
						Code: "SDD142", Severity: Error, Path: rec.Artifact.Rel, Line: 1,
						Message:    "Decision `" + key.id + "` " + field + " link is not reciprocated.",
						Correction: "Add matching `" + reverse + "`.",
					})
				}
			}
		},
		Bad: []Example{{Name: "unreciprocated-supersedes", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: superseded
    date: 2024-01-01
    decided_by: user
    statement: S
    rationale: R
  - id: D-0002
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: S2
    rationale: R2
    supersedes: D-0001
`),
		}}},
		Good: []Example{{Name: "reciprocated-supersedes", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: superseded
    date: 2024-01-01
    decided_by: user
    statement: S
    rationale: R
    superseded_by: D-0002
  - id: D-0002
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: S2
    rationale: R2
    supersedes: D-0001
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD143", Severity: Error, PyFunc: "_decision_links",
		What: "a decision's `scope` is present but not a list",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for key, rec := range repoDecisions(r) {
				scope, present := rec.Entry["scope"]
				if !present {
					continue
				}
				if _, ok := scope.([]any); ok {
					continue
				}
				// Python's `entry.get("scope", [])` defaults to a list, so an
				// absent scope never reaches this check; a null one does not
				// either, since None is not a list but `get` returned it —
				// matching that means only a present non-list value fires.
				if scope == nil {
					continue
				}
				emit(Diagnostic{
					Code: "SDD143", Severity: Error, Path: rec.Artifact.Rel, Line: 1,
					Message:    "Decision `" + key.id + "` scope is not a list.",
					Correction: "Use a YAML list.",
				})
			}
		},
		Bad: []Example{{Name: "scope-not-a-list", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: S
    rationale: R
    scope: "Research/ok.md"
`),
		}}},
		Good: []Example{{Name: "scope-is-a-list", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: proposed
    date: 2024-01-01
    decided_by: user
    statement: S
    rationale: R
    scope: []
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD144", Severity: Error, PyFunc: "_decision_links",
		What: "a decision's `scope` contains a non-string entry",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for key, rec := range repoDecisions(r) {
				scope, ok := rec.Entry["scope"].([]any)
				if !ok {
					continue
				}
				for _, s := range scope {
					if _, isStr := s.(string); isStr {
						continue
					}
					emit(Diagnostic{
						Code: "SDD144", Severity: Error, Path: rec.Artifact.Rel, Line: 1,
						Message:    "Decision `" + key.id + "` has a non-string scope.",
						Correction: "Use repository-relative paths.",
					})
				}
			}
		},
		Bad: []Example{{Name: "non-string-scope", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: S
    rationale: R
    scope:
      - ["nested"]
`),
		}}},
		Good: []Example{{Name: "string-scope", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: proposed
    date: 2024-01-01
    decided_by: user
    statement: S
    rationale: R
    scope: []
`),
		}}},
	})
}
