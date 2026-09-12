package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
)

func TestSDD190CrossPlanHashCollision(t *testing.T) {
	r := &Root{PlanDecisions: []decisions.PlanFile{
		{Plan: "A", Rel: "Plans/A/A-Decisions.json", Entries: []decisions.Entry{{ID: "pd-0c071537", Statement: "decision 1454"}}},
		{Plan: "B", Rel: "Plans/B/B-Decisions.json", Entries: []decisions.Entry{{ID: "pd-0c071537", Statement: "decision 165661"}}},
	}}
	var found bool
	for _, diagnostic := range Run(r) {
		if diagnostic.Code == "SDD190" {
			found = true
		}
	}
	if !found {
		t.Fatal("validator accepted two different statements sharing one pd identity")
	}
}

// The decisions package carries a copy of the DD definition pattern so it
// can extract Design Decisions without importing rules (rules imports it
// for the citation index). The two must never drift.
func TestDesignDecisionPatternParity(t *testing.T) {
	if got, want := decisions.DDPattern().String(), DefinitionPattern("DD").String(); got != want {
		t.Fatalf("DD pattern drift:\n decisions: %s\n rules:     %s", got, want)
	}
}

func TestUnknownAndAmbiguousPlanDecisionCitationsAreDiagnosed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root, planDir string) string
		want  string
	}{
		{
			name: "unknown",
			setup: func(_ *testing.T, _, _ string) string {
				return "pd-deadbeef"
			},
			want: "not recorded",
		},
		{
			name: "ambiguous",
			setup: func(t *testing.T, root, _ string) string {
				statement := "shared"
				entry := planDecisionsFixture(pdEntry(statement, "2026-01-01", ""))
				for _, plan := range []string{"A", "B"} {
					dir := filepath.Join(root, "Plans", plan)
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(decisions.PathFor(dir), []byte(entry), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return decisions.IDFor(statement)
			},
			want: "ambiguous",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			planDir := filepath.Join(root, "Plans", "Sample")
			if err := os.MkdirAll(planDir, 0o755); err != nil {
				t.Fatal(err)
			}
			citation := tc.setup(t, root, planDir)
			plan := strings.Replace(validPlan(false), "## Overview\n\nText.", "## Overview\n\nGoverned by "+citation+".", 1)
			if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte(plan), 0o644); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadRootRepo(root, root)
			if err != nil {
				t.Fatal(err)
			}
			var found *Diagnostic
			for _, d := range Run(loaded) {
				if strings.Contains(d.Message, citation) {
					copy := d
					found = &copy
					break
				}
			}
			if found == nil || !strings.Contains(strings.ToLower(found.Message), tc.want) {
				t.Fatalf("citation %s was not diagnosed as %s: %+v", citation, tc.want, found)
			}
		})
	}
}
