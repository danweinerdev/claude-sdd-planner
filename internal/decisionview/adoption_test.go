package decisionview_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

const (
	adoptionOwner       = decisionview.OwnerID("11111111-1111-1111-1111-111111111111")
	adoptionSourceOwner = decisionview.OwnerID("22222222-2222-2222-2222-222222222222")
	adoptionLedger      = decisionview.CollectionID("33333333-3333-3333-3333-333333333333")
	adoptionSource      = decisionview.CollectionID("44444444-4444-4444-4444-444444444444")
)

func adoptionSourceCollection() *decisionview.Collection {
	raw := []byte("---\ndecisions:\n  - id: D-0001\n    status: accepted\n    statement: inherited authority\n  - id: D-0002\n    status: rejected\n    statement: rejected alternative\n---\n")
	archive := []byte("---\ndecisions: []\n---\n")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	return &decisionview.Collection{
		ID:      adoptionSource,
		Locator: decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/parent.md", Archives: []string{"Decisions/parent-archive-*.md"}},
		Files:   []decisionview.CollectionFile{{Path: "Decisions/parent.md", Source: raw}, {Path: "Decisions/parent-archive-1.md", Source: archive, Archive: true}},
		Entries: map[string]map[string]any{
			"D-0001": {"id": "D-0001", "status": "accepted", "statement": "inherited authority"},
			"D-0002": {"id": "D-0002", "status": "rejected", "statement": "rejected alternative"},
		},
		Metadata: &decisionview.ForkMetadata{Version: decisionview.Version1, LedgerID: adoptionSource, RepositoryID: adoptionSourceOwner, Events: []decisionview.AuthorityEvent{{Version: decisionview.Version1, ID: "source-detach", Kind: decisionview.EventDetach, Date: "2026-09-01", DecidedBy: "user-approved"}}},
		Digests:  map[string]string{"Decisions/parent.md": digest},
	}
}

func adoptionSelectedConfig() []byte {
	return []byte(`{"planningRoot":".plans","repositoryId":"11111111-1111-1111-1111-111111111111","decisionLog":{"version":1,"mode":"fork","path":"Decisions/fork.md","ledgerId":"33333333-3333-3333-3333-333333333333"},"unchanged":{"enabled":true}}`)
}

func TestForkAdoptionPreviewBytesLoadThroughPublicAPI(t *testing.T) {
	root := t.TempDir()
	preview, err := decisionview.PreviewForkAdoption(decisionview.ForkAdoptionSnapshot{ConfigBefore: []byte(`{"planningRoot":".plans"}`), Source: adoptionSourceCollection()}, adoptionProposal("adopt", "binding-roundtrip", ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range preview.Envelope.Changes {
		if change.Root != decisionview.SourceRootPlanning {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(change.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(change.After), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, adoptionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/fork.md"})
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range loaded.Diagnostics {
		if diagnostic.Severity == "error" {
			t.Errorf("preview bytes fail the real collection validator: %+v", diagnostic)
		}
	}
	if loaded.Metadata == nil || loaded.Metadata.ParentBindingID != "binding-roundtrip" {
		t.Fatal("preview metadata did not round-trip")
	}
}

func adoptionProposal(operation, bindingID, parentBindingID string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"version":1,"operation":%q,"operationId":"operation-7","date":"2026-09-08","repositoryId":%q,"ledgerId":%q,"path":"Decisions/fork.md","bindingId":%q,"parentBindingId":%q,"sourceOwnerId":%q,"source":{"root":"planning","path":"Decisions/parent.md","archives":["Decisions/parent-archive-*.md"]}}`, operation, adoptionOwner, adoptionLedger, bindingID, parentBindingID, adoptionSourceOwner))
}

func TestForkAdoptionIndependentAuthority(t *testing.T) {
	config := []byte("{\n  \"planningRoot\": \".plans\",\n  \"repositoryId\": \"11111111-1111-1111-1111-111111111111\",\n  \"telemetry\": {\"ratio\": 1.25, \"labels\": [\"keep\", \"雪\"]}\n}\n")
	source := adoptionSourceCollection()
	configBefore := append([]byte(nil), config...)
	sourceBefore := append([]byte(nil), source.Files[0].Source...)

	t.Run("adoption binds independently captured source authority", func(t *testing.T) {
		got, err := decisionview.PreviewForkAdoption(decisionview.ForkAdoptionSnapshot{ConfigBefore: config, Source: source}, adoptionProposal("adopt", "binding-initial", ""))
		if err != nil {
			t.Fatalf("preview adoption: %v", err)
		}
		if got.Envelope == nil {
			t.Errorf("adoption preview omitted the exact-byte envelope")
		} else {
			wantAuthority := []decisionview.QualifiedID{"ledger:44444444-4444-4444-4444-444444444444:D-0001"}
			if !reflect.DeepEqual(got.Envelope.Delta.After, wantAuthority) {
				t.Errorf("adoption authority = %v, want independently enumerated accepted source authority %v", got.Envelope.Delta.After, wantAuthority)
			}
			if paths := adoptionChangePaths(got.Envelope); !reflect.DeepEqual(paths, []string{"planning:Decisions/fork.md", "repository:planning-config.json"}) {
				t.Errorf("adoption changed paths = %v, want complete config and local-ledger changes", paths)
			}
			assertAdoptionConfigPreserved(t, got.Envelope)
		}
		if got.Binding.ID != "binding-initial" || got.Binding.OwnerID != adoptionSourceOwner || got.Binding.CollectionID != adoptionSource || got.Binding.CanonicalHash == "" {
			t.Errorf("adoption binding did not explicitly retain source owner, collection, and immutable baseline: %+v", got.Binding)
		}
		if got.Metadata.RepositoryID != adoptionOwner || got.Metadata.LedgerID != adoptionLedger {
			t.Errorf("local authority identity = owner %q ledger %q, want owner %q ledger %q", got.Metadata.RepositoryID, got.Metadata.LedgerID, adoptionOwner, adoptionLedger)
		}
	})

	t.Run("rebinding appends identity and invalidates the old basis without changing selection", func(t *testing.T) {
		oldBinding, err := decisionview.BindCollection("binding-original", adoptionSourceOwner, source)
		if err != nil {
			t.Fatal(err)
		}
		localRaw := []byte("---\nfork:\n  version: 1\n  ledgerId: 33333333-3333-3333-3333-333333333333\n  repositoryId: 11111111-1111-1111-1111-111111111111\ndecisions:\n  - id: D-0900\n    status: accepted\n    statement: unrelated local authority\n---\n")
		localArchive := []byte("---\ndecisions: []\n---\n")
		local := &decisionview.Collection{ID: adoptionLedger, Locator: decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/fork.md", Archives: []string{"Decisions/fork-archive-*.md"}}, Files: []decisionview.CollectionFile{{Path: "Decisions/fork.md", Source: localRaw}, {Path: "Decisions/fork-archive-1.md", Source: localArchive, Archive: true}}, Entries: map[string]map[string]any{"D-0900": {"id": "D-0900", "status": "accepted", "statement": "unrelated local authority"}}, Metadata: &decisionview.ForkMetadata{Version: decisionview.Version1, LedgerID: adoptionLedger, RepositoryID: adoptionOwner, ParentBindingID: "binding-original", Bindings: []decisionview.Binding{oldBinding}}}
		selectedConfig := adoptionSelectedConfig()
		got, err := decisionview.PreviewForkAdoption(decisionview.ForkAdoptionSnapshot{ConfigBefore: selectedConfig, Local: local, Source: source}, adoptionProposal("rebind", "binding-next", "binding-original"))
		if err != nil {
			t.Fatalf("preview rebinding: %v", err)
		}
		if got.Envelope == nil {
			t.Errorf("rebinding preview omitted the exact-byte envelope")
		} else {
			wantAuthority := []decisionview.QualifiedID{"ledger:33333333-3333-3333-3333-333333333333:D-0900", "ledger:44444444-4444-4444-4444-444444444444:D-0001"}
			if !reflect.DeepEqual(got.Envelope.Delta.After, wantAuthority) {
				t.Errorf("rebound authority = %v, want retained local plus accepted source authority %v", got.Envelope.Delta.After, wantAuthority)
			}
			if paths := adoptionChangePaths(got.Envelope); !reflect.DeepEqual(paths, []string{"planning:Decisions/fork.md"}) {
				t.Errorf("rebinding changed paths = %v, want only the selected local ledger", paths)
			}
			wantSources := []string{"planning:Decisions/fork-archive-1.md", "planning:Decisions/parent-archive-1.md", "planning:Decisions/parent.md", "repository:planning-config.json"}
			if sources := adoptionSourcePaths(got.Envelope); !reflect.DeepEqual(sources, wantSources) {
				t.Errorf("rebinding source preconditions = %v, want source/archive, local archive, and unchanged config dependencies %v", sources, wantSources)
			}
			if got.Envelope.Sources[len(got.Envelope.Sources)-1].Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(selectedConfig)) {
				t.Errorf("rebinding did not bind the unchanged planning config bytes")
			}
			if strings.Contains(adoptionAfter(got.Envelope, decisionview.SourceRootPlanning, "Decisions/fork.md"), "binding-original\n") && !strings.Contains(adoptionAfter(got.Envelope, decisionview.SourceRootPlanning, "Decisions/fork.md"), "binding-next") {
				t.Errorf("rebinding did not append the new binding history")
			}
		}
		if got.Binding.ID != "binding-next" || got.Binding.ParentBindingID != "binding-original" {
			t.Errorf("rebound binding = id %q parent %q, want new id binding-next linked to binding-original", got.Binding.ID, got.Binding.ParentBindingID)
		}
		if len(got.Metadata.Bindings) != 2 || got.Metadata.ParentBindingID != "binding-next" {
			t.Errorf("rebinding metadata did not retain old history and select the appended binding: %+v", got.Metadata)
		}
	})

	if !bytes.Equal(config, configBefore) || !bytes.Equal(source.Files[0].Source, sourceBefore) {
		t.Errorf("preview mutated captured config or inherited source bytes")
	}
}

func TestForkAdoptionLegacyOwnerAndRebindSelection(t *testing.T) {
	source := adoptionSourceCollection()
	legacy := []byte("{\n  \"planningRoot\": \".plans\",\n  \"telemetry\": {\"ratio\": 1.25, \"label\": \"keep 雪\"}\n}\n")
	preview, err := decisionview.PreviewForkAdoption(decisionview.ForkAdoptionSnapshot{ConfigBefore: legacy, Source: source}, adoptionProposal("adopt", "binding-initial", ""))
	if err != nil {
		t.Fatalf("legacy adoption: %v", err)
	}
	after := adoptionAfter(preview.Envelope, decisionview.SourceRootRepository, "planning-config.json")
	if !strings.Contains(after, `"repositoryId":"11111111-1111-1111-1111-111111111111"`) || !strings.Contains(after, `"telemetry": {"ratio": 1.25, "label": "keep 雪"}`) {
		t.Fatalf("legacy owner/config bytes were not preserved while adding exact ownership: %s", after)
	}

	wrongOwner := bytes.Replace(legacy, []byte(`"telemetry"`), []byte(`"repositoryId":"99999999-9999-9999-9999-999999999999","telemetry"`), 1)
	if _, err := decisionview.PreviewForkAdoption(decisionview.ForkAdoptionSnapshot{ConfigBefore: wrongOwner, Source: source}, adoptionProposal("adopt", "binding-initial", "")); err == nil {
		t.Fatal("adoption accepted an existing different repository owner")
	}

	oldBinding, err := decisionview.BindCollection("binding-original", adoptionSourceOwner, source)
	if err != nil {
		t.Fatal(err)
	}
	localRaw := []byte("---\nfork:\n  version: 1\n  ledgerId: 33333333-3333-3333-3333-333333333333\n  repositoryId: 11111111-1111-1111-1111-111111111111\ndecisions: []\n---\n")
	local := &decisionview.Collection{ID: adoptionLedger, Locator: decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/fork.md"}, Files: []decisionview.CollectionFile{{Path: "Decisions/fork.md", Source: localRaw}}, Entries: map[string]map[string]any{}, Metadata: &decisionview.ForkMetadata{Version: 1, LedgerID: adoptionLedger, RepositoryID: adoptionOwner, ParentBindingID: oldBinding.ID, Bindings: []decisionview.Binding{oldBinding}}}
	valid := adoptionSelectedConfig()
	cases := [][]byte{
		bytes.Replace(valid, []byte(`"mode":"fork"`), []byte(`"mode":"detached"`), 1),
		bytes.Replace(valid, []byte(`"path":"Decisions/fork.md"`), []byte(`"path":"Decisions/other.md"`), 1),
		bytes.Replace(valid, []byte(`"ledgerId":"33333333-3333-3333-3333-333333333333"`), []byte(`"ledgerId":"66666666-6666-6666-6666-666666666666"`), 1),
		bytes.Replace(valid, []byte(`"planningRoot":`), []byte(`"planningRoot":"other","planningRoot":`), 1),
	}
	for _, bad := range cases {
		if _, err := decisionview.PreviewForkAdoption(decisionview.ForkAdoptionSnapshot{ConfigBefore: bad, Local: local, Source: source}, adoptionProposal("rebind", "binding-next", oldBinding.ID)); err == nil {
			t.Errorf("rebind accepted mismatched or duplicate selected config: %s", bad)
		}
	}
}

func TestForkAdoptionRebindUsesCapturedAuthorityGraphs(t *testing.T) {
	old := adoptionSourceCollection()
	old.ID = "55555555-5555-5555-5555-555555555555"
	old.Metadata.LedgerID = old.ID
	old.Locator = decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/old.md"}
	old.Files = []decisionview.CollectionFile{{Path: old.Locator.Path, Source: old.Files[0].Source}}
	oldBinding, err := decisionview.BindCollection("binding-original", adoptionSourceOwner, old)
	if err != nil {
		t.Fatal(err)
	}
	localRaw := []byte("---\nfork:\n  version: 1\n  ledgerId: 33333333-3333-3333-3333-333333333333\n  repositoryId: 11111111-1111-1111-1111-111111111111\ndecisions: []\n---\n")
	local := &decisionview.Collection{ID: adoptionLedger, Locator: decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/fork.md"}, Files: []decisionview.CollectionFile{{Path: "Decisions/fork.md", Source: localRaw}}, Entries: map[string]map[string]any{}, Metadata: &decisionview.ForkMetadata{Version: 1, LedgerID: adoptionLedger, RepositoryID: adoptionOwner, ParentBindingID: oldBinding.ID, Bindings: []decisionview.Binding{oldBinding}}}
	newSource := adoptionSourceCollection()
	preview, err := decisionview.PreviewForkAdoption(decisionview.ForkAdoptionSnapshot{ConfigBefore: adoptionSelectedConfig(), Local: local, Source: newSource, Collections: map[decisionview.CollectionID]*decisionview.Collection{old.ID: old}}, adoptionProposal("rebind", "binding-next", oldBinding.ID))
	if err != nil {
		t.Fatalf("rebind with separately captured old/new sources: %v", err)
	}
	wantBefore := []decisionview.QualifiedID{decisionview.QualifiedID("ledger:" + string(old.ID) + ":D-0001")}
	if !reflect.DeepEqual(preview.Envelope.Delta.Before, wantBefore) {
		t.Fatalf("BEFORE authority came from prospective source: got %v want current binding %v", preview.Envelope.Delta.Before, wantBefore)
	}

	old.Entries["D-0001"]["statement"] = "changed without reconciliation"
	preview, err = decisionview.PreviewForkAdoption(decisionview.ForkAdoptionSnapshot{ConfigBefore: adoptionSelectedConfig(), Local: local, Source: newSource, Collections: map[decisionview.CollectionID]*decisionview.Collection{old.ID: old}}, adoptionProposal("rebind", "binding-next", oldBinding.ID))
	if err != nil {
		t.Fatalf("rebind with separately captured old/new sources: %v", err)
	}
	if preview.Envelope.BeforeResolution == decisionview.ResolutionComplete || len(preview.Envelope.Delta.Before) != 0 {
		t.Fatalf("changed old source was presented as resolved BEFORE authority: resolution=%s before=%v", preview.Envelope.BeforeResolution, preview.Envelope.Delta.Before)
	}
	if preview.Envelope.AfterResolution != decisionview.ResolutionComplete {
		t.Fatalf("valid prospective source did not resolve independently: %s", preview.Envelope.AfterResolution)
	}

	grand := adoptionSourceCollection()
	grand.ID = "77777777-7777-7777-7777-777777777777"
	grand.Metadata = nil
	grand.Locator = decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/grand.md"}
	grand.Files = []decisionview.CollectionFile{{Path: grand.Locator.Path, Source: grand.Files[0].Source}}
	grandBinding, err := decisionview.BindCollection("grand-binding", adoptionSourceOwner, grand)
	if err != nil {
		t.Fatal(err)
	}
	basis, err := decisionview.CreateBasis(grandBinding.ID, grand, decisionview.QualifiedID("ledger:"+string(grand.ID)+":D-0001"), []string{grandBinding.ID})
	if err != nil {
		t.Fatal(err)
	}
	relationBytes, _ := json.Marshal(decisionview.OverrideDeclaration{Target: basis.TargetID, Basis: basis})
	var relation map[string]any
	if err := json.Unmarshal(relationBytes, &relation); err != nil {
		t.Fatal(err)
	}
	relation["basis"].(map[string]any)["version"] = json.Number("1")
	parent := adoptionSourceCollection()
	parent.Entries = map[string]map[string]any{"D-0100": {"id": "D-0100", "status": "accepted", "statement": "effective inherited override", "rationale": "downstream requirement", "confirmation": "approved", "scope": []any{}, "override": relation}}
	parent.Metadata = &decisionview.ForkMetadata{Version: 1, LedgerID: parent.ID, RepositoryID: adoptionSourceOwner, ParentBindingID: grandBinding.ID, Bindings: []decisionview.Binding{grandBinding}}
	preview, err = decisionview.PreviewForkAdoption(decisionview.ForkAdoptionSnapshot{ConfigBefore: adoptionSelectedConfig(), Local: local, Source: parent, Collections: map[decisionview.CollectionID]*decisionview.Collection{old.ID: old, grand.ID: grand}}, adoptionProposal("rebind", "binding-next-graph", oldBinding.ID))
	if err != nil {
		t.Fatalf("transitive-source rebind: %v", err)
	}
	want := decisionview.QualifiedID("ledger:" + string(parent.ID) + ":D-0100")
	if preview.Envelope.AfterResolution != decisionview.ResolutionComplete || !reflect.DeepEqual(preview.Envelope.Delta.After, []decisionview.QualifiedID{want}) {
		t.Fatalf("canonical Compose did not retain inherited effective override: resolution=%s after=%v", preview.Envelope.AfterResolution, preview.Envelope.Delta.After)
	}
}

func TestForkAdoptionHostileEnvelope(t *testing.T) {
	source := adoptionSourceCollection()
	snapshot := decisionview.ForkAdoptionSnapshot{ConfigBefore: []byte(`{"planningRoot":".plans","repositoryId":"11111111-1111-1111-1111-111111111111"}`), Source: source}
	valid := string(adoptionProposal("adopt", "binding-initial", ""))
	cases := map[string]string{
		"unknown instruction":    strings.TrimSuffix(valid, "}") + `,"writeInheritedSource":true}`,
		"path traversal":         strings.Replace(valid, `"path":"Decisions/fork.md"`, `"path":"../decisions.md"`, 1),
		"source alias mismatch":  strings.Replace(valid, `"path":"Decisions/parent.md"`, `"path":"Decisions/other.md"`, 1),
		"source owner mismatch":  strings.Replace(valid, string(adoptionSourceOwner), "55555555-5555-5555-5555-555555555555", 1),
		"duplicate identity key": strings.Replace(valid, `"ledgerId":`, `"ledgerId":"66666666-6666-6666-6666-666666666666","ledgerId":`, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decisionview.PreviewForkAdoption(snapshot, json.RawMessage(raw)); err == nil {
				t.Errorf("accepted hostile external adoption proposal: %s", name)
			}
		})
	}
}

func adoptionChangePaths(p *decisionview.PreviewEnvelope) []string {
	var paths []string
	for _, change := range p.Changes {
		paths = append(paths, string(change.Root)+":"+change.Path)
	}
	return paths
}

func adoptionAfter(p *decisionview.PreviewEnvelope, root decisionview.SourceRoot, path string) string {
	for _, change := range p.Changes {
		if change.Root == root && change.Path == path {
			return change.After
		}
	}
	return ""
}

func adoptionSourcePaths(p *decisionview.PreviewEnvelope) []string {
	paths := make([]string, 0, len(p.Sources))
	for _, source := range p.Sources {
		paths = append(paths, string(source.Root)+":"+source.Path)
	}
	return paths
}

func assertAdoptionConfigPreserved(t *testing.T, p *decisionview.PreviewEnvelope) {
	t.Helper()
	raw := adoptionAfter(p, decisionview.SourceRootRepository, "planning-config.json")
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Errorf("adoption config is not JSON: %v", err)
		return
	}
	want := map[string]any{"ratio": 1.25, "labels": []any{"keep", "雪"}}
	if !reflect.DeepEqual(config["telemetry"], want) {
		t.Errorf("unrelated config value telemetry = %#v, want %#v", config["telemetry"], want)
	}
}
