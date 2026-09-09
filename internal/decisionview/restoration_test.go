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

func TestForkRestoreIndependentAuthority(t *testing.T) {
	t.Run("append-only restoration retains accepted history and uses current lineage", func(t *testing.T) {
		root, snapshot, parentBefore, localBefore := restorationFixture(t, true, false)
		footprintBefore := restorationTree(t, root)
		originalOverride, err := copyJSONMap(snapshot.Local.Entries["D-0001"])
		if err != nil {
			t.Fatal(err)
		}
		preview, err := decisionview.PreviewForkRestoration(snapshot, restorationProposal("restore"))
		assertRestorationPreviewDidNotWrite(t, root, footprintBefore, parentBefore, localBefore)
		if err != nil {
			t.Errorf("preview stale restoration: %v", err)
			return
		}
		if preview == nil || preview.Envelope == nil {
			t.Errorf("restoration omitted the exact proposal envelope")
			return
		}

		localOverride := decisionview.QualifiedID("ledger:" + string(adoptionLedger) + ":D-0001")
		storedTarget := decisionview.QualifiedID("ledger:" + string(adoptionSource) + ":D-0001")
		currentTarget := decisionview.QualifiedID("ledger:" + string(adoptionSource) + ":D-0002")
		if preview.Restored != localOverride || preview.StoredTarget != storedTarget {
			t.Errorf("restoration identities = replacement %q target %q, want retained historical relation %q -> %q", preview.Restored, preview.StoredTarget, localOverride, storedTarget)
		}
		if !reflect.DeepEqual(preview.CurrentParent, []decisionview.QualifiedID{currentTarget}) || !reflect.DeepEqual(preview.Successors, []decisionview.QualifiedID{currentTarget}) {
			t.Errorf("stale restoration context = parent %v successors %v, want current parent/successor %q", preview.CurrentParent, preview.Successors, currentTarget)
		}
		if preview.Envelope.Operation != "restore" || preview.Envelope.Status != "proposal" || !preview.Envelope.RequiresApproval {
			t.Errorf("restoration inferred approval instead of returning a proposal: %+v", preview.Envelope)
		}
		if got := restorationChangePaths(preview.Envelope); !reflect.DeepEqual(got, []string{"planning:" + overridePath}) {
			t.Errorf("restoration changed paths = %v, want only the canonical local fork ledger", got)
		}

		after := restorationAfter(preview.Envelope, decisionview.SourceRootPlanning, overridePath)
		loadedRoot := t.TempDir()
		writeOverrideAfter(t, loadedRoot, after)
		loaded, err := decisionview.LoadCollection(decisionview.Roots{Repository: loadedRoot, Planning: loadedRoot}, adoptionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: overridePath})
		if err != nil {
			t.Errorf("real collection loader rejected restoration bytes: %v", err)
			return
		}
		original := loaded.Entries["D-0001"]
		if !reflect.DeepEqual(original, originalOverride) {
			t.Errorf("restoration rewrote the original accepted override:\n got: %v\nwant: %v", original, originalOverride)
		}
		last := loaded.Metadata.Events[len(loaded.Metadata.Events)-1]
		if last.Kind != decisionview.EventRestore || last.Target == nil || *last.Target != localOverride {
			t.Errorf("restoration did not append an explicit authority event for %q: %+v", localOverride, last)
		}
		collections := map[decisionview.CollectionID]*decisionview.Collection{adoptionSource: snapshot.Collections[adoptionSource], adoptionLedger: loaded}
		view, err := decisionview.Compose(adoptionLedger, "fork", collections)
		if err != nil {
			t.Errorf("compose restored authority: %v", err)
			return
		}
		if view.Resolution != decisionview.ResolutionComplete || !reflect.DeepEqual(resolvedBindingIDs(view), []string{"ledger:" + string(adoptionLedger) + ":D-0002", string(currentTarget)}) {
			t.Errorf("restored authority = resolution %s ids %v, want current parent plus unrelated local authority", view.Resolution, resolvedBindingIDs(view))
		}
		for _, record := range view.Records {
			if record.ID == localOverride && (record.OriginalStatus != "accepted" || record.Applicability != "inactive-override") {
				t.Errorf("restored accepted override = status %q applicability %q, want accepted inactive-override", record.OriginalStatus, record.Applicability)
			}
		}
	})

	t.Run("explicit detachment retains identity history and enumerates removed authority", func(t *testing.T) {
		root, snapshot, parentBefore, localBefore := restorationFixture(t, false, true)
		footprintBefore := restorationTree(t, root)
		preview, err := decisionview.PreviewForkRestoration(snapshot, restorationProposal("detach"))
		assertRestorationPreviewDidNotWrite(t, root, footprintBefore, parentBefore, localBefore)
		if err != nil {
			t.Errorf("preview detachment after restoration: %v", err)
			return
		}
		if preview == nil || preview.Envelope == nil {
			t.Errorf("detachment omitted the exact proposal envelope")
			return
		}
		parentID := decisionview.QualifiedID("ledger:" + string(adoptionSource) + ":D-0001")
		localID := decisionview.QualifiedID("ledger:" + string(adoptionLedger) + ":D-0002")
		if !reflect.DeepEqual(preview.Removed, []decisionview.QualifiedID{parentID}) || !reflect.DeepEqual(preview.Retained, []decisionview.QualifiedID{localID}) {
			t.Errorf("detachment impact = removed %v retained %v, want inherited %q removed and local %q retained", preview.Removed, preview.Retained, parentID, localID)
		}
		if preview.Config.Mode != "detached" || preview.Config.Path != overridePath || preview.Config.LedgerID != adoptionLedger {
			t.Errorf("detached config did not retain selected path and ledger identity: %+v", preview.Config)
		}
		if preview.Metadata.RepositoryID != adoptionOwner || preview.Metadata.LedgerID != adoptionLedger || len(preview.Metadata.Bindings) != 1 || len(preview.Metadata.Events) < 2 {
			t.Errorf("detachment erased owner, binding, or authority history: %+v", preview.Metadata)
		}
		wantPaths := []string{"planning:" + overridePath, "repository:planning-config.json"}
		if got := restorationChangePaths(preview.Envelope); !reflect.DeepEqual(got, wantPaths) {
			t.Errorf("detachment changed paths = %v, want exact local ledger and config migration %v", got, wantPaths)
		}
		configAfter := restorationAfter(preview.Envelope, decisionview.SourceRootRepository, "planning-config.json")
		if !strings.Contains(configAfter, `"mode":"detached"`) || !strings.Contains(configAfter, `"repositoryId":"`+string(adoptionOwner)+`"`) || !strings.Contains(configAfter, `"unchanged":{"enabled":true}`) {
			t.Errorf("detached config bytes lost explicit identity or unrelated history: %s", configAfter)
		}
	})
}

func TestForkRestoreHostileEnvelope(t *testing.T) {
	t.Run("active and stale overrides block detachment", func(t *testing.T) {
		for _, stale := range []bool{false, true} {
			root, snapshot, parentBefore, localBefore := restorationFixture(t, stale, false)
			footprintBefore := restorationTree(t, root)
			_, err := decisionview.PreviewForkRestoration(snapshot, restorationProposal("detach"))
			assertRestorationPreviewDidNotWrite(t, root, footprintBefore, parentBefore, localBefore)
			if err == nil {
				t.Errorf("detach accepted %s override without explicit restoration", map[bool]string{false: "active", true: "stale"}[stale])
			} else if !strings.Contains(strings.ToLower(err.Error()), "override") {
				t.Errorf("detach refusal for %s override did not identify the blocking authority: %v", map[bool]string{false: "active", true: "stale"}[stale], err)
			}
		}
	})

	t.Run("malformed unknown duplicate and inferred approval fields are refused", func(t *testing.T) {
		_, snapshot, _, _ := restorationFixture(t, true, false)
		valid := string(restorationProposal("restore"))
		cases := map[string]string{
			"malformed":              strings.TrimSuffix(valid, "}"),
			"unknown current target": strings.TrimSuffix(valid, "}") + `,"currentTarget":"ledger:` + string(adoptionSource) + `:D-0002"}`,
			"unknown source write":   strings.TrimSuffix(valid, "}") + `,"writeInheritedSource":true}`,
			"duplicate replacement":  strings.Replace(valid, `"replacement":`, `"replacement":"ledger:`+string(adoptionLedger)+`:D-9999","replacement":`, 1),
			"caller approval flag":   strings.TrimSuffix(valid, "}") + `,"approved":true}`,
			"missing decidedBy":      strings.Replace(valid, `,"decidedBy":"user-approved"`, "", 1),
			"agent approval":         strings.Replace(valid, `"decidedBy":"user-approved"`, `"decidedBy":"agent"`, 1),
			"missing confirmation":   strings.Replace(valid, `,"confirmation":"restore this exact accepted override relation"`, "", 1),
		}
		for name, raw := range cases {
			t.Run(name, func(t *testing.T) {
				if _, err := decisionview.PreviewForkRestoration(snapshot, json.RawMessage(raw)); err == nil {
					t.Errorf("accepted hostile restoration envelope: %s", name)
				}
			})
		}
	})
}

func restorationFixture(t *testing.T, stale, restored bool) (string, decisionview.ForkRestorationSnapshot, []byte, []byte) {
	t.Helper()
	root := t.TempDir()
	parentPath := "Decisions/parent.md"
	initial := []map[string]any{collectionEntry("D-0001", "accepted", "original inherited constraint")}
	collectionLedger(t, root, parentPath, "active", initial, nil)
	parent, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionSource, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: parentPath})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := decisionview.BindCollection("parent-binding", adoptionSourceOwner, parent)
	if err != nil {
		t.Fatal(err)
	}
	target := decisionview.QualifiedID("ledger:" + string(adoptionSource) + ":D-0001")
	basis, err := decisionview.CreateBasis(binding.ID, parent, target, []string{binding.ID})
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		old := collectionEntry("D-0001", "superseded", "original inherited constraint")
		old["superseded_by"] = "D-0002"
		next := collectionEntry("D-0002", "accepted", "current inherited successor")
		next["supersedes"] = "D-0001"
		collectionLedger(t, root, parentPath, "active", []map[string]any{old, next}, nil)
		parent, err = decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionSource, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: parentPath})
		if err != nil {
			t.Fatal(err)
		}
	}
	entries := []map[string]any{overrideEntry(t, "D-0001", "local replacement", basis, ""), collectionEntry("D-0002", "accepted", "unrelated local constraint")}
	metadata := decisionview.ForkMetadata{Version: decisionview.Version1, LedgerID: adoptionLedger, RepositoryID: adoptionOwner, ParentBindingID: binding.ID, Bindings: []decisionview.Binding{binding}}
	if restored {
		localID := decisionview.QualifiedID("ledger:" + string(adoptionLedger) + ":D-0001")
		metadata.Events = []decisionview.AuthorityEvent{{Version: decisionview.Version1, ID: "restore-operation-0", Kind: decisionview.EventRestore, Date: "2026-09-08", DecidedBy: "user-approved", Target: &localID, OperationID: "restore-operation-0"}}
		metadata.OperationIDs = []string{"restore-operation-0"}
	}
	localBefore := collectionLedger(t, root, overridePath, "active", entries, metadata)
	local, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: overridePath})
	if err != nil {
		t.Fatal(err)
	}
	parentBefore := append([]byte(nil), parent.Files[0].Source...)
	config := []byte(fmt.Sprintf(`{"planningRoot":".plans","repositoryId":%q,"decisionLog":{"version":1,"mode":"fork","path":%q,"ledgerId":%q},"unchanged":{"enabled":true}}`, adoptionOwner, overridePath, adoptionLedger))
	collections := map[decisionview.CollectionID]*decisionview.Collection{adoptionSource: parent, adoptionLedger: local}
	return root, decisionview.ForkRestorationSnapshot{ConfigBefore: config, Local: local, Collections: collections}, parentBefore, append([]byte(nil), localBefore...)
}

func restorationProposal(operation string) json.RawMessage {
	extra := ""
	if operation == "restore" {
		extra = `,"replacement":"ledger:` + string(adoptionLedger) + `:D-0001"`
	}
	return json.RawMessage(fmt.Sprintf(`{"version":1,"operation":%q,"operationId":%q,"date":"2026-09-09","decidedBy":"user-approved","confirmation":%q%s}`, operation, operation+"-operation-1", map[string]string{"restore": "restore this exact accepted override relation", "detach": "detach this exact selected fork and retain local history"}[operation], extra))
}

func assertRestorationPreviewDidNotWrite(t *testing.T, root string, footprintBefore map[string][]byte, parentBefore, localBefore []byte) {
	t.Helper()
	parentAfter, parentErr := os.ReadFile(filepath.Join(root, filepath.FromSlash("Decisions/parent.md")))
	localAfter, localErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(overridePath)))
	if parentErr != nil || localErr != nil || !bytes.Equal(parentAfter, parentBefore) || !bytes.Equal(localAfter, localBefore) {
		t.Errorf("preview wrote filesystem state: parent error=%v local error=%v parent changed=%t local changed=%t", parentErr, localErr, !bytes.Equal(parentAfter, parentBefore), !bytes.Equal(localAfter, localBefore))
	}
	if footprintAfter := restorationTree(t, root); !reflect.DeepEqual(footprintAfter, footprintBefore) {
		t.Errorf("preview changed filesystem footprint:\n before: %v\n after: %v", footprintBefore, footprintAfter)
	}
}

func restorationTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)], err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func restorationChangePaths(p *decisionview.PreviewEnvelope) []string {
	var paths []string
	for _, change := range p.Changes {
		paths = append(paths, string(change.Root)+":"+change.Path)
	}
	return paths
}

func restorationAfter(p *decisionview.PreviewEnvelope, root decisionview.SourceRoot, path string) string {
	for _, change := range p.Changes {
		if change.Root == root && change.Path == path {
			return change.After
		}
	}
	return ""
}
