package rules

// Plan decisions (Designs/PlanDecisions): the per-plan decisions file is a
// record the validator reads but never writes. Three rules: a malformed
// file (SDD190), a live citation of a superseded decision (SDD191,
// advisory), and competing successors after a merge (SDD192).

import (
	"regexp"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
)

var citePDRe = regexp.MustCompile(`\b(?:[A-Za-z0-9][A-Za-z0-9._-]*:)?pd-[0-9a-f]{8}\b`)

func planDecisionsFixture(entries string) string { return "[" + entries + "]\n" }

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
		What: "a plan's decisions file (Plans/<Name>/<Name>-Decisions.json) is malformed",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, f := range r.PlanDecisions {
				if f.Err == nil {
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
				emit(Diagnostic{
					Code: "SDD192", Severity: Error,
					Path: "Plans/" + first.Plan + "/" + first.Plan + "-Decisions.json", Line: 1,
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
}
