package rules

import (
	"os"
	"path/filepath"
	"regexp"
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

// orderedDecisions returns every decision in the order the ledgers declare
// them, which repoDecisions' map cannot preserve.
//
// Order is load-bearing for the pairwise rules: Python walks accepted[] with
// an index and compares each entry against those AFTER it, attaching the
// diagnostic to the earlier one's artifact. Iterating a map instead would
// attach it to whichever of the pair happened to come first that run, so the
// reported path would vary between runs of the same input.
func orderedDecisions(r *Root) []struct {
	Key    decisionKey
	Record decisionRecord
} {
	var out []struct {
		Key    decisionKey
		Record decisionRecord
	}
	seen := map[decisionKey]bool{}
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
			if seen[k] {
				continue // first entry wins, as repoDecisions does
			}
			seen[k] = true
			out = append(out, struct {
				Key    decisionKey
				Record decisionRecord
			}{k, decisionRecord{Artifact: a, Entry: m}})
		}
	}
	return out
}

// scopesOverlap ports Validator._scopes_overlap. An empty or absent scope on
// either side means "unscoped", which overlaps everything — so two unscoped
// decisions are always compared. Beyond literal path containment, two scopes
// also overlap when the artifacts they name are connected by `related`.
func scopesOverlap(r *Root, left, right any) bool {
	leftList, leftOK := left.([]any)
	rightList, rightOK := right.([]any)
	if !leftOK || len(leftList) == 0 || !rightOK || len(rightList) == 0 {
		return true
	}
	for _, l := range leftList {
		leftItem, ok := l.(string)
		if !ok {
			continue
		}
		for _, rr := range rightList {
			rightItem, ok := rr.(string)
			if !ok {
				continue
			}
			leftPath := strings.TrimRight(leftItem, "/")
			rightPath := strings.TrimRight(rightItem, "/")
			if leftPath == rightPath ||
				strings.HasPrefix(leftPath, rightPath+"/") ||
				strings.HasPrefix(rightPath, leftPath+"/") {
				return true
			}
			leftArtifact := resolveRelated(r, leftItem)
			rightArtifact := resolveRelated(r, rightItem)
			if leftArtifact != nil && rightArtifact != nil &&
				(artifactsConnected(r, leftArtifact, rightArtifact) ||
					artifactsConnected(r, rightArtifact, leftArtifact)) {
				return true
			}
		}
	}
	return false
}

// chosenRejected ports chosen_rejected(): whether what `chosen` decided is
// something `rejecting` listed as an option it turned down.
func chosenRejected(chosen, rejecting map[string]any) bool {
	statement := normalizedValue(chosen["statement"])
	for _, item := range asAnyList(rejecting["rejected"]) {
		s, ok := item.(string)
		if !ok {
			continue
		}
		n := normalizedValue(s)
		if n != "" && strings.Contains(statement, n) {
			return true
		}
	}
	return false
}

var defineQuestionRe = regexp.MustCompile(`(?:what (?:is|does)|define)\s+(.+?)(?:\?|$)`)
var defineStatementRe = regexp.MustCompile(`^(.+?)\s+(?:means|is defined as|refers to)\s+`)

// definitionTerm ports definition_term(): the term a `definition` decision
// defines, read from its question if it asks one, else from its statement.
func definitionTerm(entry map[string]any) string {
	if metaStr(entry, "kind") != "definition" {
		return ""
	}
	if question := normalizedValue(entry["question"]); question != "" {
		if m := defineQuestionRe.FindStringSubmatch(question); m != nil {
			return strings.Trim(m[1], " `\"'")
		}
	}
	if m := defineStatementRe.FindStringSubmatch(normalizedValue(entry["statement"])); m != nil {
		return strings.Trim(m[1], " `\"'")
	}
	return ""
}

// acceptedPair is one ordered pair of accepted decisions in the same
// repository whose scopes overlap — the input every pairwise rule shares.
type acceptedPair struct {
	Artifact        *Artifact
	LeftID, RightID string
	Left, Right     map[string]any
}

// overlappingAcceptedPairs yields each unordered pair once, in ledger order,
// attributed to the earlier decision's artifact exactly as Python does.
func overlappingAcceptedPairs(r *Root) []acceptedPair {
	var accepted []struct {
		Key    decisionKey
		Record decisionRecord
	}
	for _, d := range orderedDecisions(r) {
		if metaStr(d.Record.Entry, "status") == "accepted" {
			accepted = append(accepted, d)
		}
	}
	var out []acceptedPair
	for i, left := range accepted {
		for _, right := range accepted[i+1:] {
			if left.Key.repo != right.Key.repo {
				continue
			}
			if !scopesOverlap(r, left.Record.Entry["scope"], right.Record.Entry["scope"]) {
				continue
			}
			out = append(out, acceptedPair{
				Artifact: left.Record.Artifact,
				LeftID:   left.Key.id, RightID: right.Key.id,
				Left: left.Record.Entry, Right: right.Record.Entry,
			})
		}
	}
	return out
}

func init() {
	Register(&Rule{
		Code: "SDD147", Severity: Candidate, PyFunc: "_decision_links",
		What: "two accepted decisions answer the same question differently",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, p := range overlappingAcceptedPairs(r) {
				leftQ := normalizedValue(p.Left["question"])
				if leftQ == "" || leftQ != normalizedValue(p.Right["question"]) {
					continue
				}
				if normalizedValue(p.Left["statement"]) == normalizedValue(p.Right["statement"]) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD147", Severity: Candidate, Path: p.Artifact.Rel, Line: 1,
					Message:    "`" + p.LeftID + "` and `" + p.RightID + "` answer the same question differently.",
					Correction: "Judge whether they conflict, refine one another, or have disjoint scope.",
				})
			}
		},
		Bad: []Example{{Name: "same-question-different-answers", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: answered-question
    status: accepted
    date: 2024-01-01
    decided_by: user
    question: Which database?
    statement: We use Postgres.
    rationale: R
  - id: D-0002
    kind: answered-question
    status: accepted
    date: 2024-01-01
    decided_by: user
    question: Which database?
    statement: We use MySQL.
    rationale: R
`),
		}}},
		Good: []Example{{Name: "same-question-same-answer", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: answered-question
    status: accepted
    date: 2024-01-01
    decided_by: user
    question: Which database?
    statement: We use Postgres.
    rationale: R
  - id: D-0002
    kind: answered-question
    status: accepted
    date: 2024-01-01
    decided_by: user
    question: Which database?
    statement: We use Postgres.
    rationale: R
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD148", Severity: Candidate, PyFunc: "_decision_links",
		What: "one accepted decision chose an option another explicitly rejected",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, p := range overlappingAcceptedPairs(r) {
				if !chosenRejected(p.Left, p.Right) && !chosenRejected(p.Right, p.Left) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD148", Severity: Candidate, Path: p.Artifact.Rel, Line: 1,
					Message:    "`" + p.LeftID + "` and `" + p.RightID + "` choose and reject the same option.",
					Correction: "Judge whether they conflict or have disjoint scope.",
				})
			}
		},
		Bad: []Example{{Name: "chose-what-another-rejected", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: We use redis for caching.
    rationale: R
  - id: D-0002
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: We use memcached.
    rationale: R
    rejected: ["redis"]
`),
		}}},
		Good: []Example{{Name: "no-rejected-overlap", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: We use redis for caching.
    rationale: R
  - id: D-0002
    kind: decision
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: We use memcached.
    rationale: R
    rejected: ["hazelcast"]
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD149", Severity: Candidate, PyFunc: "_decision_links",
		What: "two accepted definitions define the same term differently",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, p := range overlappingAcceptedPairs(r) {
				leftTerm := definitionTerm(p.Left)
				if leftTerm == "" || leftTerm != definitionTerm(p.Right) {
					continue
				}
				if normalizedValue(p.Left["statement"]) == normalizedValue(p.Right["statement"]) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD149", Severity: Candidate, Path: p.Artifact.Rel, Line: 1,
					Message:    "`" + p.LeftID + "` and `" + p.RightID + "` define `" + leftTerm + "` differently.",
					Correction: "Judge whether the definitions conflict or have disjoint scope.",
				})
			}
		},
		Bad: []Example{{Name: "same-term-different-definitions", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: definition
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: A tenant means one paying customer org.
    rationale: R
  - id: D-0002
    kind: definition
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: A tenant means one deployment namespace.
    rationale: R
`),
		}}},
		Good: []Example{{Name: "distinct-terms", Files: map[string]string{
			"Decisions/decisions.md": decisionLog(`
  - id: D-0001
    kind: definition
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: A tenant means one paying customer org.
    rationale: R
  - id: D-0002
    kind: definition
    status: accepted
    date: 2024-01-01
    decided_by: user
    statement: A workspace means one project container.
    rationale: R
`),
		}}},
	})
}
