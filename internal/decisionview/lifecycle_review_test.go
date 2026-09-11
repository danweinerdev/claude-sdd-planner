package decisionview_test

import (
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func TestForkOverrideExtendedIDs(t *testing.T) {
	root := t.TempDir()
	parentPath := "Decisions/extended-parent.md"
	collectionLedger(t, root, parentPath, "active", []map[string]any{collectionEntry("D-0001", "accepted", "inherited extended-ID rule")}, nil)
	parent, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionSource, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: parentPath})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := decisionview.BindCollection("extended-parent-binding", adoptionSourceOwner, parent)
	if err != nil {
		t.Fatal(err)
	}
	metadata := decisionview.ForkMetadata{Version: decisionview.Version1, LedgerID: adoptionLedger, RepositoryID: adoptionOwner, ParentBindingID: binding.ID, Bindings: []decisionview.Binding{binding}}
	collectionLedger(t, root, overridePath, "active", []map[string]any{collectionEntry("D-10000", "accepted", "existing five-digit local decision")}, metadata)
	local, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: overridePath})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := decisionview.ForkOverrideSnapshot{Local: local, Collections: map[decisionview.CollectionID]*decisionview.Collection{adoptionSource: parent, adoptionLedger: local}}

	preview, err := decisionview.PreviewForkOverride(snapshot, overrideProposal("override", ""))
	if err != nil {
		t.Fatalf("valid D-10000 prevented fresh local allocation: %v", err)
	}
	if got := preview.Decision["id"]; got != "D-10001" {
		t.Fatalf("allocated decision id = %#v, want D-10001", got)
	}
	assertOverrideRoundTrip(t, root, preview.Envelope, "D-10001")
}
