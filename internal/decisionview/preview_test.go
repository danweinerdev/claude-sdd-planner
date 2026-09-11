package decisionview_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func previewView(ids ...string) *decisionview.ResolvedView {
	v := &decisionview.ResolvedView{Version: 1, OwnerID: selectionOwner, LocalID: resolvedLocal, Resolution: decisionview.ResolutionComplete}
	for _, id := range ids {
		v.Records = append(v.Records, decisionview.ResolvedDecision{ID: decisionview.QualifiedID(id), Applicability: "binding"})
	}
	return v
}
func previewExample(t *testing.T) *decisionview.PreviewEnvelope {
	t.Helper()
	old := "ledger:" + selectionLedger + ":D-0001"
	next := "ledger:" + resolvedLocal + ":D-0001"
	p, err := decisionview.NewPreviewEnvelope("override", "operation-1", "2026-09-08", json.RawMessage(`{"statement":"yes\n\"no\" 雪"}`), []decisionview.PreviewFileChange{{Root: decisionview.SourceRootPlanning, Path: "Decisions/fork.md", BeforeExists: true, Before: "old\n", After: "exact \"approved\"\nbytes 雪\n"}}, nil, previewView(old), previewView(next))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestForkPreviewIndependentAuthorityDelta(t *testing.T) {
	p := previewExample(t)
	if !reflect.DeepEqual(p.Delta.Removed, []decisionview.QualifiedID{"ledger:" + selectionLedger + ":D-0001"}) || !reflect.DeepEqual(p.Delta.Added, []decisionview.QualifiedID{"ledger:" + resolvedLocal + ":D-0001"}) {
		t.Fatalf("independent authority difference: %+v", p.Delta)
	}
	if p.BeforeResolution != decisionview.ResolutionComplete || p.AfterResolution != decisionview.ResolutionComplete {
		t.Fatal("resolution state lost")
	}
}
func TestForkPreviewExactHostileEnvelope(t *testing.T) {
	p := previewExample(t)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decisionview.DecodePreviewEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Changes[0].After != "exact \"approved\"\nbytes 雪\n" || got.Changes[0].Before != "old\n" {
		t.Fatal("exact persisted bytes changed during envelope round trip")
	}
	if err := decisionview.VerifyPreviewEnvelope(got, p.Digest); err != nil {
		t.Fatal(err)
	}
	got.Changes[0].After += "unapproved"
	if err := decisionview.VerifyPreviewEnvelope(got, p.Digest); err == nil {
		t.Fatal("changed exact text retained approval digest")
	}
}
func TestForkPreviewNoWriteAndNoImplicitAuthority(t *testing.T) {
	p := previewExample(t)
	if p.Status != "proposal" || !p.RequiresApproval {
		t.Fatal("content digest was represented as human approval")
	}
	if err := decisionview.VerifyPreviewEnvelope(p, ""); err == nil {
		t.Fatal("missing explicit approval digest accepted")
	}
	p.Status = "approved"
	if err := decisionview.VerifyPreviewEnvelope(p, p.Digest); err == nil {
		t.Fatal("self-asserted approval state accepted")
	}
	_, err := decisionview.NewPreviewEnvelope("override", "op", "2026-09-08", json.RawMessage(`{}`), []decisionview.PreviewFileChange{{Root: decisionview.SourceRootRepository, Path: "unrelated.txt", After: "bad"}}, nil, previewView(), previewView())
	if err == nil {
		t.Fatal("repo-root file outside the existing config was writable")
	}
	_, err = decisionview.NewPreviewEnvelope("override", "op", "2026-09-08", json.RawMessage(`{}`), []decisionview.PreviewFileChange{{Root: decisionview.SourceRootPlanning, Path: "Decisions/decisions.md", After: "bad"}}, []decisionview.PreviewSource{{Root: decisionview.SourceRootPlanning, Path: "Decisions/decisions.md", Digest: "sha256:" + strings.Repeat("0", 64)}}, previewView(), previewView())
	if err == nil {
		t.Fatal("inherited read-only source was included in writes")
	}
}
