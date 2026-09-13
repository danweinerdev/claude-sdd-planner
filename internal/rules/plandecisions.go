package rules

// Plan decisions (Designs/PlanDecisions): the per-plan decisions file is a
// record the validator reads but never writes. Four rules: a malformed
// file (SDD190), a live citation of a superseded decision (SDD191,
// advisory), competing successors after a merge (SDD192), and unresolved or
// ambiguous Markdown citations (SDD193).

import (
	"regexp"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
)

var citePDRe = regexp.MustCompile(`\b(?:[A-Za-z0-9][A-Za-z0-9._-]*:)?pd-[0-9a-f]{8}\b`)

// citeRetiredRe is the retired global-ledger id family. Live plans and phases
// must not cite it: the ledger no longer exists, so the citation resolves to
// nothing. Frozen and archived content keeps it as historical text.
var citeRetiredRe = regexp.MustCompile(`\bD-\d{4,9}\b`)

func planDecisionsFixture(entries string) string { return "[" + entries + "]\n" }

// openWork reports whether a plan or phase still has work ahead of it: the
// owning plan is not complete or archived, and a phase is not itself
// complete. Anything else is frozen history and keeps the ids it carried.
func openWork(r *Root, a *Artifact) bool {
	plan := a
	if a.Kind() == "phase" {
		if name := metaStr(a.Meta, "plan"); name != "" {
			if p, ok := r.ByPath["Plans/"+name+"/README.md"]; ok {
				plan = p
			}
		}
		if a.Status() == "complete" {
			return false
		}
	}
	switch plan.Status() {
	case "complete", "archived":
		return false
	}
	return true
}

func pdEntry(statement, date, supersedes string) string {
	s := `{"id":"` + decisions.IDFor(statement) + `","date":"` + date + `","statement":"` + statement + `"`
	if supersedes != "" {
		s += `,"supersedes":"` + supersedes + `"`
	}
	return s + "}"
}

func init() {
	Register(&Rule{
		Code: "SDD190", Severity: Error, Native: true,
		What: "a plan's decisions file is malformed or its decision identity collides with another plan",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			seen := map[string]decisions.Located{}
			for _, f := range r.PlanDecisions {
				if f.Err == nil {
					for _, entry := range f.Entries {
						if prior, exists := seen[entry.ID]; exists && decisions.Normalize(prior.Entry.Statement) != decisions.Normalize(entry.Statement) {
							emit(Diagnostic{
								Code: "SDD190", Severity: Error, Path: f.Rel, Line: 1,
								Message:    "Decision identity `" + entry.ID + "` names a different statement in plan `" + prior.Plan + "`.",
								Correction: "Stop and reconcile the colliding records; do not silently merge different statements or select one by plan order.",
							})
						} else if !exists {
							seen[entry.ID] = decisions.Located{Plan: f.Plan, Entry: entry}
						}
					}
					continue
				}
				emit(Diagnostic{
					Code: "SDD190", Severity: Error, Path: f.Rel, Line: 1,
					Message:    f.Err.Error(),
					Correction: "Restore the file to the canonical five-field array; entries are immutable — supersede, never edit.",
				})
			}
		},
		Bad: []Example{{Name: "malformed-decisions-file", Files: map[string]string{
			"Plans/Sample/README.md":             validPlan(false),
			"Plans/Sample/Sample-Decisions.json": `[{"id":"nope"}]`,
		}}},
		Good: []Example{{Name: "canonical-decisions-file", Files: map[string]string{
			"Plans/Sample/README.md":             validPlan(false),
			"Plans/Sample/Sample-Decisions.json": planDecisionsFixture(pdEntry("x", "2026-01-01", "")),
		}}},
	})

	Register(&Rule{
		Code: "SDD191", Severity: Candidate, Native: true,
		What: "a live artifact cites a superseded plan decision",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			if r.DecisionIndex == nil {
				return
			}
			for _, a := range r.Artifacts {
				if a.Meta == nil || !isLiveArtifact(a) {
					continue
				}
				if kind := a.Kind(); kind != "plan" && kind != "phase" {
					continue
				}
				// A completed or archived plan's text is history: the
				// decisions it cites were current when it closed
				// (PlanDecisions DD-12). Only open work is asked to move.
				if !openWork(r, a) {
					continue
				}
				index := BuildCitationIndex(r, a)
				body := citationBody(a)
				seen := map[string]bool{}
				for _, cited := range citePDRe.FindAllString(body, -1) {
					if seen[cited] {
						continue
					}
					seen[cited] = true
					hit, ok := index.Resolve(cited)
					if !ok {
						continue
					}
					succ, gone := index.DecisionSuccessor(hit.ID)
					if !gone {
						continue
					}
					emit(Diagnostic{
						Code: "SDD191", Severity: Candidate, Path: a.Rel, Line: citationLine(a, cited),
						Message:    "Live artifact cites plan decision `" + cited + "`, superseded by `" + succ.Plan + ":" + succ.Entry.ID + "`.",
						Correction: "Cite the successor, or record why the earlier decision still governs this work.",
					})
				}
			}
		},
		Bad: []Example{{Name: "cites-superseded", Files: map[string]string{
			"Plans/Sample/README.md": strReplace(validPlan(false), "## Overview\n\nText.", "## Overview\n\nGoverned by "+decisions.IDFor("old")+"."),
			"Plans/Sample/Sample-Decisions.json": planDecisionsFixture(
				pdEntry("old", "2026-01-01", "") + "," + pdEntry("new", "2026-01-02", decisions.IDFor("old"))),
		}}},
		Good: []Example{{Name: "cites-current", Files: map[string]string{
			"Plans/Sample/README.md": strReplace(validPlan(false), "## Overview\n\nText.", "## Overview\n\nGoverned by "+decisions.IDFor("new")+"."),
			"Plans/Sample/Sample-Decisions.json": planDecisionsFixture(
				pdEntry("old", "2026-01-01", "") + "," + pdEntry("new", "2026-01-02", decisions.IDFor("old"))),
		}}},
	})

	Register(&Rule{
		Code: "SDD192", Severity: Error, Native: true,
		What: "two plans supersede the same decision",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			if r.DecisionIndex == nil {
				return
			}
			for _, c := range r.DecisionIndex.Conflicts() {
				var names []string
				for _, s := range c.Successors {
					names = append(names, s.Plan+":"+s.Entry.ID)
				}
				sort.Strings(names)
				first := c.Successors[0]
				var implicated []string
				for _, s := range c.Successors {
					implicated = append(implicated, "Plans/"+s.Plan+"/"+s.Plan+"-Decisions.json", "Plans/"+s.Plan+"/README.md")
				}
				for _, l := range r.DecisionIndex.EntriesWithID(c.ID) {
					implicated = append(implicated, "Plans/"+l.Plan+"/"+l.Plan+"-Decisions.json", "Plans/"+l.Plan+"/README.md")
				}
				sort.Strings(implicated)
				emit(Diagnostic{
					Code: "SDD192", Severity: Error,
					Path: "Plans/" + first.Plan + "/" + first.Plan + "-Decisions.json", Line: 1,
					Implicated: implicated,
					Message:    "Decision `" + c.ID + "` has competing successors: " + strings.Join(names, ", ") + ".",
					Correction: "Record one reconciling decision whose --supersedes lists every competing successor.",
				})
			}
		},
		Bad: []Example{{Name: "competing-successors", Files: map[string]string{
			"Plans/Q/README.md":        validPlan(false),
			"Plans/Q/Q-Decisions.json": planDecisionsFixture(pdEntry("base", "2026-01-01", "")),
			"Plans/P/README.md":        validPlan(false),
			"Plans/P/P-Decisions.json": planDecisionsFixture(pdEntry("p wins", "2026-01-02", "Q:"+decisions.IDFor("base"))),
			"Plans/R/README.md":        validPlan(false),
			"Plans/R/R-Decisions.json": planDecisionsFixture(pdEntry("r wins", "2026-01-02", "Q:"+decisions.IDFor("base"))),
		}}},
		Good: []Example{{Name: "reconciled", Files: map[string]string{
			"Plans/Q/README.md": validPlan(false),
			"Plans/Q/Q-Decisions.json": planDecisionsFixture(
				pdEntry("base", "2026-01-01", "") + "," +
					pdEntry("reconciled", "2026-01-03", "P:"+decisions.IDFor("p wins")+", R:"+decisions.IDFor("r wins"))),
			"Plans/P/README.md":        validPlan(false),
			"Plans/P/P-Decisions.json": planDecisionsFixture(pdEntry("p wins", "2026-01-02", "Q:"+decisions.IDFor("base"))),
			"Plans/R/README.md":        validPlan(false),
			"Plans/R/R-Decisions.json": planDecisionsFixture(pdEntry("r wins", "2026-01-02", "Q:"+decisions.IDFor("base"))),
		}}},
	})

	Register(&Rule{
		Code: "SDD193", Severity: Error, Native: true,
		What: "a live plan or phase contains an unresolved, ambiguous, or retired-ledger decision citation",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			authority, err := r.ValidatedDecisionIndex()
			if err != nil {
				// SDD190 owns malformed authority files. Avoid deriving
				// additional unknown-citation claims from a partial snapshot.
				return
			}
			for _, a := range r.Artifacts {
				if a.Meta == nil || !isLiveArtifact(a) {
					continue
				}
				if kind := a.Kind(); kind != "plan" && kind != "phase" {
					continue
				}
				index := BuildCitationIndex(r, a)
				index.decisions = authority
				body := citationBody(a)
				retiredSeen := map[string]bool{}
				// Completed plans and phases are frozen history: their text
				// keeps the ids it carried. Only open work must move to
				// pd- citations.
				for _, cited := range citeRetiredRe.FindAllString(body, -1) {
					if !openWork(r, a) {
						break
					}
					if retiredSeen[cited] {
						continue
					}
					retiredSeen[cited] = true
					emit(Diagnostic{
						Code: "SDD193", Severity: Error, Path: a.Rel, Line: citationLine(a, cited),
						Message:    "Live artifact cites retired global-ledger id `" + cited + "`; the ledger no longer exists.",
						Correction: "Record the decision with `sdd decide add` and cite its pd- id, or drop the citation.",
					})
				}
				seen := map[string]bool{}
				for _, cited := range citePDRe.FindAllString(body, -1) {
					if seen[cited] {
						continue
					}
					seen[cited] = true
					if _, ok := index.Resolve(cited); ok {
						continue
					}
					if ambiguous := index.Ambiguous(cited); len(ambiguous) > 0 {
						emit(Diagnostic{
							Code: "SDD193", Severity: Error, Path: a.Rel, Line: citationLine(a, cited),
							Message:    "Plan-decision citation `" + cited + "` is ambiguous; it is carried by " + strings.Join(ambiguous, ", ") + ".",
							Correction: "Replace the bare citation with one of the listed plan-qualified spellings.",
						})
						continue
					}
					emit(Diagnostic{
						Code: "SDD193", Severity: Error, Path: a.Rel, Line: citationLine(a, cited),
						Message:    "Plan-decision citation `" + cited + "` is not recorded in any plan under the planning root.",
						Correction: "Correct the citation or record the exact user-approved decision with `sdd decide add`.",
					})
				}
			}
		},
		Bad: []Example{
			{Name: "retired-ledger-citation", Files: map[string]string{
				"Plans/Sample/README.md": strReplace(validPlan(false), "## Overview\n\nText.", "## Overview\n\nGoverned by D-0024."),
			}},
			{Name: "unknown-plan-decision-citation", Files: map[string]string{
				"Plans/Sample/README.md": strReplace(validPlan(false), "## Overview\n\nText.", "## Overview\n\nGoverned by pd-deadbeef."),
			}},
			{Name: "ambiguous-plan-decision-citation", Files: map[string]string{
				"Plans/Sample/README.md":   strReplace(validPlan(false), "## Overview\n\nText.", "## Overview\n\nGoverned by "+decisions.IDFor("shared")+"."),
				"Plans/A/A-Decisions.json": planDecisionsFixture(pdEntry("shared", "2026-01-01", "")),
				"Plans/B/B-Decisions.json": planDecisionsFixture(pdEntry("shared", "2026-01-01", "")),
			}},
		},
		Good: []Example{{Name: "resolved-plan-decision-citation", Files: map[string]string{
			"Plans/Sample/README.md":             strReplace(validPlan(false), "## Overview\n\nText.", "## Overview\n\nGoverned by "+decisions.IDFor("current")+"."),
			"Plans/Sample/Sample-Decisions.json": planDecisionsFixture(pdEntry("current", "2026-01-01", "")),
		}}},
	})
}
