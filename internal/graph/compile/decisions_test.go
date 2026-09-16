package compile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func TestNewSourcesRefusesMalformedPerPlanDecisions(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	other := filepath.Join(root, "Plans", "Other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisions.PathFor(other), []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSources(root, root, "SamplePlan"); err == nil {
		t.Fatal("citation anchoring accepted an incomplete per-plan decisions index")
	}
}

func TestSyncDesignDecisionsCopiesDDsVerbatimAndIsIdempotent(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	first, err := SyncDesignDecisions(root, root, "SamplePlan", "2026-09-11")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Added) != 1 {
		t.Fatalf("added = %+v", first.Added)
	}
	e := first.Added[0]
	if e.Source != "Designs/Sample:DD-1" || !strings.HasPrefix(e.Statement, "**DD-1**: Strict decoding.") || !strings.Contains(e.Statement, "Rationale: drift.") {
		t.Fatalf("entry = %+v", e)
	}
	if e.ID != decisions.IDFor(e.Statement) || e.Date != "2026-09-11" {
		t.Fatalf("entry id/date = %+v", e)
	}
	second, err := SyncDesignDecisions(root, root, "SamplePlan", "2026-09-12")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Added) != 0 || len(second.Skipped) != 1 {
		t.Fatalf("recompile was not idempotent: %+v", second)
	}
	// A new DD in the design lands as exactly one new entry.
	designPath := filepath.Join(root, "Designs", "Sample", "README.md")
	raw, _ := os.ReadFile(designPath)
	os.WriteFile(designPath, append(raw, []byte("- **DD-2**: Second decision.\n  Decision: y.\n")...), 0o644)
	third, err := SyncDesignDecisions(root, root, "SamplePlan", "2026-09-13")
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Added) != 1 || third.Added[0].Source != "Designs/Sample:DD-2" {
		t.Fatalf("extension = %+v", third)
	}
	entries, _ := decisions.Load(decisions.PathFor(filepath.Join(root, "Plans", "SamplePlan")))
	if len(entries) != 2 || entries[0].ID != e.ID {
		t.Fatalf("file = %+v", entries)
	}
}

func TestNodeCitesPlanDecisionAndEmbedsFingerprint(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	planDir := filepath.Join(root, "Plans", "SamplePlan")
	res, err := decisions.Append(decisions.PathFor(planDir), "Audit events carry only the caller tenant id.", "", "", "2026-09-11", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	stage(t, root, strings.Replace(happyProposal, `"justifies": ["FR-01", "D-0001"]`, `"justifies": ["FR-01", "`+res.Entry.ID+`"]`, 1))
	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings: %v", findings)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	// Validate against the stored graph: the decision fingerprint must
	// still resolve (entries are immutable, so it can never go stale).
	findings, err = Validate(root, root, "SamplePlan", g)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("validate findings: %v", findings)
	}
}

func TestUnknownPlanDecisionCitationIsRefused(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	stage(t, root, strings.Replace(happyProposal, `"justifies": ["FR-01", "D-0001"]`, `"justifies": ["FR-01", "pd-deadbeef"]`, 1))
	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Msg, "pd-deadbeef") || !strings.Contains(findings[0].Msg, "sdd decide add") {
		t.Fatalf("findings = %v", findings)
	}
}

func TestQualifiedCrossPlanDecisionCitation(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	// Another plan records a decision; SamplePlan cites it qualified.
	otherDir := filepath.Join(root, "Plans", "Other")
	os.MkdirAll(otherDir, 0o755)
	os.WriteFile(filepath.Join(otherDir, "README.md"), []byte(strings.Replace(fixturePlan, `title: "Sample Plan"`, `title: "Other"`, 1)), 0o644)
	res, err := decisions.Append(decisions.PathFor(otherDir), "Shared constraint from another plan.", "", "", "2026-09-11", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Bare citation from SamplePlan resolves too, since exactly one plan
	// carries the id and the citing plan carries nothing by that id.
	stage(t, root, strings.Replace(happyProposal, `"justifies": ["FR-01", "D-0001"]`, `"justifies": ["FR-01", "Other:`+res.Entry.ID+`", "`+res.Entry.ID+`"]`, 1))
	_, findings, err := Run(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings: %v", findings)
	}
}

func TestSyncRecordsSupersessionDeclaredInDesign(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	designPath := filepath.Join(root, "Designs", "Sample", "README.md")
	raw, _ := os.ReadFile(designPath)
	os.WriteFile(designPath, append(raw, []byte("- **DD-2**: Lenient decoding after all.\n  Supersedes DD-1. Decision: tolerate unknown keys.\n")...), 0o644)
	res, err := SyncDesignDecisions(root, root, "SamplePlan", "2026-09-11")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 2 || res.Added[1].Supersedes != res.Added[0].ID {
		t.Fatalf("added = %+v", res.Added)
	}
	// A dangling Supersedes refuses the sync rather than dropping the edge.
	os.WriteFile(designPath, append(raw, []byte("- **DD-2**: Something.\n  Supersedes DD-9.\n")...), 0o644)
	if _, err := SyncDesignDecisions(root, root, "SamplePlan", "2026-09-11"); err == nil {
		t.Fatal("dangling supersedes accepted")
	}
}

func TestSyncAllowsIdenticalSupersedingDesignDecisionCopiesAcrossPlans(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	designPath := filepath.Join(root, "Designs", "Sample", "README.md")
	raw, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(designPath, append(raw, []byte("- **DD-2**: Replacement.\n  Supersedes DD-1. Decision: replacement.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	otherDir := filepath.Join(root, "Plans", "Other")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "README.md"), []byte(fixturePlan), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncDesignDecisions(root, root, "SamplePlan", "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncDesignDecisions(root, root, "Other", "2026-01-02"); err != nil {
		t.Fatalf("identical design successor could not be copied into a second plan: %v", err)
	}
	entries, err := decisions.Load(decisions.PathFor(otherDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Supersedes != entries[0].ID {
		t.Fatalf("second plan decisions = %+v", entries)
	}
}

func TestSyncRefusesIncompleteDecisionSnapshot(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	badDir := filepath.Join(root, "Plans", "Other")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisions.PathFor(badDir), []byte("[{broken]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncDesignDecisions(root, root, "SamplePlan", "2026-01-01"); err == nil {
		t.Fatal("decision sync mutated against a partial decision snapshot")
	}
	entries, err := decisions.Load(decisions.PathFor(filepath.Join(root, "Plans", "SamplePlan")))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("sync partially appended before refusing incomplete snapshot: %+v", entries)
	}
}
