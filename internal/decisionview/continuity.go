package decisionview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type BasisComparison struct {
	Current        bool
	ChangedFields  []string
	Target         map[string]any
	Successors     []QualifiedID
	BindingChanged bool
}

func BindCollection(id string, owner OwnerID, c *Collection) (Binding, error) {
	if strings.TrimSpace(id) == "" {
		return Binding{}, fmt.Errorf("%w: binding ID required", ErrInvalidCollection)
	}
	if err := owner.Validate(); err != nil {
		return Binding{}, err
	}
	if err := continuityInput(c); err != nil {
		return Binding{}, err
	}
	if c.Metadata != nil && c.Metadata.RepositoryID != owner {
		return Binding{}, fmt.Errorf("%w: source owner does not match binding", ErrInvalidCollection)
	}
	b := Binding{Version: Version1, ID: id, OwnerID: owner, CollectionID: c.ID, Source: c.Locator, CanonicalContent: make([]map[string]any, 0, len(c.Entries))}
	b.Source.Archives = append([]string(nil), c.Locator.Archives...)
	if c.Metadata != nil {
		b.ForkSource = true
		b.ParentBindingID = c.Metadata.ParentBindingID
		b.ForkBindings = map[string]string{}
		b.ForkEvents = map[string]string{}
		for _, source := range c.Metadata.Bindings {
			if source.ID == "" || b.ForkBindings[source.ID] != "" {
				return Binding{}, fmt.Errorf("%w: duplicate or missing source binding ID", ErrInvalidCollection)
			}
			digest, err := forkMetadataDigest(source)
			if err != nil {
				return Binding{}, err
			}
			b.ForkBindings[source.ID] = digest
		}
		for _, event := range c.Metadata.Events {
			if b.ForkEvents[event.ID] != "" {
				return Binding{}, fmt.Errorf("%w: duplicate source event ID", ErrInvalidCollection)
			}
			digest, err := forkMetadataDigest(event)
			if err != nil {
				return Binding{}, err
			}
			b.ForkEvents[event.ID] = digest
			b.ForkEventOrder = append(b.ForkEventOrder, event.ID)
		}
	}
	for _, key := range continuityIDs(c.Entries) {
		copy, err := copyEntry(c.Entries[key])
		if err != nil {
			return Binding{}, err
		}
		b.CanonicalContent = append(b.CanonicalContent, copy)
	}
	digest, err := bindingDigest(b)
	if err != nil {
		return Binding{}, err
	}
	b.CanonicalHash = digest
	return b, nil
}
func CheckContinuity(b Binding, c *Collection) ([]Diagnostic, error) {
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCollection, err)
	}
	if err := continuityInput(c); err != nil {
		return nil, err
	}
	want, err := bindingDigest(b)
	if err != nil {
		return nil, err
	}
	if b.ID == "" || b.CanonicalHash != want {
		return nil, fmt.Errorf("%w: binding baseline digest mismatch", ErrInvalidCollection)
	}
	var out []Diagnostic
	issue := func(message string) {
		out = append(out, Diagnostic{Code: "FDL010", Severity: Error, Path: c.Locator.Path, Message: message, Correction: "Inspect the retained binding baseline and explicitly reconcile source rebinding or restore valid source history."})
	}
	if c.ID != b.CollectionID || !reflect.DeepEqual(c.Locator, b.Source) {
		issue("Source identity or declared locator no longer matches binding " + b.ID)
		return out, nil
	}
	if c.Metadata != nil && c.Metadata.RepositoryID != b.OwnerID {
		issue("Source owner no longer matches binding " + b.ID)
	}
	if b.ForkSource && c.Metadata == nil {
		issue("Source fork declaration was removed from " + b.ID)
		return out, nil
	}
	if b.ForkSource {
		bindings := map[string]string{}
		events := map[string]string{}
		for _, record := range c.Metadata.Bindings {
			digest, err := forkMetadataDigest(record)
			if err != nil {
				return nil, err
			}
			bindings[record.ID] = digest
		}
		for _, event := range c.Metadata.Events {
			digest, err := forkMetadataDigest(event)
			if err != nil {
				return nil, err
			}
			events[event.ID] = digest
		}
		keys := make([]string, 0, len(b.ForkBindings))
		for id := range b.ForkBindings {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys {
			if bindings[id] != b.ForkBindings[id] {
				issue("Retained source binding " + id + " was changed or removed")
			}
		}
		for i, id := range b.ForkEventOrder {
			if i >= len(c.Metadata.Events) || c.Metadata.Events[i].ID != id || events[id] != b.ForkEvents[id] {
				issue("Retained source authority event " + id + " was changed, reordered or removed")
			}
		}
	}
	seen := map[string]bool{}
	for _, old := range b.CanonicalContent {
		id, _ := old["id"].(string)
		if id == "" || seen[id] {
			return nil, fmt.Errorf("%w: invalid baseline identity", ErrInvalidCollection)
		}
		seen[id] = true
		now, ok := c.Entries[id]
		if !ok {
			issue("Retained baseline decision " + id + " is missing")
			continue
		}
		status, _ := old["status"].(string)
		switch status {
		case "accepted":
			if now["status"] != "accepted" && now["status"] != "superseded" {
				issue("Accepted decision " + id + " has an invalid lifecycle transition")
			}
			fields, err := changedEntryFields(old, now, map[string]bool{"status": true, "superseded_by": true})
			if err != nil {
				return nil, err
			}
			for _, field := range fields {
				issue("Accepted decision " + id + " changed immutable field " + field)
			}
		case "rejected", "superseded":
			fields, err := changedEntryFields(old, now, nil)
			if err != nil {
				return nil, err
			}
			if len(fields) > 0 {
				issue("Historical decision " + id + " changed after reaching " + status)
			}
		case "proposed": // Retain identity, but proposals may evolve before acceptance.
		default:
			return nil, fmt.Errorf("%w: unsupported baseline status", ErrInvalidCollection)
		}
	}
	return out, nil
}
func CreateBasis(bindingID string, c *Collection, target QualifiedID, lineage []string) (OverrideBasis, error) {
	if err := continuityInput(c); err != nil {
		return OverrideBasis{}, err
	}
	collection, id, ok := ParseQualifiedID(string(target))
	if !ok || collection != c.ID {
		return OverrideBasis{}, fmt.Errorf("%w: target is not in this source collection", ErrInvalidCollection)
	}
	entry, ok := c.Entries[id]
	if !ok || entry["status"] != "accepted" {
		return OverrideBasis{}, fmt.Errorf("%w: basis target must be accepted", ErrInvalidCollection)
	}
	if bindingID == "" {
		return OverrideBasis{}, fmt.Errorf("%w: binding ID required", ErrInvalidCollection)
	}
	copy, err := copyEntry(entry)
	if err != nil {
		return OverrideBasis{}, err
	}
	raw, err := json.Marshal(copy)
	if err != nil {
		return OverrideBasis{}, err
	}
	digest, err := DigestEntry(CanonicalVersion, copy)
	if err != nil {
		return OverrideBasis{}, err
	}
	return OverrideBasis{Version: Version1, TargetID: target, BindingID: bindingID, Canonicalization: CanonicalVersion, CanonicalHash: digest, CanonicalContent: raw, Lineage: append([]string(nil), lineage...)}, nil
}
func CompareBasis(b OverrideBasis, c *Collection, bindingID string, lineage []string) (BasisComparison, error) {
	var result BasisComparison
	if err := b.Validate(); err != nil {
		return result, fmt.Errorf("%w: %v", ErrInvalidCollection, err)
	}
	if b.Canonicalization != CanonicalVersion {
		return result, fmt.Errorf("%w: unsupported basis canonicalization", ErrInvalidCollection)
	}
	if err := continuityInput(c); err != nil {
		return result, err
	}
	var old map[string]any
	if err := modelStrictJSON(b.CanonicalContent, &old); err != nil {
		return result, fmt.Errorf("%w: %v", ErrInvalidCollection, err)
	}
	digest, err := DigestEntry(CanonicalVersion, old)
	if err != nil {
		return result, err
	}
	collection, id, ok := ParseQualifiedID(string(b.TargetID))
	if !ok || old["id"] != id || digest != b.CanonicalHash {
		return result, fmt.Errorf("%w: target basis content/digest mismatch", ErrInvalidCollection)
	}
	result.BindingChanged = collection != c.ID || b.BindingID != bindingID || !equalLineage(b.Lineage, lineage)
	now, exists := c.Entries[id]
	if exists {
		result.Target, err = copyEntry(now)
		if err != nil {
			return result, err
		}
		result.ChangedFields, err = changedEntryFields(old, now, nil)
		if err != nil {
			return result, err
		}
	}
	for _, nextID := range continuityIDs(c.Entries) {
		if c.Entries[nextID]["supersedes"] == id {
			result.Successors = append(result.Successors, QualifiedID("ledger:"+string(c.ID)+":"+nextID))
		}
	}
	result.Current = exists && now["status"] == "accepted" && !result.BindingChanged && len(result.ChangedFields) == 0
	return result, nil
}

func continuityInput(c *Collection) error {
	if c == nil || c.Entries == nil {
		return fmt.Errorf("%w: no captured collection", ErrInvalidCollection)
	}
	if err := c.ID.Validate(); err != nil {
		return err
	}
	if err := c.Locator.Validate(); err != nil {
		return err
	}
	for _, d := range c.Diagnostics {
		if d.Severity.Invalidating() {
			return fmt.Errorf("%w: source diagnostics invalidate the collection", ErrInvalidCollection)
		}
	}
	return nil
}
func continuityIDs(entries map[string]map[string]any) []string {
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func copyEntry(entry map[string]any) (map[string]any, error) {
	encoded, err := EncodeEntry(CanonicalVersion, entry)
	if err != nil {
		return nil, err
	}
	return DecodeEntry(CanonicalVersion, encoded)
}
func bindingDigest(b Binding) (string, error) {
	records := b.CanonicalContent
	if records == nil {
		records = []map[string]any{}
	}
	archives := b.Source.Archives
	if archives == nil {
		archives = []string{}
	}
	bindings := b.ForkBindings
	if bindings == nil {
		bindings = map[string]string{}
	}
	events := b.ForkEvents
	if events == nil {
		events = map[string]string{}
	}
	order := b.ForkEventOrder
	if order == nil {
		order = []string{}
	}
	return DigestEntry(CanonicalVersion, map[string]any{"bindingId": b.ID, "ownerId": string(b.OwnerID), "collectionId": string(b.CollectionID), "root": string(b.Source.Root), "path": b.Source.Path, "archives": archives, "entries": records, "forkSource": b.ForkSource, "parentBindingId": b.ParentBindingID, "forkBindings": bindings, "forkEvents": events, "forkEventOrder": order})
}

// Flat fingerprints of retained metadata preserve source fork history without
// recursively copying entire ancestor metadata snapshots into each binding.
func forkMetadataDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var object map[string]any
	if err := modelStrictJSON(raw, &object); err != nil {
		return "", err
	}
	return DigestEntry(CanonicalVersion, object)
}
func equalLineage(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func changedEntryFields(a, b map[string]any, ignore map[string]bool) ([]string, error) {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	var changed []string
	for _, k := range ordered {
		if ignore[k] {
			continue
		}
		av, ap := a[k]
		bv, bp := b[k]
		if ap != bp {
			changed = append(changed, k)
			continue
		}
		x, err := EncodeEntry(CanonicalVersion, map[string]any{"v": av})
		if err != nil {
			return nil, err
		}
		y, err := EncodeEntry(CanonicalVersion, map[string]any{"v": bv})
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(x, y) {
			changed = append(changed, k)
		}
	}
	return changed, nil
}
