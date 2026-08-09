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
