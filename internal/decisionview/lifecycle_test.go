package decisionview_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

const overridePath = "Decisions/fork.md"

func TestForkOverrideIndependentAuthority(t *testing.T) {
	t.Run("override derives independently expected effective authority", func(t *testing.T) {
		root, snapshot, parentBefore, localBefore := overrideFixture(t, false)
		preview, err := decisionview.PreviewForkOverride(snapshot, overrideProposal("override", ""))
		if err != nil {
			t.Errorf("preview override: %v", err)
			return
		}
		if preview == nil || preview.Envelope == nil {
			t.Errorf("override preview omitted its exact-byte envelope")
			return
		}

		want := []decisionview.QualifiedID{
			decisionview.QualifiedID("ledger:" + string(adoptionLedger) + ":D-0001"),
			decisionview.QualifiedID("ledger:" + string(adoptionLedger) + ":D-0002"),
		}
		if preview.Envelope.AfterResolution != decisionview.ResolutionComplete || !reflect.DeepEqual(preview.Envelope.Delta.After, want) {
			t.Errorf("override authority = resolution %s ids %v, want independently derived complete authority %v", preview.Envelope.AfterResolution, preview.Envelope.Delta.After, want)
		}
		if preview.Envelope.Operation != "override" || !preview.Envelope.RequiresApproval || preview.Envelope.Status != "proposal" {
			t.Errorf("override preview implied authority without exact approval: %+v", preview.Envelope)
		}
		assertOverrideDecision(t, preview.Decision, "D-0002", "", "replacement rule")
		assertOverrideRoundTrip(t, root, preview.Envelope, "D-0002")
		if !bytes.Equal(snapshot.Collections[adoptionSource].Files[0].Source, parentBefore) || !bytes.Equal(snapshot.Local.Files[0].Source, localBefore) {
			t.Errorf("preview mutated inherited or accepted local history")
		}
	})

	t.Run("reconciliation appends explicit reciprocal successor authority", func(t *testing.T) {
		root, snapshot, parentBefore, localBefore := overrideFixture(t, true)
		old, err := copyJSONMap(snapshot.Local.Entries["D-0002"])
		if err != nil {
			t.Fatal(err)
		}
		preview, err := decisionview.PreviewForkOverride(snapshot, overrideProposal("reconcile", "ledger:"+string(adoptionLedger)+":D-0002"))
		if err != nil {
			t.Errorf("preview reconciliation: %v", err)
			return
		}
		if preview == nil || preview.Envelope == nil {
			t.Errorf("reconciliation preview omitted its exact-byte envelope")
			return
		}
		assertOverrideDecision(t, preview.Decision, "D-0003", "D-0002", "replacement rule")
		assertOverrideRoundTrip(t, root, preview.Envelope, "D-0003")

		after := overrideAfter(preview.Envelope)
		loadedRoot := t.TempDir()
		writeOverrideAfter(t, loadedRoot, after)
		loaded, err := decisionview.LoadCollection(decisionview.Roots{Repository: loadedRoot, Planning: loadedRoot}, adoptionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: overridePath})
		if err != nil {
			t.Errorf("load reconciled exact bytes: %v", err)
			return
		}
		previous := loaded.Entries["D-0002"]
		if previous["status"] != "superseded" || previous["superseded_by"] != "D-0003" || loaded.Entries["D-0003"]["supersedes"] != "D-0002" {
			t.Errorf("reconciliation omitted explicit reciprocal successor links: old=%v new=%v", previous, loaded.Entries["D-0003"])
		}
		for key, value := range old {
			if key == "status" {
				continue
			}
			if !reflect.DeepEqual(previous[key], value) {
				t.Errorf("accepted override history field %q changed: got %#v want %#v", key, previous[key], value)
			}
		}
		if !bytes.Equal(snapshot.Collections[adoptionSource].Files[0].Source, parentBefore) || !bytes.Equal(snapshot.Local.Files[0].Source, localBefore) {
			t.Errorf("reconciliation mutated captured inherited or accepted bytes")
		}
	})

	t.Run("successor has no implicit override authority", func(t *testing.T) {
		_, snapshot, _, _ := overrideFixture(t, true)
		old := snapshot.Local.Entries["D-0002"]
		old["status"] = "superseded"
		old["superseded_by"] = "D-0003"
		next := collectionEntry("D-0003", "accepted", "successor without authority")
		next["supersedes"] = "D-0002"
		snapshot.Local.Entries["D-0003"] = next
		view, err := decisionview.Compose(snapshot.Local.ID, "fork", snapshot.Collections)
		if err != nil {
			t.Fatal(err)
		}
		wantParent := "ledger:" + string(adoptionSource) + ":D-0001"
		if got := resolvedBindingIDs(view); !containsString(got, wantParent) {
			t.Errorf("unlinked successor silently inherited override authority: binding ids %v view=%+v", got, view)
		}
	})
}

func TestForkOverrideHostileEnvelope(t *testing.T) {
	t.Run("missing complete confirmation or scope", func(t *testing.T) {
		_, snapshot, _, _ := overrideFixture(t, false)
		valid := string(overrideProposal("override", ""))
		cases := map[string]string{
			"missing confirmation": removeJSONMember(valid, `,"confirmation":"confirmed replacement obligations"`),
			"missing scope":        removeJSONMember(valid, `,"scope":["internal/decisionview/"]`),
			"empty confirmation":   strings.Replace(valid, `"confirmation":"confirmed replacement obligations"`, `"confirmation":""`, 1),
		}
		for name, raw := range cases {
			t.Run(name, func(t *testing.T) {
				if _, err := decisionview.PreviewForkOverride(snapshot, json.RawMessage(raw)); err == nil {
					t.Errorf("accepted override proposal with %s", name)
				}
			})
		}
	})

	t.Run("duplicate current target and hidden ancestor", func(t *testing.T) {
		_, duplicate, _, _ := overrideFixture(t, true)
		if _, err := decisionview.PreviewForkOverride(duplicate, overrideProposal("override", "")); err == nil {
			t.Errorf("accepted a second current override for one target")
		}

		_, child, grandTarget := hiddenAncestorFixture(t)
		raw := strings.Replace(string(overrideProposal("override", "")), "ledger:"+string(adoptionSource)+":D-0001", grandTarget, 1)
		if _, err := decisionview.PreviewForkOverride(child, json.RawMessage(raw)); err == nil {
			t.Errorf("accepted already-hidden ancestor target %s", grandTarget)
		}
	})

	t.Run("hostile external proposal keys", func(t *testing.T) {
		_, snapshot, _, _ := overrideFixture(t, false)
		valid := string(overrideProposal("override", ""))
		cases := map[string]string{
			"unknown inherited write": strings.TrimSuffix(valid, "}") + `,"writeInheritedSource":true}`,
			"caller allocated id":     strings.TrimSuffix(valid, "}") + `,"id":"D-9000"}`,
			"caller supplied basis":   strings.TrimSuffix(valid, "}") + `,"basis":{"targetId":"ledger:` + string(adoptionSource) + `:D-0001"}}`,
			"duplicate target key":    strings.Replace(valid, `"target":`, `"target":"ledger:`+string(adoptionSource)+`:D-9999","target":`, 1),
			"implicit approval":       strings.TrimSuffix(valid, "}") + `,"approved":true}`,
		}
		for name, raw := range cases {
			t.Run(name, func(t *testing.T) {
				if _, err := decisionview.PreviewForkOverride(snapshot, json.RawMessage(raw)); err == nil {
					t.Errorf("accepted hostile external proposal: %s", name)
				}
			})
		}
	})
}

func overrideFixture(t *testing.T, withOverride bool) (string, decisionview.ForkOverrideSnapshot, []byte, []byte) {
	t.Helper()
	root := t.TempDir()
	parentEntries := []map[string]any{collectionEntry("D-0001", "accepted", "inherited rule")}
	parentRaw := collectionLedger(t, root, "Decisions/parent.md", "active", parentEntries, nil)
	parent, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionSource, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/parent.md"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := decisionview.BindCollection("parent-binding", adoptionSourceOwner, parent)
	if err != nil {
		t.Fatal(err)
	}
	entries := []map[string]any{collectionEntry("D-0001", "accepted", "unrelated local rule")}
	if withOverride {
		basis, err := decisionview.CreateBasis(binding.ID, parent, decisionview.QualifiedID("ledger:"+string(parent.ID)+":D-0001"), []string{binding.ID})
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, overrideEntry(t, "D-0002", "existing replacement", basis, ""))
	}
	metadata := decisionview.ForkMetadata{Version: 1, LedgerID: adoptionLedger, RepositoryID: adoptionOwner, ParentBindingID: binding.ID, Bindings: []decisionview.Binding{binding}}
	localRaw := collectionLedger(t, root, overridePath, "active", entries, metadata)
	local, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: overridePath})
	if err != nil {
		t.Fatal(err)
	}
	collections := map[decisionview.CollectionID]*decisionview.Collection{parent.ID: parent, local.ID: local}
	return root, decisionview.ForkOverrideSnapshot{Local: local, Collections: collections}, append([]byte(nil), parentRaw...), append([]byte(nil), localRaw...)
}

func hiddenAncestorFixture(t *testing.T) (string, decisionview.ForkOverrideSnapshot, string) {
	t.Helper()
	root := t.TempDir()
	grandRaw := collectionLedger(t, root, "Decisions/grand.md", "active", []map[string]any{collectionEntry("D-0001", "accepted", "grand authority")}, nil)
	_ = grandRaw
	grandID := decisionview.CollectionID("55555555-5555-5555-5555-555555555555")
	grand, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, grandID, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/grand.md"})
	if err != nil {
		t.Fatal(err)
	}
	grandBinding, err := decisionview.BindCollection("grand-binding", adoptionSourceOwner, grand)
	if err != nil {
		t.Fatal(err)
	}
	grandTarget := decisionview.QualifiedID("ledger:" + string(grand.ID) + ":D-0001")
	basis, err := decisionview.CreateBasis(grandBinding.ID, grand, grandTarget, []string{grandBinding.ID})
	if err != nil {
		t.Fatal(err)
	}
	parentMeta := decisionview.ForkMetadata{Version: 1, LedgerID: adoptionSource, RepositoryID: adoptionSourceOwner, ParentBindingID: grandBinding.ID, Bindings: []decisionview.Binding{grandBinding}}
	collectionLedger(t, root, "Decisions/parent.md", "active", []map[string]any{overrideEntry(t, "D-0001", "parent replacement", basis, "")}, parentMeta)
	parent, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionSource, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/parent.md"})
	if err != nil {
		t.Fatal(err)
	}
	parentBinding, err := decisionview.BindCollection("parent-binding", adoptionSourceOwner, parent)
	if err != nil {
		t.Fatal(err)
	}
	localMeta := decisionview.ForkMetadata{Version: 1, LedgerID: adoptionLedger, RepositoryID: adoptionOwner, ParentBindingID: parentBinding.ID, Bindings: []decisionview.Binding{parentBinding}}
	collectionLedger(t, root, overridePath, "active", []map[string]any{collectionEntry("D-0001", "accepted", "local rule")}, localMeta)
	local, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: overridePath})
	if err != nil {
		t.Fatal(err)
	}
	collections := map[decisionview.CollectionID]*decisionview.Collection{grand.ID: grand, parent.ID: parent, local.ID: local}
	return root, decisionview.ForkOverrideSnapshot{Local: local, Collections: collections}, string(grandTarget)
}

func overrideProposal(operation, replacement string) json.RawMessage {
	extra := ""
	if replacement != "" {
		extra = fmt.Sprintf(`,"replacement":%q`, replacement)
	}
	return json.RawMessage(fmt.Sprintf(`{"version":1,"operation":%q,"operationId":"override-operation-1","date":"2026-09-09","target":%q%s,"kind":"decision","decidedBy":"user-approved","statement":"replacement rule","rationale":"complete replacement rationale","scope":["internal/decisionview/"],"confirmation":"confirmed replacement obligations","rejected":["retain inherited rule"],"tags":["fork"],"reversibility":"two-way"}`, operation, "ledger:"+string(adoptionSource)+":D-0001", extra))
}

func overrideEntry(t *testing.T, id, statement string, basis decisionview.OverrideBasis, supersedes string) map[string]any {
	t.Helper()
	entry := collectionEntry(id, "accepted", statement)
	entry["decided_by"] = "user-approved"
	entry["confirmation"] = "confirmed replacement obligations"
	entry["scope"] = []string{"internal/decisionview/"}
	if supersedes != "" {
		entry["supersedes"] = supersedes
	}
	raw, err := json.Marshal(decisionview.OverrideDeclaration{Target: basis.TargetID, Basis: basis})
	if err != nil {
		t.Fatal(err)
	}
	var relation map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&relation); err != nil {
		t.Fatal(err)
	}
	// yaml.v3 otherwise emits json.Number as a quoted scalar; the real public
	// loader must receive the schema version as an integer.
	relation["basis"].(map[string]any)["version"] = 1
	entry["override"] = relation
	return entry
}

func assertOverrideDecision(t *testing.T, entry map[string]any, id, supersedes, statement string) {
	t.Helper()
	if entry == nil {
		t.Errorf("preview omitted the complete local replacement decision")
		return
	}
	for _, key := range []string{"kind", "decided_by", "statement", "rationale", "scope", "confirmation", "override"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("replacement %s omitted complete field %s: %v", id, key, entry)
		}
	}
	if entry["id"] != id || entry["statement"] != statement || entry["status"] != "accepted" {
		t.Errorf("replacement decision = %v, want allocated local accepted %s", entry, id)
	}
	if supersedes != "" && entry["supersedes"] != supersedes {
		t.Errorf("replacement %s lacks explicit successor relation to %s", id, supersedes)
	}
	relation, err := json.Marshal(entry["override"])
	if err != nil {
		t.Fatal(err)
	}
	var declaration decisionview.OverrideDeclaration
	if err := json.Unmarshal(relation, &declaration); err != nil {
		t.Errorf("replacement override relation is not canonical: %v", err)
		return
	}
	if declaration.Target != decisionview.QualifiedID("ledger:"+string(adoptionSource)+":D-0001") || declaration.Basis.TargetID != declaration.Target || declaration.Basis.CanonicalHash == "" || len(declaration.Basis.CanonicalContent) == 0 {
		t.Errorf("replacement omitted complete canonical target basis: %+v", declaration)
	}
}

func assertOverrideRoundTrip(t *testing.T, root string, envelope *decisionview.PreviewEnvelope, wantID string) {
	t.Helper()
	after := overrideAfter(envelope)
	if after == "" {
		t.Errorf("preview omitted exact local ledger bytes")
		return
	}
	writeOverrideAfter(t, root, after)
	loaded, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: overridePath})
	if err != nil {
		t.Errorf("public loader rejected exact preview bytes: %v", err)
		return
	}
	if len(loaded.Files) == 0 || !bytes.Equal(loaded.Files[0].Source, []byte(after)) {
		t.Errorf("public-loader roundtrip changed eventual exact output bytes")
	}
	if loaded.Entries[wantID] == nil {
		t.Errorf("public loader did not recover replacement %s", wantID)
	}
}

func overrideAfter(envelope *decisionview.PreviewEnvelope) string {
	if envelope == nil {
		return ""
	}
	for _, change := range envelope.Changes {
		if change.Root == decisionview.SourceRootPlanning && change.Path == overridePath {
			return change.After
		}
	}
	return ""
}

func writeOverrideAfter(t *testing.T, root, after string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(overridePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyJSONMap(value map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func removeJSONMember(raw, member string) string { return strings.Replace(raw, member, "", 1) }

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
