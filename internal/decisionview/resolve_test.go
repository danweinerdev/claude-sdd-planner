package decisionview_test

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

const resolvedLocal = "cccccccc-dddd-eeee-ffff-000000000000"

func resolvedFixture(t *testing.T, parentID, localID decisionview.CollectionID) (*decisionview.Collection, *decisionview.Collection) {
	t.Helper()
	parent := continuityCollection()
	parent.ID = parentID
	b, err := decisionview.BindCollection("parent-binding", selectionOwner, parent)
	if err != nil {
		t.Fatal(err)
	}
	basis, err := decisionview.CreateBasis(b.ID, parent, decisionview.QualifiedID("ledger:"+string(parentID)+":D-0001"), []string{b.ID})
	if err != nil {
		t.Fatal(err)
	}
	decl := decisionview.OverrideDeclaration{Target: basis.TargetID, Basis: basis}
	raw, err := json.Marshal(decl)
	if err != nil {
		t.Fatal(err)
	}
	var relation map[string]any
	if err := json.Unmarshal(raw, &relation); err != nil {
		t.Fatal(err)
	}
	// Decode JSON through a number-preserving path so version remains an
	// integer in the canonical entry model rather than a float64.
	relation["basis"].(map[string]any)["version"] = json.Number("1")
	entry := collectionEntry("D-0001", "accepted", "local replacement")
	entry["confirmation"] = "verify local contract"
	entry["override"] = relation
	local := &decisionview.Collection{ID: localID, Locator: decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/fork.md"}, Entries: map[string]map[string]any{"D-0001": entry}, Metadata: &decisionview.ForkMetadata{Version: 1, LedgerID: localID, RepositoryID: selectionOwner, ParentBindingID: b.ID, Bindings: []decisionview.Binding{b}}}
	return parent, local
}
func resolvedBindingIDs(v *decisionview.ResolvedView) []string {
	var out []string
	for _, r := range v.Records {
		if r.Applicability == "binding" {
			out = append(out, string(r.ID))
		}
	}
	sort.Strings(out)
	return out
}
func TestForkEffectiveIndependentSets(t *testing.T) {
	for _, status := range []string{"accepted", "proposed", "rejected", "restored"} {
		t.Run(status, func(t *testing.T) {
			parent, local := resolvedFixture(t, selectionLedger, resolvedLocal)
			want := []string{"ledger:" + resolvedLocal + ":D-0001"}
			if status == "restored" {
				target := decisionview.QualifiedID(want[0])
				local.Metadata.Events = []decisionview.AuthorityEvent{{Version: 1, ID: "restore-1", Kind: decisionview.EventRestore, Date: "2026-09-08", DecidedBy: "user", Target: &target}}
			} else {
				local.Entries["D-0001"]["status"] = status
			}
			if status != "accepted" {
				want = []string{"ledger:" + selectionLedger + ":D-0001"}
			}
			view, err := decisionview.Compose(local.ID, "fork", map[decisionview.CollectionID]*decisionview.Collection{local.ID: local, parent.ID: parent})
			if err != nil {
				t.Fatal(err)
			}
			if view.Resolution != decisionview.ResolutionComplete || !reflect.DeepEqual(resolvedBindingIDs(view), want) {
				t.Fatalf("independent expected %v; got %+v %+v", want, view, resolvedBindingIDs(view))
			}
			if parent.Entries["D-0001"]["status"] != "accepted" {
				t.Fatal("parent source status mutated")
			}
		})
	}
}
func TestForkEffectiveReverseNaturalOrder(t *testing.T) {
	parent, local := resolvedFixture(t, "ffffffff-ffff-ffff-ffff-ffffffffffff", "00000000-0000-0000-0000-000000000001")
	local.Locator.Path = "00-local/fork.md"
	parent.Locator.Path = "99-parent/decisions.md"
	b, err := decisionview.BindCollection("parent-binding", selectionOwner, parent)
	if err != nil {
		t.Fatal(err)
	}
	local.Metadata.Bindings = []decisionview.Binding{b}
	v, err := decisionview.Compose(local.ID, "fork", map[decisionview.CollectionID]*decisionview.Collection{local.ID: local, parent.ID: parent})
	if err != nil {
		t.Fatal(err)
	}
	if v.Resolution != decisionview.ResolutionComplete || !reflect.DeepEqual(resolvedBindingIDs(v), []string{"ledger:" + string(local.ID) + ":D-0001"}) {
		t.Fatalf("natural order overrode parent-first semantics: %+v", v)
	}
}
func TestForkEffectiveStaleAncestorAndDuplicates(t *testing.T) {
	parent, local := resolvedFixture(t, selectionLedger, resolvedLocal)
	parent.Entries["D-0001"]["statement"] = "unapproved parent edit"
	v, err := decisionview.Compose(local.ID, "fork", map[decisionview.CollectionID]*decisionview.Collection{local.ID: local, parent.ID: parent})
	if err != nil {
		t.Fatal(err)
	}
	if v.Resolution == decisionview.ResolutionComplete || len(resolvedBindingIDs(v)) != 0 {
		t.Fatal("invalid parent was hidden under local override")
	}
	parent, local = resolvedFixture(t, selectionLedger, resolvedLocal)
	copy := map[string]any{}
	for k, val := range local.Entries["D-0001"] {
		copy[k] = val
	}
	copy["id"] = "D-0002"
	local.Entries["D-0002"] = copy
	v, err = decisionview.Compose(local.ID, "fork", map[decisionview.CollectionID]*decisionview.Collection{local.ID: local, parent.ID: parent})
	if err != nil {
		t.Fatal(err)
	}
	if v.Resolution == decisionview.ResolutionComplete {
		t.Fatal("duplicate active replacements chose a winner")
	}
}

func TestForkEffectiveDetectsErasedParentMetadata(t *testing.T) {
	grand := continuityCollection()
	gb, err := decisionview.BindCollection("grand-binding", selectionOwner, grand)
	if err != nil {
		t.Fatal(err)
	}
	parent := &decisionview.Collection{ID: resolvedLocal, Locator: decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/parent.md"}, Entries: map[string]map[string]any{}, Metadata: &decisionview.ForkMetadata{Version: 1, LedgerID: resolvedLocal, RepositoryID: selectionOwner, ParentBindingID: gb.ID, Bindings: []decisionview.Binding{gb}}}
	pb, err := decisionview.BindCollection("parent-binding", selectionOwner, parent)
	if err != nil {
		t.Fatal(err)
	}
	// Persist/reload the captured source receipt: omitted empty maps must not
	// invalidate its digest, and erasing metadata must remain detectable.
	raw, err := json.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	var persisted decisionview.Binding
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	childID := decisionview.CollectionID("dddddddd-eeee-ffff-0000-111111111111")
	child := &decisionview.Collection{ID: childID, Locator: decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/child.md"}, Entries: map[string]map[string]any{}, Metadata: &decisionview.ForkMetadata{Version: 1, LedgerID: childID, RepositoryID: selectionOwner, ParentBindingID: pb.ID, Bindings: []decisionview.Binding{persisted}}}
	sources := map[decisionview.CollectionID]*decisionview.Collection{grand.ID: grand, parent.ID: parent, child.ID: child}
	v, err := decisionview.Compose(child.ID, "fork", sources)
	if err != nil || v.Resolution != decisionview.ResolutionComplete || len(resolvedBindingIDs(v)) != 1 {
		t.Fatalf("valid three-level source chain: %+v %v", v, err)
	}
	parent.Metadata = nil
	v, err = decisionview.Compose(child.ID, "fork", sources)
	if err != nil {
		t.Fatal(err)
	}
	if v.Resolution == decisionview.ResolutionComplete || len(resolvedBindingIDs(v)) != 0 {
		t.Fatal("erased parent fork metadata silently dropped inherited constraints")
	}
}
