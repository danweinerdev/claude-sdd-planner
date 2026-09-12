package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
)

func writePlanREADME(t *testing.T, root, plan, status string) {
	t.Helper()
	path := filepath.Join(root, "Plans", plan, "README.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	readme := "---\ntitle: \"" + plan + "\"\ntype: plan\nstatus: \"" + status + "\"\n---\n\n# " + plan + "\n"
	if err := os.WriteFile(path, []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendPlanDecision(t *testing.T, root, plan, statement, date, supersedes string) decisions.Entry {
	t.Helper()
	entry, err := decisions.Append(decisions.PathFor(filepath.Join(root, "Plans", plan)), statement, supersedes, "", date, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	return entry.Entry
}

func TestSessionStartInjectsOnlySingleActivePlanDecisions(t *testing.T) {
	root := t.TempDir()
	writePlanREADME(t, root, "Active", "active")
	writePlanREADME(t, root, "Done", "complete")
	writePlanREADME(t, root, "Old", "archived")
	appendPlanDecision(t, root, "Active", "Use the active behavior.", "2026-01-01", "")
	appendPlanDecision(t, root, "Done", "Use the completed behavior.", "2026-01-02", "")
	appendPlanDecision(t, root, "Old", "Use the archived behavior.", "2026-01-03", "")

	ctx := SessionStartContext(root)
	if !strings.Contains(ctx, "Use the active behavior.") {
		t.Fatalf("active plan decision missing: %q", ctx)
	}
	if strings.Contains(ctx, "Use the completed behavior.") || strings.Contains(ctx, "Use the archived behavior.") {
		t.Fatalf("unrelated plan decision leaked into active plan context: %q", ctx)
	}
	if !strings.Contains(ctx, "Active") || !strings.Contains(ctx, "constraints for that plan") {
		t.Fatalf("standing-decision header does not identify plan scope: %q", ctx)
	}
}

func TestSessionStartDoesNotUnionPlansWithoutExactlyOneActivePlan(t *testing.T) {
	for _, tc := range []struct {
		name     string
		statuses map[string]string
	}{
		{name: "no active plan", statuses: map[string]string{"Done": "complete", "Old": "archived"}},
		{name: "multiple active plans", statuses: map[string]string{"One": "active", "Two": "active"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for plan, status := range tc.statuses {
				writePlanREADME(t, root, plan, status)
				appendPlanDecision(t, root, plan, "Decision for "+plan+".", "2026-01-01", "")
			}

			if ctx := SessionStartContext(root); ctx != "" {
				t.Fatalf("ambiguous or absent active plan injected a union: %q", ctx)
			}
		})
	}
}

func TestSessionStartDoesNotGuessWhenPlanFrontmatterIsMalformed(t *testing.T) {
	root := t.TempDir()
	writePlanREADME(t, root, "Active", "active")
	appendPlanDecision(t, root, "Active", "Do not inject this while selection is unsafe.", "2026-01-01", "")
	badPath := filepath.Join(root, "Plans", "Broken", "README.md")
	if err := os.MkdirAll(filepath.Dir(badPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath, []byte("---\ntype: plan\nstatus: [active\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if ctx := SessionStartContext(root); ctx != "" {
		t.Fatalf("malformed plan frontmatter caused a guessed selection: %q", ctx)
	}
}

func TestSessionStartWarnsWithoutPartialStandingDecisionContext(t *testing.T) {
	root := t.TempDir()
	writePlanREADME(t, root, "P", "active")
	writePlanREADME(t, root, "Q", "complete")
	oldPath := decisions.PathFor(filepath.Join(root, "Plans", "P"))
	old, err := decisions.Append(oldPath, "Use the old behavior.", "", "", "2026-01-01", "P", nil)
	if err != nil {
		t.Fatal(err)
	}
	newPath := decisions.PathFor(filepath.Join(root, "Plans", "Q"))
	newEntry := decisions.Entry{ID: decisions.IDFor("Use the replacement."), Date: "2026-01-02", Statement: "Use the replacement.", Supersedes: "P:" + old.Entry.ID}
	raw, err := decisions.Encode([]decisions.Entry{newEntry})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("[{broken]"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := SessionStartContext(root)
	if !strings.Contains(ctx, "Warning") {
		t.Fatalf("incomplete decision snapshot emitted no warning: %q", ctx)
	}
	if strings.Contains(ctx, "Use the old behavior.") || strings.Contains(ctx, "## Standing decisions") {
		t.Fatalf("incomplete decision snapshot was presented as standing context: %q", ctx)
	}
}

func TestSessionStartRespectsCrossPlanSupersession(t *testing.T) {
	root := t.TempDir()
	writePlanREADME(t, root, "Active", "active")
	writePlanREADME(t, root, "Later", "complete")
	old := appendPlanDecision(t, root, "Active", "Use the superseded behavior.", "2026-01-01", "")
	appendPlanDecision(t, root, "Active", "Keep the independent active decision.", "2026-01-01", "")
	_, index, err := decisions.LoadValidatedIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decisions.Append(decisions.PathFor(filepath.Join(root, "Plans", "Later")), "Use the replacement behavior.", "Active:"+old.ID, "", "2026-01-02", "Later", index); err != nil {
		t.Fatal(err)
	}

	ctx := SessionStartContext(root)
	if !strings.Contains(ctx, "Keep the independent active decision.") || strings.Contains(ctx, "Use the superseded behavior.") || strings.Contains(ctx, "Use the replacement behavior.") {
		t.Fatalf("cross-plan supersession was not respected within active-plan scope: %q", ctx)
	}
}
