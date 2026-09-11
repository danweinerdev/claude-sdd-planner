package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func TestForkRepositorySourcePathCollision(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repository")
	planning := filepath.Join(repository, ".plans")
	for _, root := range []string{repository, planning} {
		if err := os.MkdirAll(filepath.Join(root, "Decisions"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	repositoryEntries := []map[string]any{
		forkRulesDecision("D-0001", "accepted", "first repository decision", nil),
		forkRulesDecision("D-0001", "accepted", "duplicate repository decision", nil),
	}
	planningEntries := []map[string]any{
		forkRulesDecision("D-0001", "accepted", "independent planning decision", nil),
	}
	forkRulesWriteLedger(t, repository, "Decisions/decisions.md", "active", repositoryEntries, nil)
	forkRulesWriteLedger(t, planning, "Decisions/decisions.md", "active", planningEntries, nil)

	r, err := LoadRootRepo(planning, repository)
	if err != nil {
		t.Fatal(err)
	}
	repositorySource, err := os.ReadFile(filepath.Join(repository, "Decisions", "decisions.md"))
	if err != nil {
		t.Fatal(err)
	}
	planningSource, err := os.ReadFile(filepath.Join(planning, "Decisions", "decisions.md"))
	if err != nil {
		t.Fatal(err)
	}
	repositoryID := decisionview.CollectionID("55555555-5555-5555-5555-555555555555")
	planningID := decisionview.CollectionID("66666666-6666-6666-6666-666666666666")
	r.DecisionView = &decisionview.ConsumerCapture{Collections: map[decisionview.CollectionID]*decisionview.Collection{
		repositoryID: {
			ID:      repositoryID,
			Locator: decisionview.SourceLocator{Root: decisionview.SourceRootRepository, Path: "Decisions/decisions.md"},
			Files:   []decisionview.CollectionFile{{Path: "Decisions/decisions.md", Source: repositorySource}},
		},
		planningID: {
			ID:      planningID,
			Locator: decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/decisions.md"},
			Files:   []decisionview.CollectionFile{{Path: "Decisions/decisions.md", Source: planningSource}},
		},
	}}

	var got []Diagnostic
	Get("SDD032").CheckRoot(r, func(d Diagnostic) { got = append(got, d) })
	if len(got) != 1 {
		t.Fatalf("repository-local duplicate diagnostics = %+v, want exactly one; equal IDs across collections are legal", got)
	}
	if got[0].Path != "../Decisions/decisions.md" {
		t.Fatalf("repository duplicate diagnostic path = %q, want deterministic source-relative path", got[0].Path)
	}
}

func TestForkRepositorySourceDiagnosticPath(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repository")
	planning := filepath.Join(repository, ".plans")
	for _, root := range []string{repository, planning} {
		if err := os.MkdirAll(filepath.Join(root, "Decisions"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	repositoryDecision := forkRulesDecision("D-0001", "accepted", "repository authority", nil)
	repositoryDecision["scope"] = "not-a-list"
	forkRulesWriteLedger(t, repository, "Decisions/decisions.md", "active", []map[string]any{repositoryDecision}, nil)
	forkRulesWriteLedger(t, planning, "Decisions/decisions.md", "active", []map[string]any{
		forkRulesDecision("D-0001", "accepted", "unrelated planning authority", nil),
	}, nil)

	r, err := LoadRootRepo(planning, repository)
	if err != nil {
		t.Fatal(err)
	}
	collectionID := decisionview.CollectionID("77777777-7777-7777-7777-777777777777")
	source := decisionview.SourceLocator{Root: decisionview.SourceRootRepository, Path: "Decisions/decisions.md"}
	r.DecisionView = &decisionview.ConsumerCapture{View: &decisionview.ResolvedView{
		Resolution: decisionview.ResolutionComplete,
		Records: []decisionview.ResolvedDecision{{
			ID: "ledger:" + decisionview.QualifiedID(collectionID) + ":D-0001", CollectionID: collectionID,
			Original: repositoryDecision, OriginalStatus: "accepted", Applicability: "binding", Source: source,
		}},
	}}

	record := repoDecisions(r)[decisionKey{repo: string(collectionID), id: "D-0001"}]
	wantAbs := filepath.Join(r.RepoRoot, "Decisions", "decisions.md")
	if record.Artifact == nil || filepath.Clean(record.Artifact.AbsPath) != filepath.Clean(wantAbs) {
		got := "<nil>"
		if record.Artifact != nil {
			got = record.Artifact.AbsPath
		}
		t.Fatalf("resolved repository decision source = %q, want %q; planning collision must not alias", got, wantAbs)
	}

	var got []Diagnostic
	Get("SDD143").CheckRoot(r, func(d Diagnostic) { got = append(got, d) })
	if len(got) != 1 || got[0].Path != "../Decisions/decisions.md" {
		t.Fatalf("source diagnostic = %+v, want one deterministic repository-source path", got)
	}
}
