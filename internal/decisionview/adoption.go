package decisionview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// ForkAdoptionSnapshot contains the already captured bytes and parsed
// collections from which an adoption or rebinding proposal is derived.
// PreviewForkAdoption must treat every field as read-only.
type ForkAdoptionSnapshot struct {
	ConfigBefore []byte
	Local        *Collection
	Source       *Collection
	Collections  map[CollectionID]*Collection
}

// ForkAdoptionPreview exposes both the generic exact-byte envelope and the
// authority records derived for the prospective local collection.
type ForkAdoptionPreview struct {
	Envelope *PreviewEnvelope
	Binding  Binding
	Metadata ForkMetadata
}

type forkAdoptionRequest struct {
	Version         SchemaVersion   `json:"version"`
	Operation       string          `json:"operation"`
	OperationID     string          `json:"operationId"`
	Date            string          `json:"date"`
	RepositoryID    OwnerID         `json:"repositoryId"`
	LedgerID        CollectionID    `json:"ledgerId"`
	Path            string          `json:"path"`
	BindingID       string          `json:"bindingId"`
	ParentBindingID string          `json:"parentBindingId"`
	SourceOwnerID   OwnerID         `json:"sourceOwnerId"`
	Source          SourceLocator   `json:"source"`
	SourceLedgerID  CollectionID    `json:"sourceLedgerId,omitempty"`
	LegacyContexts  []LegacyContext `json:"legacyContexts,omitempty"`
}

// PreviewForkAdoption derives all prospective bytes from an immutable snapshot.
// It does not open files, acquire writer locks, or mutate any captured value.
func PreviewForkAdoption(snapshot ForkAdoptionSnapshot, proposal json.RawMessage) (*ForkAdoptionPreview, error) {
	var request forkAdoptionRequest
	if err := modelStrictJSON(proposal, &request); err != nil {
		return nil, fmt.Errorf("decisionview: adoption proposal: %w", err)
	}
	if err := validateForkAdoptionRequest(request, snapshot); err != nil {
		return nil, err
	}

	binding, err := BindCollection(request.BindingID, request.SourceOwnerID, snapshot.Source)
	if err != nil {
		return nil, err
	}
	if request.Operation == "rebind" {
		binding.ParentBindingID = request.ParentBindingID
		binding.CanonicalHash, err = bindingDigest(binding)
		if err != nil {
			return nil, err
		}
	}

	metadata, before, localBefore, err := adoptionMetadataAndBefore(request, snapshot, binding)
	if err != nil {
		return nil, err
	}
	localAfterBytes, err := renderAdoptionLedger(localBefore, metadata)
	if err != nil {
		return nil, err
	}
	localAfter := adoptionCollection(snapshot.Local, request, metadata, localAfterBytes)
	after, err := adoptionCompose(snapshot, localAfter, true)
	if err != nil {
		return nil, err
	}

	changes := []PreviewFileChange{{
		Root:         SourceRootPlanning,
		Path:         request.Path,
		BeforeExists: snapshot.Local != nil,
		Before:       string(localBefore),
		After:        string(localAfterBytes),
	}}
	if request.Operation == "adopt" {
		configAfter, err := adoptionConfig(snapshot.ConfigBefore, request)
		if err != nil {
			return nil, err
		}
		changes = append(changes, PreviewFileChange{Root: SourceRootRepository, Path: "planning-config.json", BeforeExists: true, Before: string(snapshot.ConfigBefore), After: string(configAfter)})
	}

	sources, err := adoptionSources(snapshot, request)
	if err != nil {
		return nil, err
	}
	envelope, err := NewPreviewEnvelope(request.Operation, request.OperationID, request.Date, proposal, changes, sources, before, after)
	if err != nil {
		return nil, err
	}
	return &ForkAdoptionPreview{Envelope: envelope, Binding: binding, Metadata: metadata}, nil
}

func validateForkAdoptionRequest(request forkAdoptionRequest, snapshot ForkAdoptionSnapshot) error {
	if request.Version != Version1 || (request.Operation != "adopt" && request.Operation != "rebind") {
		return fmt.Errorf("decisionview: unsupported adoption operation")
	}
	if strings.TrimSpace(request.OperationID) == "" || strings.ContainsAny(request.OperationID, "\x00\r\n") {
		return fmt.Errorf("decisionview: adoption operation identity required")
	}
	if strings.TrimSpace(request.BindingID) == "" {
		return fmt.Errorf("decisionview: binding identity required")
	}
	if err := request.RepositoryID.Validate(); err != nil {
		return err
	}
	if err := request.LedgerID.Validate(); err != nil {
		return err
	}
	if err := request.SourceOwnerID.Validate(); err != nil {
		return err
	}
	if err := validateRelativeLocator(request.Path); err != nil {
		return err
	}
	if err := request.Source.Validate(); err != nil {
		return err
	}
	if snapshot.Source == nil {
		return fmt.Errorf("%w: no captured source collection", ErrInvalidCollection)
	}
	if request.SourceLedgerID != "" && request.SourceLedgerID != snapshot.Source.ID {
		return fmt.Errorf("%w: proposed sourceLedgerId differs from captured source", ErrInvalidCollection)
	}
	if request.Operation == "rebind" && len(request.LegacyContexts) != 0 {
		return fmt.Errorf("decisionview: rebinding must preserve the original legacy citation contexts")
	}
	contexts := map[string]bool{}
	for _, legacy := range request.LegacyContexts {
		key := string(legacy.Root) + "\x00" + legacy.Path
		if contexts[key] {
			return fmt.Errorf("decisionview: duplicate legacy citation context for %s", legacy.Path)
		}
		contexts[key] = true
		collection := adoptionCollectionSet(snapshot, nil, true)[legacy.Namespace]
		if collection == nil {
			return fmt.Errorf("decisionview: legacy citation namespace is not a captured source")
		}
		for _, id := range legacy.LocalIDs {
			if _, ok := collection.Entries[id]; !ok {
				return fmt.Errorf("decisionview: legacy citation %s is absent from its source namespace", id)
			}
		}
	}
	if snapshot.Source.ID == request.LedgerID {
		return fmt.Errorf("%w: local and source collection identities must differ", ErrInvalidCollection)
	}
	if !reflect.DeepEqual(snapshot.Source.Locator, request.Source) {
		return fmt.Errorf("%w: proposal source locator differs from captured source", ErrInvalidCollection)
	}
	if snapshot.Source.Metadata != nil && snapshot.Source.Metadata.RepositoryID != request.SourceOwnerID {
		return fmt.Errorf("%w: proposal source owner differs from captured source", ErrInvalidCollection)
	}
	if err := validateAdoptionConfig(snapshot.ConfigBefore, request); err != nil {
		return err
	}
	if request.Operation == "adopt" {
		if snapshot.Local != nil || request.ParentBindingID != "" {
			return fmt.Errorf("decisionview: initial adoption cannot replace existing local authority")
		}
	} else {
		if snapshot.Local == nil || snapshot.Local.Metadata == nil {
			return fmt.Errorf("decisionview: rebinding requires captured local fork authority")
		}
		if len(snapshot.Local.Files) == 0 || snapshot.Local.Files[0].Path != request.Path {
			return fmt.Errorf("%w: rebinding requires captured canonical local bytes", ErrInvalidCollection)
		}
		if snapshot.Local.ID != request.LedgerID || snapshot.Local.Locator.Root != SourceRootPlanning || snapshot.Local.Locator.Path != request.Path {
			return fmt.Errorf("%w: rebinding target differs from selected local collection", ErrInvalidCollection)
		}
		if snapshot.Local.Metadata.RepositoryID != request.RepositoryID || snapshot.Local.Metadata.LedgerID != request.LedgerID {
			return fmt.Errorf("%w: rebinding owner or collection identity differs", ErrInvalidCollection)
		}
		if request.ParentBindingID == "" || snapshot.Local.Metadata.ParentBindingID != request.ParentBindingID {
			return fmt.Errorf("decisionview: rebinding parent does not match current binding")
		}
		parentIndex := adoptionParentBindingIndex(snapshot.Local.Metadata)
		if parentIndex < 0 {
			return fmt.Errorf("%w: rebinding snapshot lacks the selected parent binding", ErrInvalidCollection)
		}
		if _, ok := adoptionCollectionSet(snapshot, snapshot.Local, false)[snapshot.Local.Metadata.Bindings[parentIndex].CollectionID]; !ok {
			return fmt.Errorf("%w: rebinding snapshot lacks the current source collection", ErrInvalidCollection)
		}
		for _, existing := range snapshot.Local.Metadata.Bindings {
			if existing.ID == request.BindingID {
				return fmt.Errorf("decisionview: binding identity already exists")
			}
		}
		for _, existing := range snapshot.Local.Metadata.Events {
			if existing.ID == request.OperationID || existing.OperationID == request.OperationID {
				return fmt.Errorf("decisionview: operation identity already exists")
			}
		}
		for _, existing := range snapshot.Local.Metadata.OperationIDs {
			if existing == request.OperationID {
				return fmt.Errorf("decisionview: operation identity already exists")
			}
		}
	}
	return nil
}

func adoptionMetadataAndBefore(request forkAdoptionRequest, snapshot ForkAdoptionSnapshot, binding Binding) (ForkMetadata, *ResolvedView, []byte, error) {
	event := AuthorityEvent{Version: Version1, ID: request.OperationID, Kind: EventKind(request.Operation), Date: request.Date, DecidedBy: "user-approved", OperationID: request.OperationID}
	if err := event.Validate(); err != nil {
		return ForkMetadata{}, nil, nil, err
	}
	if request.Operation == "adopt" {
		metadata := ForkMetadata{Version: Version1, LedgerID: request.LedgerID, RepositoryID: request.RepositoryID, ParentBindingID: binding.ID, Bindings: []Binding{binding}, Events: []AuthorityEvent{event}, OperationIDs: []string{request.OperationID}, LegacyContexts: request.LegacyContexts}
		before, err := adoptionCurrentSourceView(snapshot, request.SourceOwnerID)
		return metadata, before, nil, err
	}

	metadata := *snapshot.Local.Metadata
	metadata.Archives = append([]string(nil), metadata.Archives...)
	metadata.Bindings = append(append([]Binding(nil), metadata.Bindings...), binding)
	metadata.Events = append(append([]AuthorityEvent(nil), metadata.Events...), event)
	metadata.LegacyContexts = append([]LegacyContext(nil), metadata.LegacyContexts...)
	metadata.OperationIDs = append(append([]string(nil), metadata.OperationIDs...), request.OperationID)
	metadata.ParentBindingID = binding.ID
	before, err := adoptionCompose(snapshot, snapshot.Local, false)
	if err != nil {
		return ForkMetadata{}, nil, nil, err
	}
	return metadata, before, append([]byte(nil), snapshot.Local.Files[0].Source...), nil
}

func adoptionSourceView(source *Collection, owner OwnerID) (*ResolvedView, error) {
	if err := continuityInput(source); err != nil {
		return nil, err
	}
	v := &ResolvedView{Version: 1, OwnerID: owner, LocalID: source.ID, Resolution: ResolutionComplete, Records: []ResolvedDecision{}, Diagnostics: []Diagnostic{}}
	for _, id := range continuityIDs(source.Entries) {
		entry, err := copyEntry(source.Entries[id])
		if err != nil {
			return nil, err
		}
		status, _ := entry["status"].(string)
		applicability := "historical"
		if status == "accepted" {
			applicability = "binding"
		}
		v.Records = append(v.Records, ResolvedDecision{ID: QualifiedID("ledger:" + string(source.ID) + ":" + id), CollectionID: source.ID, Original: entry, OriginalStatus: status, Applicability: applicability, Source: source.Locator})
	}
	return v, nil
}

func adoptionCurrentSourceView(snapshot ForkAdoptionSnapshot, owner OwnerID) (*ResolvedView, error) {
	if snapshot.Source.Metadata == nil {
		return adoptionSourceView(snapshot.Source, owner)
	}
	return Compose(snapshot.Source.ID, adoptionMode(snapshot.Source), adoptionCollectionSet(snapshot, snapshot.Source, true))
}

func adoptionCompose(snapshot ForkAdoptionSnapshot, local *Collection, prospective bool) (*ResolvedView, error) {
	return Compose(local.ID, adoptionMode(local), adoptionCollectionSet(snapshot, local, prospective))
}

func adoptionMode(c *Collection) string {
	if c != nil && c.Metadata != nil && lastModeEvent(c.Metadata.Events) == EventDetach {
		return "detached"
	}
	return "fork"
}

func adoptionCollectionSet(snapshot ForkAdoptionSnapshot, local *Collection, prospective bool) map[CollectionID]*Collection {
	sources := make(map[CollectionID]*Collection, len(snapshot.Collections)+2)
	for id, collection := range snapshot.Collections {
		sources[id] = collection
	}
	if snapshot.Source != nil {
		if _, exists := sources[snapshot.Source.ID]; prospective || !exists {
			sources[snapshot.Source.ID] = snapshot.Source
		}
	}
	if local != nil {
		sources[local.ID] = local
	}
	return sources
}

func adoptionParentBindingIndex(metadata *ForkMetadata) int {
	if metadata == nil {
		return -1
	}
	for i := range metadata.Bindings {
		if metadata.Bindings[i].ID == metadata.ParentBindingID {
			return i
		}
	}
	return -1
}

func adoptionSources(snapshot ForkAdoptionSnapshot, request forkAdoptionRequest) ([]PreviewSource, error) {
	dependencies := map[string]PreviewSource{}
	add := func(root SourceRoot, path string, raw []byte) error {
		key := string(root) + "\x00" + path
		next := PreviewSource{Root: root, Path: path, Digest: collectionDigest(raw)}
		if previous, exists := dependencies[key]; exists && previous.Digest != next.Digest {
			return fmt.Errorf("%w: captured dependency bytes disagree for %s", ErrInvalidCollection, path)
		}
		dependencies[key] = next
		return nil
	}
	collections := adoptionCollectionSet(snapshot, snapshot.Local, true)
	for _, collection := range collections {
		if collection == nil {
			continue
		}
		for _, file := range collection.Files {
			if collection.ID == request.LedgerID && collection.Locator.Root == SourceRootPlanning && file.Path == request.Path && !file.Archive {
				continue
			}
			if err := add(collection.Locator.Root, file.Path, file.Source); err != nil {
				return nil, err
			}
		}
	}
	if request.Operation == "rebind" {
		if err := add(SourceRootRepository, "planning-config.json", snapshot.ConfigBefore); err != nil {
			return nil, err
		}
	}
	out := make([]PreviewSource, 0, len(dependencies))
	for _, dependency := range dependencies {
		out = append(out, dependency)
	}
	return out, nil
}

func adoptionCollection(old *Collection, request forkAdoptionRequest, metadata ForkMetadata, raw []byte) *Collection {
	if old == nil {
		return &Collection{ID: request.LedgerID, Locator: SourceLocator{Root: SourceRootPlanning, Path: request.Path}, Files: []CollectionFile{{Path: request.Path, Source: append([]byte(nil), raw...)}}, Entries: map[string]map[string]any{}, EntryPaths: map[string]string{}, Metadata: &metadata, Digests: map[string]string{request.Path: collectionDigest(raw)}}
	}
	next := *old
	next.Files = append([]CollectionFile(nil), old.Files...)
	next.Files[0].Source = append([]byte(nil), raw...)
	next.Metadata = &metadata
	next.Digests = make(map[string]string, len(old.Digests))
	for path, digest := range old.Digests {
		next.Digests[path] = digest
	}
	next.Digests[request.Path] = collectionDigest(raw)
	return &next
}

func renderAdoptionLedger(before []byte, metadata ForkMetadata) ([]byte, error) {
	if err := metadata.Validate(); err != nil {
		return nil, err
	}
	block, err := yaml.Marshal(struct {
		Fork ForkMetadata `yaml:"fork"`
	}{Fork: metadata})
	if err != nil {
		return nil, err
	}
	blockLines := strings.Split(strings.TrimSuffix(string(block), "\n"), "\n")
	if before == nil {
		if len(metadata.Events) == 0 {
			return nil, fmt.Errorf("decisionview: new ledger requires a dated adoption event")
		}
		date := metadata.Events[0].Date
		lines := append([]string{"---", "title: Fork decision ledger", "type: decision-log", "status: active", "created: " + date, "updated: " + date, "tags: []", "related: []"}, blockLines...)
		lines = append(lines, "decisions: []", "---", "")
		return []byte(strings.Join(lines, "\n")), nil
	}
	if !utf8.Valid(before) {
		return nil, fmt.Errorf("decisionview: local ledger must be UTF-8")
	}
	lines := strings.Split(string(before), "\n")
	end := 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "---" {
		end++
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" || end == len(lines) {
		return nil, fmt.Errorf("%w: malformed local frontmatter", ErrInvalidCollection)
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: local frontmatter must be a mapping", ErrInvalidCollection)
	}
	mapping := document.Content[0]
	// Line-splicing a flow-style top-level mapping would replace unrelated
	// keys sharing the same line. Refuse instead of corrupting approved bytes.
	if mapping.Style&yaml.FlowStyle != 0 {
		return nil, fmt.Errorf("%w: rebinding requires block-style ledger frontmatter", ErrInvalidCollection)
	}
	start, stop := -1, end
	for i := 0; i < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		if key.Value == "fork" {
			start = key.Line
			if i+2 < len(mapping.Content) {
				stop = mapping.Content[i+2].Line
			}
			break
		}
	}
	if start < 1 {
		return nil, fmt.Errorf("%w: local ledger lacks fork metadata", ErrInvalidCollection)
	}
	result := append([]string(nil), lines[:start]...)
	result = append(result, blockLines...)
	result = append(result, lines[stop:]...)
	return []byte(strings.Join(result, "\n")), nil
}

func validateAdoptionConfig(raw []byte, request forkAdoptionRequest) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("decisionview: planning config must be UTF-8")
	}
	if err := selectionUniqueRootKeys(raw); err != nil {
		return err
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil || config == nil {
		return fmt.Errorf("decisionview: invalid planning config")
	}
	if rawOwner, exists := config["repositoryId"]; exists {
		var found OwnerID
		if err := json.Unmarshal(rawOwner, &found); err != nil || found.Validate() != nil {
			return fmt.Errorf("decisionview: invalid repositoryId in planning config")
		}
		if found != request.RepositoryID {
			return fmt.Errorf("decisionview: proposal owner differs from planning config")
		}
	} else if request.Operation == "rebind" {
		return fmt.Errorf("decisionview: rebinding requires the selected repository owner")
	}
	rawSelection, selected := config["decisionLog"]
	if request.Operation == "adopt" {
		if selected {
			return fmt.Errorf("decisionview: adoption cannot replace an existing decisionLog selection")
		}
		return nil
	}
	if !selected {
		return fmt.Errorf("decisionview: rebinding requires an existing decisionLog selection")
	}
	selection, err := DecodeForkConfig(rawSelection)
	if err != nil {
		return fmt.Errorf("decisionview: decisionLog: %w", err)
	}
	if selection.Mode != "fork" || selection.Path != request.Path || selection.LedgerID != request.LedgerID || selection.Transaction != nil {
		return fmt.Errorf("decisionview: rebinding target differs from the exact selected fork")
	}
	return nil
}

func adoptionConfig(before []byte, request forkAdoptionRequest) ([]byte, error) {
	var config map[string]json.RawMessage
	if err := json.Unmarshal(before, &config); err != nil || config == nil {
		return nil, fmt.Errorf("decisionview: invalid planning config")
	}
	selection, err := json.Marshal(ForkConfig{Version: Version1, Mode: "fork", Path: request.Path, LedgerID: request.LedgerID})
	if err != nil {
		return nil, err
	}
	fields := make([][]byte, 0, 2)
	if _, exists := config["repositoryId"]; !exists {
		owner, err := json.Marshal(request.RepositoryID)
		if err != nil {
			return nil, err
		}
		fields = append(fields, append([]byte(`"repositoryId":`), owner...))
	}
	fields = append(fields, append([]byte(`"decisionLog":`), selection...))
	close := len(before) - 1
	for close >= 0 && (before[close] == ' ' || before[close] == '\t' || before[close] == '\r' || before[close] == '\n') {
		close--
	}
	if close < 0 || before[close] != '}' {
		return nil, fmt.Errorf("decisionview: planning config must be an object")
	}
	last := close - 1
	for last >= 0 && (before[last] == ' ' || before[last] == '\t' || before[last] == '\r' || before[last] == '\n') {
		last--
	}
	if last < 0 {
		return nil, fmt.Errorf("decisionview: planning config must be an object")
	}
	multiline := bytes.Contains(before[:close], []byte("\n"))
	out := append([]byte(nil), before[:last+1]...)
	if before[last] != '{' {
		out = append(out, ',')
	}
	trailing := before[last+1 : close]
	if multiline {
		if bytes.Contains(trailing, []byte("\n")) {
			out = append(out, trailing...)
			out = append(out, []byte("  ")...)
		} else {
			out = append(out, []byte("\n  ")...)
		}
	}
	for i, field := range fields {
		if i > 0 && !multiline {
			out = append(out, ',')
		}
		out = append(out, field...)
		if multiline && i+1 < len(fields) {
			out = append(out, []byte(",\n  ")...)
		}
	}
	if multiline {
		out = append(out, '\n')
	}
	out = append(out, before[close:]...)
	if !json.Valid(out) {
		return nil, fmt.Errorf("decisionview: could not preserve planning config while adding selection")
	}
	return out, nil
}
