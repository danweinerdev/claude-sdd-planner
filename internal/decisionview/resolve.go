package decisionview

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type OverrideDeclaration struct {
	Target QualifiedID   `json:"target"`
	Basis  OverrideBasis `json:"basis"`
}
type ResolvedDecision struct {
	ID             QualifiedID          `json:"qualified_id"`
	CollectionID   CollectionID         `json:"collection_id"`
	Original       map[string]any       `json:"original"`
	OriginalStatus string               `json:"original_status"`
	Applicability  string               `json:"applicability"`
	Replacement    QualifiedID          `json:"replacement,omitempty"`
	Override       *OverrideDeclaration `json:"override,omitempty"`
	Lineage        []string             `json:"lineage,omitempty"`
	Source         SourceLocator        `json:"source"`
	History        string               `json:"history,omitempty"`
}
type ResolvedView struct {
	Version     int                `json:"version"`
	OwnerID     OwnerID            `json:"repository_id"`
	LocalID     CollectionID       `json:"ledger_id"`
	Resolution  Resolution         `json:"resolution"`
	Records     []ResolvedDecision `json:"records"`
	Diagnostics []Diagnostic       `json:"diagnostics"`
}

func Compose(localID CollectionID, mode string, sources map[CollectionID]*Collection) (*ResolvedView, error) {
	if mode != "fork" && mode != "detached" {
		return nil, fmt.Errorf("%w: unsupported selection mode", ErrInvalidCollection)
	}
	active := map[CollectionID]bool{}
	var visit func(CollectionID, int) (*ResolvedView, error)
	visit = func(id CollectionID, depth int) (*ResolvedView, error) {
		v := &ResolvedView{Version: 1, LocalID: id, Resolution: ResolutionComplete, Records: []ResolvedDecision{}, Diagnostics: []Diagnostic{}}
		issue := func(code, msg string) {
			v.Diagnostics = append(v.Diagnostics, Diagnostic{Code: code, Severity: Error, Message: msg, Correction: "Inspect original records and reconcile the explicit fork relationship before relying on it."})
			v.Resolution = ResolutionInvalid
		}
		if depth >= 32 || active[id] {
			issue("FDL020", "Inheritance cycle or maximum depth exceeded")
			return v, nil
		}
		c := sources[id]
		if c == nil || c.ID != id {
			issue("FDL020", "Missing or mismatched source collection "+string(id))
			return v, nil
		}
		for _, d := range c.Diagnostics {
			location := c.Locator.Path
			for _, f := range c.Files {
				if strings.HasSuffix(strings.ReplaceAll(d.Path, "\\", "/"), "/"+f.Path) || d.Path == f.Path {
					location = f.Path
					break
				}
			}
			v.Diagnostics = append(v.Diagnostics, Diagnostic{Code: d.Code, Severity: Severity(d.Severity), Path: location, Line: d.Line, Message: "Source history " + string(c.ID) + ": " + d.Message, Correction: d.Correction})
		}
		if err := continuityInput(c); err != nil {
			issue("FDL020", err.Error())
			return v, nil
		}
		active[id] = true
		defer delete(active, id)
		var parentBinding *Binding
		if c.Metadata != nil {
			if err := c.Metadata.Validate(); err != nil {
				issue("FDL020", err.Error())
				return v, nil
			}
			if c.Metadata.LedgerID != id {
				issue("FDL020", "Fork metadata identity mismatch")
				return v, nil
			}
			v.OwnerID = c.Metadata.RepositoryID
			seen := map[string]bool{}
			for i := range c.Metadata.Bindings {
				b := &c.Metadata.Bindings[i]
				if b.ID == "" || seen[b.ID] {
					issue("FDL020", "Duplicate or missing binding ID")
					return v, nil
				}
				seen[b.ID] = true
				if b.ID == c.Metadata.ParentBindingID {
					parentBinding = b
				}
			}
			detached := lastModeEvent(c.Metadata.Events) == EventDetach
			if id == localID && mode == "fork" && detached {
				issue("FDL020", "Fork selection conflicts with the retained detachment event")
				return v, nil
			}
			if id == localID && mode == "detached" {
				if !detached {
					issue("FDL020", "Detached selection lacks its explicit authority event")
					return v, nil
				}
				parentBinding = nil
			} else if detached {
				parentBinding = nil
			} else if c.Metadata.ParentBindingID == "" {
				if !detached {
					issue("FDL020", "Fork source lost its parent declaration without detachment")
					return v, nil
				}
			} else if parentBinding == nil {
				issue("FDL020", "Declared parent binding does not exist")
				return v, nil
			}
		} else if id == localID {
			issue("FDL020", "Selected local collection lacks fork metadata")
			return v, nil
		}
		if parentBinding != nil {
			parent, err := visit(parentBinding.CollectionID, depth+1)
			if err != nil {
				return nil, err
			}
			v.Diagnostics = append(v.Diagnostics, parent.Diagnostics...)
			if parent.Resolution != ResolutionComplete {
				v.Resolution = parent.Resolution
			}
			for _, record := range parent.Records {
				record.Lineage = append([]string{parentBinding.ID}, record.Lineage...)
				v.Records = append(v.Records, record)
			}
			if parentSource := sources[parentBinding.CollectionID]; parentSource != nil {
				diags, err := CheckContinuity(*parentBinding, parentSource)
				if err != nil {
					issue("FDL020", err.Error())
				} else {
					v.Diagnostics = append(v.Diagnostics, diags...)
					if len(diags) > 0 {
						v.Resolution = ResolutionInvalid
					}
				}
			}
		}
		parentIndices := map[QualifiedID]int{}
		for i, r := range v.Records {
			if r.Applicability == "binding" {
				parentIndices[r.ID] = i
			}
		}
		restored := map[QualifiedID]bool{}
		if c.Metadata != nil {
			seen := map[string]bool{}
			for _, event := range c.Metadata.Events {
				if seen[event.ID] {
					issue("FDL020", "Duplicate authority event ID")
					continue
				}
				seen[event.ID] = true
				if event.Kind == EventRestore {
					if event.Target == nil {
						issue("FDL020", "Restoration lacks a local replacement identity")
						continue
					}
					collection, local, ok := ParseQualifiedID(string(*event.Target))
					entry := c.Entries[local]
					if !ok || collection != id || entry == nil || entry["override"] == nil || (entry["status"] != "accepted" && entry["status"] != "superseded") {
						issue("FDL020", "Restoration does not name a local override")
						continue
					}
					restored[*event.Target] = true
				}
			}
		}
		start := len(v.Records)
		targetCounts := map[QualifiedID]int{}
		for _, local := range continuityIDs(c.Entries) {
			entry, err := copyEntry(c.Entries[local])
			if err != nil {
				return nil, err
			}
			status, _ := entry["status"].(string)
			qid := QualifiedID("ledger:" + string(id) + ":" + local)
			r := ResolvedDecision{ID: qid, CollectionID: id, Original: entry, OriginalStatus: status, Applicability: "historical", Source: c.Locator, History: c.History}
			if actual := c.EntryPaths[local]; actual != "" {
				r.Source = SourceLocator{Root: c.Locator.Root, Path: actual}
			}
			if status == "accepted" {
				r.Applicability = "binding"
			}
			if raw, present := entry["override"]; present {
				if c.Metadata == nil {
					issue("FDL020", "Override relation lacks a fork declaration")
				}
				decl, err := decodeOverride(raw)
				if err != nil {
					issue("FDL020", err.Error())
					r.Applicability = "unresolved"
				} else {
					r.Override = decl
					if restored[qid] {
						r.Applicability = "inactive-override"
					} else if status == "accepted" {
						if !nonemptyEntryString(entry, "statement") || !nonemptyEntryString(entry, "rationale") || !nonemptyEntryString(entry, "confirmation") {
							issue("FDL020", "Accepted override lacks a complete statement, rationale or confirmation")
							r.Applicability = "unresolved"
						}
						if _, explicit := entry["scope"]; !explicit {
							issue("FDL020", "Accepted override must state its complete scope")
							r.Applicability = "unresolved"
						}
						targetCounts[decl.Target]++
					}
				}
			}
			v.Records = append(v.Records, r)
		}
		for i := start; i < len(v.Records); i++ {
			r := &v.Records[i]
			if r.Override == nil || r.Applicability != "binding" {
				continue
			}
			if targetCounts[r.Override.Target] > 1 {
				issue("FDL020", "Multiple accepted replacements target "+string(r.Override.Target))
				r.Applicability = "unresolved"
				continue
			}
			at, visible := parentIndices[r.Override.Target]
			if !visible || parentBinding == nil || v.Resolution != ResolutionComplete {
				r.Applicability = "unresolved"
				if v.Resolution == ResolutionComplete {
					v.Resolution = ResolutionReconciliationNeeded
				}
				v.Diagnostics = append(v.Diagnostics, staleOverrideDiagnostic(r.ID, "Target is not a currently effective immediate-parent decision"))
				continue
			}
			targetCollection, _, _ := ParseQualifiedID(string(r.Override.Target))
			comparison, err := CompareBasis(r.Override.Basis, sources[targetCollection], parentBinding.ID, v.Records[at].Lineage)
			if err != nil {
				issue("FDL020", err.Error())
				r.Applicability = "unresolved"
				continue
			}
			if !comparison.Current {
				v.Resolution = ResolutionReconciliationNeeded
				r.Applicability = "unresolved"
				v.Records[at].Applicability = "unresolved"
				v.Diagnostics = append(v.Diagnostics, staleOverrideDiagnostic(r.ID, "Approved target or inheritance basis changed"))
				continue
			}
			v.Records[at].Applicability = "overridden"
			v.Records[at].Replacement = r.ID
			r.Lineage = append(append([]string(nil), v.Records[at].Lineage...), "override:"+string(r.ID))
		}
		if v.Resolution != ResolutionComplete {
			// Until scope context has been supplied, conservatively expose no
			// partial binding set as clean authority. Original records survive.
			for i := range v.Records {
				if v.Records[i].Applicability == "binding" {
					v.Records[i].Applicability = "unresolved"
				}
			}
		}
		sort.Slice(v.Records, func(i, j int) bool { return v.Records[i].ID < v.Records[j].ID })
		v.Diagnostics = append(v.Diagnostics, CollisionCandidates(v.Records, nil)...)
		sort.SliceStable(v.Diagnostics, func(i, j int) bool {
			a, b := v.Diagnostics[i], v.Diagnostics[j]
			if a.Path != b.Path {
				return a.Path < b.Path
			}
			if a.Code != b.Code {
				return a.Code < b.Code
			}
			return a.Message < b.Message
		})
		unique := v.Diagnostics[:0]
		for _, d := range v.Diagnostics {
			if len(unique) == 0 || unique[len(unique)-1] != d {
				unique = append(unique, d)
			}
		}
		v.Diagnostics = unique
		return v, nil
	}
	return visit(localID, 0)
}

func decodeOverride(value any) (*OverrideDeclaration, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decl OverrideDeclaration
	if err := modelStrictJSON(raw, &decl); err != nil {
		return nil, err
	}
	if err := decl.Target.Validate(); err != nil {
		return nil, err
	}
	if decl.Target != decl.Basis.TargetID {
		return nil, fmt.Errorf("override target and basis identity differ")
	}
	return &decl, nil
}
func nonemptyEntryString(entry map[string]any, key string) bool {
	s, ok := entry[key].(string)
	return ok && strings.TrimSpace(s) != ""
}
func staleOverrideDiagnostic(id QualifiedID, reason string) Diagnostic {
	return Diagnostic{Code: "FDL021", Severity: Error, Message: reason + ": " + string(id), Correction: "Inspect old/current target and explicitly reconcile or restore this override; no automatic transfer is permitted."}
}
func lastModeEvent(events []AuthorityEvent) EventKind {
	var mode EventKind
	for _, e := range events {
		if e.Kind == EventAdopt || e.Kind == EventDetach {
			mode = e.Kind
		}
	}
	return mode
}
