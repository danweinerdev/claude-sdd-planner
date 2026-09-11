package decisionview

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ForkRestorationSnapshot contains already captured authority and config bytes.
// PreviewForkRestoration must treat the complete snapshot as immutable.
type ForkRestorationSnapshot struct {
	ConfigBefore []byte
	Local        *Collection
	Collections  map[CollectionID]*Collection
}

// ForkRestorationPreview exposes the exact proposal plus independently derived
// restoration/detachment authority context.
type ForkRestorationPreview struct {
	Envelope      *PreviewEnvelope
	Metadata      ForkMetadata
	Config        ForkConfig
	Restored      QualifiedID
	StoredTarget  QualifiedID
	CurrentParent []QualifiedID
	Successors    []QualifiedID
	Removed       []QualifiedID
	Retained      []QualifiedID
}

// PreviewForkRestoration previews either an append-only restore or explicit
// detachment. It changes only prospective bytes; approval and publication are
// separate operations and never inferred from a caller-supplied flag.
func PreviewForkRestoration(snapshot ForkRestorationSnapshot, proposal json.RawMessage) (*ForkRestorationPreview, error) {
	var request struct {
		Version      SchemaVersion `json:"version"`
		Operation    string        `json:"operation"`
		OperationID  string        `json:"operationId"`
		Date         string        `json:"date"`
		DecidedBy    string        `json:"decidedBy"`
		Confirmation string        `json:"confirmation"`
		Replacement  QualifiedID   `json:"replacement"`
	}
	if err := modelStrictJSON(proposal, &request); err != nil {
		return nil, err
	}
	if request.Version != Version1 || (request.Operation != "restore" && request.Operation != "detach") || strings.TrimSpace(request.Confirmation) == "" {
		return nil, fmt.Errorf("decisionview: explicit restore/detach operation and confirmation required")
	}
	local := snapshot.Local
	if local == nil || local.Metadata == nil || len(local.Files) == 0 || local.Locator.Root != SourceRootPlanning || local.Files[0].Path != local.Locator.Path {
		return nil, fmt.Errorf("decisionview: selected local fork snapshot required")
	}
	if err := validateAdoptionConfig(snapshot.ConfigBefore, forkAdoptionRequest{Operation: "rebind", RepositoryID: local.Metadata.RepositoryID, LedgerID: local.ID, Path: local.Locator.Path}); err != nil {
		return nil, err
	}
	if adoptionMode(local) != "fork" {
		return nil, fmt.Errorf("decisionview: authority is already detached")
	}
	if err := local.Metadata.Validate(); err != nil {
		return nil, err
	}
	meta := *local.Metadata
	for _, event := range meta.Events {
		if event.ID == request.OperationID || event.OperationID == request.OperationID {
			return nil, fmt.Errorf("decisionview: operation identity already used")
		}
	}
	for _, id := range meta.OperationIDs {
		if id == request.OperationID {
			return nil, fmt.Errorf("decisionview: operation identity already used")
		}
	}
	event := AuthorityEvent{Version: Version1, ID: request.OperationID, Kind: EventKind(request.Operation), Date: request.Date, DecidedBy: request.DecidedBy, Confirmation: request.Confirmation, OperationID: request.OperationID}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	sources := map[CollectionID]*Collection{}
	for id, c := range snapshot.Collections {
		sources[id] = c
	}
	sources[local.ID] = local
	before, err := Compose(local.ID, "fork", sources)
	if err != nil {
		return nil, err
	}
	index := adoptionParentBindingIndex(&meta)
	if index < 0 {
		return nil, fmt.Errorf("decisionview: missing parent binding")
	}
	binding := meta.Bindings[index]
	parent := sources[binding.CollectionID]
	if parent == nil {
		return nil, fmt.Errorf("decisionview: current parent not captured")
	}
	var parentView *ResolvedView
	if parent.Metadata == nil {
		parentView, err = adoptionSourceView(parent, binding.OwnerID)
	} else {
		parentView, err = Compose(parent.ID, adoptionMode(parent), sources)
	}
	if err != nil {
		return nil, err
	}
	if parentView.Resolution != ResolutionComplete {
		return nil, fmt.Errorf("decisionview: current parent must resolve before restoration/detachment")
	}
	preview := &ForkRestorationPreview{Config: ForkConfig{Version: Version1, Mode: "fork", LedgerID: local.ID, Path: local.Locator.Path}}
	preview.CurrentParent, err = previewBindingIDs(parentView)
	if err != nil {
		return nil, err
	}
	if request.Operation == "restore" {
		collection, id, ok := ParseQualifiedID(string(request.Replacement))
		if !ok || collection != local.ID {
			return nil, fmt.Errorf("decisionview: restore requires a qualified local override")
		}
		entry := local.Entries[id]
		if entry == nil || (entry["status"] != "accepted" && entry["status"] != "superseded") {
			return nil, fmt.Errorf("decisionview: restore target is not retained accepted override history")
		}
		declaration, err := decodeOverride(entry["override"])
		if err != nil {
			return nil, err
		}
		for _, old := range meta.Events {
			if old.Kind == EventRestore && old.Target != nil && *old.Target == request.Replacement {
				return nil, fmt.Errorf("decisionview: override is already restored")
			}
		}
		event.Target = &request.Replacement
		preview.Restored = request.Replacement
		preview.StoredTarget = declaration.Target
		preview.Successors, err = restorationSuccessors(declaration.Target, sources, parentView)
		if err != nil {
			return nil, err
		}
	} else {
		if request.Replacement != "" {
			return nil, fmt.Errorf("decisionview: detach does not implicitly restore an override")
		}
		for _, record := range before.Records {
			if record.CollectionID == local.ID && record.OriginalStatus == "accepted" && record.Override != nil && record.Applicability != "inactive-override" {
				return nil, fmt.Errorf("decisionview: restore active/stale override %s before detachment", record.ID)
			}
		}
		if before.Resolution != ResolutionComplete {
			return nil, fmt.Errorf("decisionview: unresolved authority must be reconciled before detachment")
		}
		preview.Config.Mode = "detached"
	}
	meta.Events = append(append([]AuthorityEvent(nil), meta.Events...), event)
	meta.OperationIDs = append(append([]string(nil), meta.OperationIDs...), request.OperationID)
	next := *local
	next.Metadata = &meta
	raw, err := renderOverrideLedger(local.Files[0].Source, &next, request.Date)
	if err != nil {
		return nil, err
	}
	next.Files = append([]CollectionFile(nil), local.Files...)
	next.Files[0].Source = raw
	sources[local.ID] = &next
	after, err := Compose(local.ID, preview.Config.Mode, sources)
	if err != nil {
		return nil, err
	}
	if after.Resolution == ResolutionInvalid {
		return nil, fmt.Errorf("decisionview: proposed authority is invalid: %v", after.Diagnostics)
	}
	changes := []PreviewFileChange{{Root: SourceRootPlanning, Path: local.Locator.Path, BeforeExists: true, Before: string(local.Files[0].Source), After: string(raw)}}
	if request.Operation == "detach" {
		configAfter, err := detachedConfig(snapshot.ConfigBefore, preview.Config)
		if err != nil {
			return nil, err
		}
		changes = append(changes, PreviewFileChange{Root: SourceRootRepository, Path: "planning-config.json", BeforeExists: true, Before: string(snapshot.ConfigBefore), After: string(configAfter)})
	}
	var dependencies []PreviewSource
	for _, c := range sources {
		if c == nil {
			continue
		}
		for _, file := range c.Files {
			if c.ID == local.ID && file.Path == local.Locator.Path {
				continue
			}
			dependencies = append(dependencies, PreviewSource{Root: c.Locator.Root, Path: file.Path, Digest: collectionDigest(file.Source)})
		}
	}
	if request.Operation == "restore" {
		dependencies = append(dependencies, PreviewSource{Root: SourceRootRepository, Path: "planning-config.json", Digest: collectionDigest(snapshot.ConfigBefore)})
	}
	preview.Envelope, err = NewPreviewEnvelope(request.Operation, request.OperationID, request.Date, proposal, changes, dependencies, before, after)
	if err != nil {
		return nil, err
	}
	preview.Metadata = meta
	preview.Removed = append([]QualifiedID(nil), preview.Envelope.Delta.Removed...)
	preview.Retained = append([]QualifiedID(nil), preview.Envelope.Delta.After...)
	return preview, nil
}

func restorationSuccessors(target QualifiedID, sources map[CollectionID]*Collection, parent *ResolvedView) ([]QualifiedID, error) {
	collection, id, ok := ParseQualifiedID(string(target))
	if !ok || sources[collection] == nil {
		return nil, fmt.Errorf("decisionview: stored target collection is unavailable")
	}
	seen := map[string]bool{}
	var out []QualifiedID
	for {
		if seen[id] {
			return nil, fmt.Errorf("decisionview: cyclic target successor history")
		}
		seen[id] = true
		entry := sources[collection].Entries[id]
		if entry == nil {
			return nil, fmt.Errorf("decisionview: stored target history disappeared")
		}
		next, _ := entry["superseded_by"].(string)
		if next == "" {
			break
		}
		successor := sources[collection].Entries[next]
		if successor == nil || successor["supersedes"] != id {
			return nil, fmt.Errorf("decisionview: inconsistent target successor history")
		}
		qid := QualifiedID("ledger:" + string(collection) + ":" + next)
		for _, record := range parent.Records {
			if record.ID == qid && record.Applicability == "binding" {
				out = append(out, qid)
			}
		}
		id = next
	}
	return out, nil
}

// Replace exactly the selected JSON value, preserving all unrelated config
// tokens and formatting. Input was already checked for duplicate keys.
func detachedConfig(before []byte, config ForkConfig) ([]byte, error) {
	decoder := json.NewDecoder(strings.NewReader(string(before)))
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		start := int(decoder.InputOffset())
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		if key != "decisionLog" {
			continue
		}
		for start < len(before) && (before[start] == ':' || before[start] == ' ' || before[start] == '\t' || before[start] == '\n' || before[start] == '\r') {
			start++
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		out := append([]byte(nil), before[:start]...)
		out = append(out, encoded...)
		out = append(out, before[int(decoder.InputOffset()):]...)
		if !json.Valid(out) {
			return nil, fmt.Errorf("decisionview: invalid detached config projection")
		}
		return out, nil
	}
	return nil, fmt.Errorf("decisionview: missing decisionLog selection")
}
