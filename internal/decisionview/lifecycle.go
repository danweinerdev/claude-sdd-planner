package decisionview

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/dlg"
	"gopkg.in/yaml.v3"
)

// ForkOverrideSnapshot is the immutable authority graph captured for an
// override or reconciliation preview.
type ForkOverrideSnapshot struct {
	Local       *Collection
	Collections map[CollectionID]*Collection
}

// ForkOverridePreview exposes the exact-byte proposal envelope and the local
// decision that would carry the prospective authority.
type ForkOverridePreview struct {
	Envelope *PreviewEnvelope
	Decision map[string]any
}

// PreviewForkOverride previews a whole-decision override or reconciliation.
func PreviewForkOverride(snapshot ForkOverrideSnapshot, proposal json.RawMessage) (*ForkOverridePreview, error) {
	var request forkOverrideRequest
	if err := modelStrictJSON(proposal, &request); err != nil {
		return nil, err
	}
	if request.Version != Version1 || (request.Operation != "override" && request.Operation != "reconcile") || request.Target.Validate() != nil {
		return nil, fmt.Errorf("decisionview: invalid override operation or target")
	}
	for _, text := range []string{request.Statement, request.Rationale, request.Confirmation, request.Kind, request.OperationID} {
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("decisionview: whole replacement requires explicit complete fields")
		}
	}
	if request.Scope == nil {
		return nil, fmt.Errorf("decisionview: whole replacement requires explicit scope")
	}
	local := snapshot.Local
	if local == nil || local.Metadata == nil || len(local.Files) == 0 || local.Locator.Root != SourceRootPlanning || local.Files[0].Path != local.Locator.Path {
		return nil, fmt.Errorf("decisionview: selected canonical local fork snapshot required")
	}
	if err := local.Metadata.Validate(); err != nil {
		return nil, err
	}
	if adoptionMode(local) != "fork" {
		return nil, fmt.Errorf("decisionview: detached authority has no inherited override target")
	}
	index := adoptionParentBindingIndex(local.Metadata)
	if index < 0 {
		return nil, fmt.Errorf("decisionview: missing parent binding")
	}
	binding := local.Metadata.Bindings[index]
	sources := map[CollectionID]*Collection{}
	for id, c := range snapshot.Collections {
		sources[id] = c
	}
	sources[local.ID] = local
	parent := sources[binding.CollectionID]
	if parent == nil {
		return nil, fmt.Errorf("decisionview: immediate parent was not captured")
	}
	var parentView *ResolvedView
	var err error
	if parent.Metadata == nil {
		parentView, err = adoptionSourceView(parent, binding.OwnerID)
	} else {
		parentView, err = Compose(parent.ID, adoptionMode(parent), sources)
	}
	if err != nil {
		return nil, err
	}
	if parentView.Resolution != ResolutionComplete {
		return nil, fmt.Errorf("decisionview: immediate parent authority is unresolved")
	}
	var target *ResolvedDecision
	for i := range parentView.Records {
		r := &parentView.Records[i]
		if r.ID == request.Target && r.Applicability == "binding" && r.OriginalStatus == "accepted" {
			target = r
		}
	}
	if target == nil {
		return nil, fmt.Errorf("decisionview: target is not effective in the immediate parent's view")
	}
	before, err := Compose(local.ID, "fork", sources)
	if err != nil {
		return nil, err
	}
	replaced := ""
	if request.Operation == "reconcile" {
		collection, id, ok := ParseQualifiedID(string(request.Replacement))
		if !ok || collection != local.ID {
			return nil, fmt.Errorf("decisionview: reconciliation requires an explicit local replacement")
		}
		old := local.Entries[id]
		if old == nil || old["status"] != "accepted" || old["override"] == nil {
			return nil, fmt.Errorf("decisionview: replacement is not an accepted local override")
		}
		if local.EntryPaths[id] != local.Locator.Path {
			return nil, fmt.Errorf("decisionview: replacement must be in the canonical local file")
		}
		replaced = id
	} else if request.Replacement != "" {
		return nil, fmt.Errorf("decisionview: an initial override cannot supersede implicitly")
	}
	for _, r := range before.Records {
		if r.CollectionID != local.ID || r.OriginalStatus != "accepted" || r.Override == nil || r.Applicability == "inactive-override" {
			continue
		}
		_, id, _ := ParseQualifiedID(string(r.ID))
		if id != replaced && r.Override.Target == request.Target {
			return nil, fmt.Errorf("decisionview: another accepted local replacement already targets this decision")
		}
	}
	for _, event := range local.Metadata.Events {
		if event.ID == request.OperationID || event.OperationID == request.OperationID {
			return nil, fmt.Errorf("decisionview: operation identity already used")
		}
	}
	for _, id := range local.Metadata.OperationIDs {
		if id == request.OperationID {
			return nil, fmt.Errorf("decisionview: operation identity already used")
		}
	}
	lineage := append([]string{binding.ID}, target.Lineage...)
	basis, err := CreateBasis(binding.ID, sources[target.CollectionID], request.Target, lineage)
	if err != nil {
		return nil, err
	}
	max := 0
	for id := range local.Entries {
		if len(id) != 6 || !strings.HasPrefix(id, "D-") {
			return nil, fmt.Errorf("decisionview: malformed local decision identity")
		}
		n, err := strconv.Atoi(id[2:])
		if err != nil {
			return nil, err
		}
		if n > max {
			max = n
		}
	}
	if max >= 9999 {
		return nil, fmt.Errorf("decisionview: local decision identity space exhausted")
	}
	id := fmt.Sprintf("D-%04d", max+1)
	entry := map[string]any{"id": id, "status": "accepted", "date": request.Date, "kind": request.Kind, "decided_by": request.DecidedBy, "statement": request.Statement, "rationale": request.Rationale, "scope": request.Scope, "confirmation": request.Confirmation, "rejected": request.Rejected, "tags": request.Tags, "reversibility": request.Reversibility, "override": OverrideDeclaration{Target: request.Target, Basis: basis}}
	if replaced != "" {
		entry["supersedes"] = replaced
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	err = modelStrictJSON(encoded, &normalized)
	if err != nil {
		return nil, err
	}
	entry = normalized
	next := *local
	next.Entries = map[string]map[string]any{}
	next.EntryPaths = map[string]string{}
	for oldID, value := range local.Entries {
		copy, err := copyEntry(value)
		if err != nil {
			return nil, err
		}
		next.Entries[oldID] = copy
		next.EntryPaths[oldID] = local.EntryPaths[oldID]
	}
	if replaced != "" {
		next.Entries[replaced]["status"] = "superseded"
		next.Entries[replaced]["superseded_by"] = id
	}
	next.Entries[id] = entry
	next.EntryPaths[id] = local.Locator.Path
	meta := *local.Metadata
	meta.Events = append([]AuthorityEvent(nil), meta.Events...)
	meta.OperationIDs = append([]string(nil), meta.OperationIDs...)
	event := AuthorityEvent{Version: Version1, ID: request.OperationID, Kind: EventKind(request.Operation), Date: request.Date, DecidedBy: request.DecidedBy, Target: &request.Target, Basis: &basis, OperationID: request.OperationID}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	meta.Events = append(meta.Events, event)
	meta.OperationIDs = append(meta.OperationIDs, request.OperationID)
	next.Metadata = &meta
	raw, err := renderOverrideLedger(local.Files[0].Source, &next, request.Date)
	if err != nil {
		return nil, err
	}
	next.Files = append([]CollectionFile(nil), local.Files...)
	next.Files[0].Source = raw
	sources[local.ID] = &next
	after, err := Compose(local.ID, "fork", sources)
	if err != nil {
		return nil, err
	}
	if after.Resolution == ResolutionInvalid {
		return nil, fmt.Errorf("decisionview: replacement produces invalid authority: %v", after.Diagnostics)
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
	envelope, err := NewPreviewEnvelope(request.Operation, request.OperationID, request.Date, proposal, []PreviewFileChange{{Root: SourceRootPlanning, Path: local.Locator.Path, BeforeExists: true, Before: string(local.Files[0].Source), After: string(raw)}}, dependencies, before, after)
	if err != nil {
		return nil, err
	}
	return &ForkOverridePreview{Envelope: envelope, Decision: entry}, nil
}

type forkOverrideRequest struct {
	Version       SchemaVersion `json:"version"`
	Operation     string        `json:"operation"`
	OperationID   string        `json:"operationId"`
	Date          string        `json:"date"`
	Target        QualifiedID   `json:"target"`
	Replacement   QualifiedID   `json:"replacement"`
	Kind          string        `json:"kind"`
	DecidedBy     string        `json:"decidedBy"`
	Statement     string        `json:"statement"`
	Rationale     string        `json:"rationale"`
	Scope         []string      `json:"scope"`
	Confirmation  string        `json:"confirmation"`
	Rejected      []string      `json:"rejected"`
	Tags          []string      `json:"tags"`
	Reversibility string        `json:"reversibility"`
}

func renderOverrideLedger(before []byte, next *Collection, date string) ([]byte, error) {
	lines := strings.Split(string(before), "\n")
	end := 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "---" {
		end++
	}
	if len(lines) == 0 || lines[0] != "---" || end == len(lines) {
		return nil, fmt.Errorf("decisionview: malformed local ledger frontmatter")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("decisionview: ledger metadata must be a mapping")
	}
	var entries []map[string]any
	for _, id := range continuityIDs(next.Entries) {
		if next.EntryPaths[id] == next.Locator.Path {
			entries = append(entries, next.Entries[id])
		}
	}
	values := map[string]any{"decisions": entries, "fork": next.Metadata, "updated": date}
	mapping := doc.Content[0]
	for i := 0; i < len(mapping.Content); i += 2 {
		key := mapping.Content[i].Value
		value, changed := values[key]
		if !changed {
			continue
		}
		n, err := modelMarshalYAML(value)
		if err != nil {
			return nil, err
		}
		replacement := n.(*yaml.Node)
		var block func(*yaml.Node)
		block = func(n *yaml.Node) {
			n.Style = 0
			for _, child := range n.Content {
				block(child)
			}
		}
		block(replacement)
		mapping.Content[i+1] = replacement
		delete(values, key)
	}
	if len(values) != 0 {
		return nil, fmt.Errorf("decisionview: canonical ledger lacks required fields")
	}
	body, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	raw := []byte("---\n" + string(body) + strings.Join(lines[end:], "\n"))
	canonical, diagnostics := dlg.ParseLedgerBytes(next.Locator.Path, raw)
	if canonical == nil {
		return nil, fmt.Errorf("decisionview: invalid prospective ledger: %v", diagnostics)
	}
	var archives []*dlg.Ledger
	for _, file := range next.Files {
		if !file.Archive {
			continue
		}
		archive, diags := dlg.ParseLedgerBytes(file.Path, file.Source)
		diagnostics = append(diagnostics, diags...)
		if archive == nil {
			return nil, fmt.Errorf("decisionview: invalid local archive")
		}
		archives = append(archives, archive)
	}
	more, _ := dlg.ValidateExplicitCollection(canonical, archives)
	diagnostics = append(diagnostics, more...)
	for _, d := range diagnostics {
		if d.Severity.Invalidating() {
			return nil, fmt.Errorf("decisionview: prospective ledger fails %s: %s", d.Code, d.Message)
		}
	}
	return raw, nil
}
