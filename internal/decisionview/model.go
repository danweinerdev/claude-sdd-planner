// Package decisionview models explicitly selected fork decision ledgers.
// Decoding checks the wire contract, not whether an override is authorized;
// collection integrity, basis comparison and effective authority are separate.
package decisionview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type SchemaVersion int

const (
	VersionUnknown SchemaVersion = 0
	Version1       SchemaVersion = 1
)

func (v SchemaVersion) String() string { return fmt.Sprint(int(v)) }
func (v SchemaVersion) Validate() error {
	if v != Version1 {
		return fmt.Errorf("decisionview: unsupported schema version %d", v)
	}
	return nil
}
func (v SchemaVersion) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(int(v))
}
func (v *SchemaVersion) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("decisionview: integer version required: %w", err)
	}
	next := SchemaVersion(n)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
func (v SchemaVersion) MarshalYAML() (any, error) { return int(v), v.Validate() }
func (v *SchemaVersion) UnmarshalYAML(n *yaml.Node) error {
	data, err := modelYAMLJSON(n)
	if err != nil {
		return err
	}
	return v.UnmarshalJSON(data)
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var localDecisionIDRe = regexp.MustCompile(`^D-[0-9]{4,}$`)
var integerJSONRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

type CollectionID string
type OwnerID string
type QualifiedID string

func (id CollectionID) String() string  { return string(id) }
func (id OwnerID) String() string       { return string(id) }
func (id QualifiedID) String() string   { return string(id) }
func (id CollectionID) Validate() error { return validateUUID("collection", string(id)) }
func (id OwnerID) Validate() error      { return validateUUID("owner", string(id)) }
func validateUUID(kind, id string) error {
	if !uuidRe.MatchString(id) {
		return fmt.Errorf("decisionview: %s ID %q must be a lowercase UUID", kind, id)
	}
	return nil
}
func ParseQualifiedID(id string) (CollectionID, string, bool) {
	p := strings.SplitN(id, ":", 3)
	if len(p) != 3 || p[0] != "ledger" || !uuidRe.MatchString(p[1]) || !localDecisionIDRe.MatchString(p[2]) {
		return "", "", false
	}
	return CollectionID(p[1]), p[2], true
}
func (id QualifiedID) Validate() error {
	if _, _, ok := ParseQualifiedID(string(id)); !ok {
		return fmt.Errorf("decisionview: invalid qualified decision ID %q", id)
	}
	return nil
}

type SourceRoot string

const (
	SourceRootPlanning   SourceRoot = "planning"
	SourceRootRepository SourceRoot = "repository"
)

type SourceLocator struct {
	Root     SourceRoot `json:"root" yaml:"root"`
	Path     string     `json:"path" yaml:"path"`
	Archives []string   `json:"archives,omitempty" yaml:"archives,omitempty"`
}

func (v SourceLocator) Validate() error {
	if v.Root != SourceRootPlanning && v.Root != SourceRootRepository {
		return fmt.Errorf("decisionview: unsupported source root %q", v.Root)
	}
	if err := validateRelativeLocator(v.Path); err != nil {
		return err
	}
	for _, a := range v.Archives {
		if err := validateRelativeLocator(a); err != nil {
			return err
		}
	}
	return nil
}

// This is syntax checking only. Safe filesystem opening and containment are
// required when resolving a locator; a syntactically valid path is not trusted.
func validateRelativeLocator(p string) error {
	if p == "" || strings.TrimSpace(p) == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:\x00\r\n") {
		return fmt.Errorf("decisionview: unsafe relative locator %q", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "." || part == "" {
			return fmt.Errorf("decisionview: non-canonical relative locator %q", p)
		}
	}
	return nil
}

// ForkConfig is the decisionLog object, not the entire planning configuration.
// The enclosing config carries repositoryId and may retain unrelated keys.
type ForkConfig struct {
	Version     SchemaVersion   `json:"version" yaml:"version"`
	Mode        string          `json:"mode" yaml:"mode"`
	Path        string          `json:"path" yaml:"path"`
	LedgerID    CollectionID    `json:"ledgerId" yaml:"ledgerId"`
	Transaction *TransactionRef `json:"transaction,omitempty" yaml:"transaction,omitempty"`
}
type TransactionRef struct {
	ID      string `json:"id" yaml:"id"`
	Journal string `json:"journal" yaml:"journal"`
}

func (v ForkConfig) Validate() error {
	if err := v.Version.Validate(); err != nil {
		return err
	}
	if v.Mode != "fork" && v.Mode != "detached" {
		return fmt.Errorf("decisionview: unsupported fork mode %q", v.Mode)
	}
	if err := v.LedgerID.Validate(); err != nil {
		return err
	}
	if err := validateRelativeLocator(v.Path); err != nil {
		return err
	}
	if v.Transaction != nil {
		if strings.TrimSpace(v.Transaction.ID) == "" {
			return fmt.Errorf("decisionview: transaction ID is required")
		}
		if err := validateRelativeLocator(v.Transaction.Journal); err != nil {
			return err
		}
	}
	return nil
}

// Binding preserves the source baseline. Canonical content is data here;
// baseline completeness and continuity are checked by the collection resolver.
type Binding struct {
	Version          SchemaVersion     `json:"version" yaml:"version"`
	ID               string            `json:"id,omitempty" yaml:"id,omitempty"`
	OwnerID          OwnerID           `json:"ownerId" yaml:"ownerId"`
	CollectionID     CollectionID      `json:"collectionId" yaml:"collectionId"`
	Source           SourceLocator     `json:"source" yaml:"source"`
	ParentBindingID  string            `json:"parentBindingId,omitempty" yaml:"parentBindingId,omitempty"`
	Description      string            `json:"description,omitempty" yaml:"description,omitempty"`
	CanonicalHash    string            `json:"canonicalHash,omitempty" yaml:"canonicalHash,omitempty"`
	CanonicalContent []map[string]any  `json:"canonicalContent,omitempty" yaml:"canonicalContent,omitempty"`
	ForkSource       bool              `json:"forkSource,omitempty" yaml:"forkSource,omitempty"`
	ForkBindings     map[string]string `json:"forkBindings,omitempty" yaml:"forkBindings,omitempty"`
	ForkEvents       map[string]string `json:"forkEvents,omitempty" yaml:"forkEvents,omitempty"`
	ForkEventOrder   []string          `json:"forkEventOrder,omitempty" yaml:"forkEventOrder,omitempty"`
}

func (v Binding) Validate() error {
	if err := v.Version.Validate(); err != nil {
		return err
	}
	if err := v.OwnerID.Validate(); err != nil {
		return err
	}
	if err := v.CollectionID.Validate(); err != nil {
		return err
	}
	return v.Source.Validate()
}

type OverrideBasis struct {
	Version          SchemaVersion   `json:"version" yaml:"version"`
	TargetID         QualifiedID     `json:"targetId" yaml:"targetId"`
	BindingID        string          `json:"bindingId,omitempty" yaml:"bindingId,omitempty"`
	Canonicalization string          `json:"canonicalization,omitempty" yaml:"canonicalization,omitempty"`
	CanonicalHash    string          `json:"canonicalHash,omitempty" yaml:"canonicalHash,omitempty"`
	CanonicalContent json.RawMessage `json:"canonicalContent,omitempty" yaml:"canonicalContent,omitempty"`
	Lineage          []string        `json:"lineage,omitempty" yaml:"lineage,omitempty"`
}

func (v OverrideBasis) Validate() error {
	if err := v.Version.Validate(); err != nil {
		return err
	}
	if err := v.TargetID.Validate(); err != nil {
		return err
	}
	if v.Canonicalization != "" && v.Canonicalization != "entry-v1" {
		return fmt.Errorf("decisionview: unsupported canonicalization %q", v.Canonicalization)
	}
	if len(v.CanonicalContent) > 0 {
		var obj map[string]any
		if err := modelStrictJSON(v.CanonicalContent, &obj); err != nil {
			return err
		}
		if obj == nil {
			return fmt.Errorf("decisionview: canonical entry must be an object")
		}
	}
	return nil
}

type EventKind string

const (
	EventAdopt     EventKind = "adopt"
	EventOverride  EventKind = "override"
	EventReconcile EventKind = "reconcile"
	EventRestore   EventKind = "restore"
	EventRebind    EventKind = "rebind"
	EventDetach    EventKind = "detach"
)

func (v EventKind) Validate() error {
	switch v {
	case EventAdopt, EventOverride, EventReconcile, EventRestore, EventRebind, EventDetach:
		return nil
	}
	return fmt.Errorf("decisionview: unknown authority event kind %q", v)
}

type AuthorityEvent struct {
	Version      SchemaVersion  `json:"version" yaml:"version"`
	ID           string         `json:"id" yaml:"id"`
	Kind         EventKind      `json:"kind" yaml:"kind"`
	Date         string         `json:"date" yaml:"date"`
	DecidedBy    string         `json:"decidedBy" yaml:"decidedBy"`
	Target       *QualifiedID   `json:"target,omitempty" yaml:"target,omitempty"`
	Basis        *OverrideBasis `json:"basis,omitempty" yaml:"basis,omitempty"`
	Statement    string         `json:"statement,omitempty" yaml:"statement,omitempty"`
	Rationale    string         `json:"rationale,omitempty" yaml:"rationale,omitempty"`
	Confirmation string         `json:"confirmation,omitempty" yaml:"confirmation,omitempty"`
	Scope        []string       `json:"scope,omitempty" yaml:"scope,omitempty"`
	OperationID  string         `json:"operationId,omitempty" yaml:"operationId,omitempty"`
}

func (v AuthorityEvent) Validate() error {
	if err := v.Version.Validate(); err != nil {
		return err
	}
	if err := v.Kind.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(v.ID) == "" {
		return fmt.Errorf("decisionview: event ID is required")
	}
	if _, err := time.Parse("2006-01-02", v.Date); err != nil {
		return fmt.Errorf("decisionview: invalid event date %q", v.Date)
	}
	if v.DecidedBy != "user" && v.DecidedBy != "user-approved" {
		return fmt.Errorf("decisionview: authority event requires user approval")
	}
	if v.Target != nil {
		if err := v.Target.Validate(); err != nil {
			return err
		}
	}
	if v.Basis != nil {
		return v.Basis.Validate()
	}
	return nil
}

type LegacyContext struct {
	Root      SourceRoot   `json:"root" yaml:"root"`
	Path      string       `json:"path" yaml:"path"`
	Namespace CollectionID `json:"namespace" yaml:"namespace"`
	LocalIDs  []string     `json:"localIds" yaml:"localIds"`
}
type ForkMetadata struct {
	Version         SchemaVersion    `json:"version" yaml:"version"`
	LedgerID        CollectionID     `json:"ledgerId" yaml:"ledgerId"`
	RepositoryID    OwnerID          `json:"repositoryId" yaml:"repositoryId"`
	Archives        []string         `json:"archives,omitempty" yaml:"archives,omitempty"`
	ParentBindingID string           `json:"parentBindingId,omitempty" yaml:"parentBindingId,omitempty"`
	Bindings        []Binding        `json:"bindings,omitempty" yaml:"bindings,omitempty"`
	Events          []AuthorityEvent `json:"events,omitempty" yaml:"events,omitempty"`
	LegacyContexts  []LegacyContext  `json:"legacyContexts,omitempty" yaml:"legacyContexts,omitempty"`
	OperationIDs    []string         `json:"operationIds,omitempty" yaml:"operationIds,omitempty"`
}

func (v ForkMetadata) Validate() error {
	if err := v.Version.Validate(); err != nil {
		return err
	}
	if err := v.LedgerID.Validate(); err != nil {
		return err
	}
	if err := v.RepositoryID.Validate(); err != nil {
		return err
	}
	for _, archive := range v.Archives {
		if err := validateRelativeLocator(archive); err != nil {
			return err
		}
	}
	for _, b := range v.Bindings {
		if err := b.Validate(); err != nil {
			return err
		}
	}
	for _, e := range v.Events {
		if err := e.Validate(); err != nil {
			return err
		}
	}
	for _, c := range v.LegacyContexts {
		if err := (SourceLocator{Root: c.Root, Path: c.Path}).Validate(); err != nil {
			return err
		}
		if err := c.Namespace.Validate(); err != nil {
			return err
		}
		for _, id := range c.LocalIDs {
			if !localDecisionIDRe.MatchString(id) {
				return fmt.Errorf("decisionview: invalid legacy ID %q", id)
			}
		}
	}
	return nil
}

type Resolution string

const (
	ResolutionComplete             Resolution = "complete"
	ResolutionReconciliationNeeded Resolution = "reconciliation-required"
	ResolutionInvalid              Resolution = "invalid"
	ResolutionRecoveryNeeded       Resolution = "recovery-required"
)

type Severity string

const (
	Error       Severity = "error"
	Operational Severity = "operational"
	Candidate   Severity = "candidate"
	Warning     Severity = "warning"
	Waived      Severity = "waived"
)

type Diagnostic struct {
	Severity   Severity `json:"severity" yaml:"severity"`
	Code       string   `json:"code" yaml:"code"`
	Message    string   `json:"message" yaml:"message"`
	Path       string   `json:"path,omitempty" yaml:"path,omitempty"`
	Line       int      `json:"line,omitempty" yaml:"line,omitempty"`
	Correction string   `json:"correction,omitempty" yaml:"correction,omitempty"`
}
type EffectiveView struct {
	Resolution  Resolution       `json:"resolution" yaml:"resolution"`
	Config      *ForkConfig      `json:"config,omitempty" yaml:"config,omitempty"`
	Bindings    []Binding        `json:"bindings,omitempty" yaml:"bindings,omitempty"`
	Events      []AuthorityEvent `json:"events,omitempty" yaml:"events,omitempty"`
	Diagnostics []Diagnostic     `json:"diagnostics,omitempty" yaml:"diagnostics,omitempty"`
	Entries     []map[string]any `json:"entries,omitempty" yaml:"entries,omitempty"`
}

func DecodeForkConfig(data []byte) (*ForkConfig, error) {
	var value ForkConfig
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return &value, nil
}
func DecodeForkConfigYAML(data []byte) (*ForkConfig, error) {
	var value ForkConfig
	if err := modelDecodeYAMLDocument(data, &value); err != nil {
		return nil, err
	}
	return &value, nil
}
func DecodeForkMetadata(data []byte) (*ForkMetadata, error) {
	var value ForkMetadata
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return &value, nil
}
func DecodeForkMetadataYAML(data []byte) (*ForkMetadata, error) {
	var value ForkMetadata
	if err := modelDecodeYAMLDocument(data, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func (v *ForkConfig) UnmarshalJSON(b []byte) error {
	type wire ForkConfig
	var next wire
	if err := modelStrictJSON(b, &next); err != nil {
		return err
	}
	value := ForkConfig(next)
	if err := value.Validate(); err != nil {
		return err
	}
	*v = value
	return nil
}
func (v *SourceLocator) UnmarshalJSON(b []byte) error {
	type wire SourceLocator
	var next wire
	if err := modelStrictJSON(b, &next); err != nil {
		return err
	}
	value := SourceLocator(next)
	if err := value.Validate(); err != nil {
		return err
	}
	*v = value
	return nil
}
func (v *Binding) UnmarshalJSON(b []byte) error {
	type wire Binding
	var next wire
	if err := modelStrictJSON(b, &next); err != nil {
		return err
	}
	value := Binding(next)
	if err := value.Validate(); err != nil {
		return err
	}
	*v = value
	return nil
}
func (v *OverrideBasis) UnmarshalJSON(b []byte) error {
	type wire OverrideBasis
	var next wire
	if err := modelStrictJSON(b, &next); err != nil {
		return err
	}
	value := OverrideBasis(next)
	if err := value.Validate(); err != nil {
		return err
	}
	*v = value
	return nil
}
func (v *AuthorityEvent) UnmarshalJSON(b []byte) error {
	type wire AuthorityEvent
	var next wire
	if err := modelStrictJSON(b, &next); err != nil {
		return err
	}
	value := AuthorityEvent(next)
	if err := value.Validate(); err != nil {
		return err
	}
	*v = value
	return nil
}
func (v *ForkMetadata) UnmarshalJSON(b []byte) error {
	type wire ForkMetadata
	var next wire
	if err := modelStrictJSON(b, &next); err != nil {
		return err
	}
	value := ForkMetadata(next)
	if err := value.Validate(); err != nil {
		return err
	}
	*v = value
	return nil
}

func modelDecodeYAMLDocument(data []byte, value any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var n yaml.Node
	if err := decoder.Decode(&n); err != nil {
		return err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decisionview: multiple YAML documents")
		}
		return err
	}
	return modelUnmarshalYAML(&n, value)
}
func modelUnmarshalYAML(n *yaml.Node, value any) error {
	b, err := modelYAMLJSON(n)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}
func (v *ForkConfig) UnmarshalYAML(n *yaml.Node) error     { return modelUnmarshalYAML(n, v) }
func (v *SourceLocator) UnmarshalYAML(n *yaml.Node) error  { return modelUnmarshalYAML(n, v) }
func (v *Binding) UnmarshalYAML(n *yaml.Node) error        { return modelUnmarshalYAML(n, v) }
func (v *OverrideBasis) UnmarshalYAML(n *yaml.Node) error  { return modelUnmarshalYAML(n, v) }
func (v *AuthorityEvent) UnmarshalYAML(n *yaml.Node) error { return modelUnmarshalYAML(n, v) }
func (v *ForkMetadata) UnmarshalYAML(n *yaml.Node) error   { return modelUnmarshalYAML(n, v) }

// JSON RawMessage is not a YAML byte array. Convert via typed JSON values so
// canonical content remains a mapping when models are written as frontmatter.
func modelMarshalYAML(value any) (any, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var n yaml.Node
	if err := yaml.Unmarshal(b, &n); err != nil {
		return nil, err
	}
	return n.Content[0], nil
}
func (v OverrideBasis) MarshalYAML() (any, error)  { return modelMarshalYAML(v) }
func (v AuthorityEvent) MarshalYAML() (any, error) { return modelMarshalYAML(v) }
func (v Binding) MarshalYAML() (any, error)        { return modelMarshalYAML(v) }
func (v ForkMetadata) MarshalYAML() (any, error)   { return modelMarshalYAML(v) }

func modelStrictJSON(data []byte, out any) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("decisionview: JSON must be UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := modelJSONValue(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decisionview: trailing JSON value")
		}
		return err
	}
	if err := modelJSONKeys(data, reflect.TypeOf(out), "$", 0); err != nil {
		return err
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return fmt.Errorf("decisionview: decode: %w", err)
	}
	return nil
}
func modelJSONValue(d *json.Decoder, depth int) error {
	if depth > 256 {
		return fmt.Errorf("decisionview: JSON nesting exceeds 256")
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	switch v := t.(type) {
	case json.Number:
		if !integerJSONRe.MatchString(string(v)) {
			return fmt.Errorf("decisionview: unsupported non-integer number %q", v)
		}
	case json.Delim:
		if v != '{' && v != '[' {
			return fmt.Errorf("decisionview: unexpected delimiter %q", v)
		}
		seen := map[string]bool{}
		for d.More() {
			if v == '{' {
				key, err := d.Token()
				if err != nil {
					return err
				}
				s, ok := key.(string)
				if !ok {
					return fmt.Errorf("decisionview: object key is not a string")
				}
				if seen[s] {
					return fmt.Errorf("decisionview: duplicate JSON key %q", s)
				}
				seen[s] = true
			}
			if err := modelJSONValue(d, depth+1); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	return nil
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func modelJSONKeys(data []byte, typ reflect.Type, at string, depth int) error {
	if depth > 256 {
		return fmt.Errorf("decisionview: model nesting exceeds 256")
	}
	for typ.Kind() == reflect.Pointer {
		if depth > 0 && bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
			return nil
		}
		typ = typ.Elem()
	}
	if typ == rawMessageType {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var members map[string]json.RawMessage
		if err := json.Unmarshal(data, &members); err != nil {
			return fmt.Errorf("decisionview: %s must be an object: %w", at, err)
		}
		if members == nil {
			return fmt.Errorf("decisionview: %s must be an object", at)
		}
		allowed := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag != "" && tag != "-" {
				allowed[tag] = f.Type
			}
		}
		keys := make([]string, 0, len(members))
		for k := range members {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			raw := members[k]
			t, ok := allowed[k]
			if !ok {
				return fmt.Errorf("decisionview: unknown field %s.%s", at, k)
			}
			if err := modelJSONKeys(raw, t, at+"."+k, depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		var elements []json.RawMessage
		if err := json.Unmarshal(data, &elements); err != nil {
			return err
		}
		for i, raw := range elements {
			if err := modelJSONKeys(raw, typ.Elem(), fmt.Sprintf("%s[%d]", at, i), depth+1); err != nil {
				return err
			}
		}
	case reflect.String:
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if _, ok := value.(string); !ok {
			return fmt.Errorf("decisionview: %s must be a string", at)
		}
	}
	return nil
}

func modelYAMLJSON(root *yaml.Node) ([]byte, error) {
	active := map[*yaml.Node]bool{}
	var convert func(*yaml.Node, int) (any, error)
	convert = func(n *yaml.Node, depth int) (any, error) {
		if n == nil || depth > 256 || active[n] {
			return nil, fmt.Errorf("decisionview: cyclic or excessively nested YAML")
		}
		active[n] = true
		defer delete(active, n)
		switch n.Kind {
		case yaml.DocumentNode:
			if len(n.Content) != 1 {
				return nil, fmt.Errorf("decisionview: one YAML document required")
			}
			return convert(n.Content[0], depth+1)
		case yaml.AliasNode:
			return convert(n.Alias, depth+1)
		case yaml.MappingNode:
			if n.Tag != "!!map" || len(n.Content)%2 != 0 {
				return nil, fmt.Errorf("decisionview: invalid or unsupported YAML mapping")
			}
			m := map[string]any{}
			for i := 0; i < len(n.Content); i += 2 {
				key := n.Content[i]
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
					return nil, fmt.Errorf("decisionview: YAML key must be a string")
				}
				if _, exists := m[key.Value]; exists {
					return nil, fmt.Errorf("decisionview: duplicate YAML key %q", key.Value)
				}
				v, err := convert(n.Content[i+1], depth+1)
				if err != nil {
					return nil, err
				}
				m[key.Value] = v
			}
			return m, nil
		case yaml.SequenceNode:
			if n.Tag != "!!seq" {
				return nil, fmt.Errorf("decisionview: unsupported YAML sequence tag %q", n.Tag)
			}
			a := make([]any, 0, len(n.Content))
			for _, child := range n.Content {
				v, err := convert(child, depth+1)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			return a, nil
		case yaml.ScalarNode:
			switch n.Tag {
			case "!!str", "!!timestamp":
				return n.Value, nil
			case "!!null":
				return nil, nil
			case "!!bool":
				var v bool
				if err := n.Decode(&v); err != nil {
					return nil, err
				}
				return v, nil
			case "!!int":
				var v any
				if err := n.Decode(&v); err != nil {
					return nil, err
				}
				switch v.(type) {
				case int, int64, uint64:
					return v, nil
				}
				return nil, fmt.Errorf("decisionview: unsupported YAML integer")
			}
		}
		return nil, fmt.Errorf("decisionview: unsupported YAML node/tag %q", n.Tag)
	}
	v, err := convert(root, 0)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
