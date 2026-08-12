// Package schema declares SDD artifact types as data (FR-14).
//
// One schema per artifact type is the single source for the parser's rule set,
// apply's allocation and refusal behavior, and (later) generated templates and
// help text (FR-15). Spike scope: the `spec` type only.
package schema

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed *.json
var embedded embed.FS

// Ownership says who may write a frontmatter field. A tool-owned field present
// in a payload is an error naming the owning subcommand (FR-18).
type Ownership string

const (
	Author Ownership = "author"
	Tool   Ownership = "tool"
)

// Field is one frontmatter key.
type Field struct {
	Key      string   `json:"key"`
	OwnerRaw string   `json:"ownership"`
	Required bool     `json:"required"`
	Fixed    string   `json:"fixed,omitempty"`
	Enum     []string `json:"enum,omitempty"`
	// Default is the value the upgrade path inserts when this required field is
	// absent. An empty Default means the field cannot be filled in mechanically
	// and the upgrade must report it for a human to supply.
	Default string `json:"default,omitempty"`
	// Aliases are legacy spellings of this key that the upgrade path renames to
	// Key. Only ever list spellings whose MEANING is identical: renaming is
	// mechanical, so an alias whose semantics differ (a relationship pointing the
	// other way, for instance) must be reported for a human instead.
	Aliases []string `json:"aliases,omitempty"`
	// Entry declares the shape of one element when this field holds a block
	// sequence (decisions[], phases[], tasks[], findings[]). Nil means the field
	// is a scalar or an unmodeled list.
	//
	// Declaring the shape here makes it one fact rather than three: the
	// validator's per-entry rules are generated from it, and the writers check
	// against it instead of pattern-matching raw lines.
	Entry *Entry `json:"entry,omitempty"`
	// Doc is a one-paragraph human explanation of the field, for schemas whose
	// meaning is not obvious from the key. It is documentation only — nothing
	// validates against it — but it lives here so the explanation sits with the
	// declaration instead of drifting in a separate document.
	Doc string `json:"doc,omitempty"`
}

// Entry is the declared shape of one block-sequence element.
type Entry struct {
	Fields []EntryField `json:"fields"`
}

// Field returns the named entry field, or nil.
func (e *Entry) Field(key string) *EntryField {
	for i := range e.Fields {
		if e.Fields[i].Key == key {
			return &e.Fields[i]
		}
	}
	return nil
}

// EntryField is one key within a block-sequence element.
type EntryField struct {
	Key string `json:"key"`
	// Required fields must be present and nonempty.
	Required bool `json:"required"`
	// Enum, when set, is the closed set of permitted values.
	Enum []string `json:"enum,omitempty"`
	// Pattern, when set, is a regular expression the value must fully match.
	Pattern string `json:"pattern,omitempty"`
	// RequiredWhen makes this field required only when a sibling field holds a
	// given value — e.g. a decision's `question` is required only when `kind`
	// is "answered-question".
	RequiredWhen *Condition `json:"requiredWhen,omitempty"`
	// ForbiddenWhen refuses a value combination — e.g. `decided_by: agent` is
	// permitted only while `status` is "proposed".
	ForbiddenWhen *Condition `json:"forbiddenWhen,omitempty"`

	// compiled is Pattern, anchored, built once at Load so a malformed pattern
	// is a schema error rather than a panic at first use.
	compiled *regexp.Regexp
}

// Condition names a sibling field and the values that trigger a rule. When
// Not is true the condition holds while the sibling is NOT one of Values.
type Condition struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
	Not    bool     `json:"not,omitempty"`
	// Value, when set, additionally scopes the condition to this field holding
	// that value.
	Value string `json:"value,omitempty"`
}

// Holds reports whether the condition is satisfied by an entry.
func (c *Condition) Holds(get func(string) string) bool {
	actual := get(c.Field)
	in := false
	for _, v := range c.Values {
		if actual == v {
			in = true
			break
		}
	}
	if c.Not {
		return !in
	}
	return in
}

// CompiledPattern returns the field's Pattern as an anchored regexp, or nil
// when the field declares none.
func (f *EntryField) CompiledPattern() *regexp.Regexp { return f.compiled }

func (f Field) Ownership() Ownership { return Ownership(f.OwnerRaw) }

// Heading is one declared section slot. Only headings at a declared depth that
// match a declared slot are slots; deeper headings are body content.
type Heading struct {
	Text        string `json:"text"`
	Depth       int    `json:"depth"`
	Required    bool   `json:"required"`
	IDNamespace string `json:"idNamespace,omitempty"`
	FreeProse   bool   `json:"freeProse,omitempty"`
	// DefaultBody is the canonical content the upgrade path inserts when this
	// required section is absent. Some sections have an exact conventional form
	// the validator matches on (a completion-evidence section must read
	// "Pending — not complete."), so the correct stub is schema data rather
	// than something the migration invents.
	DefaultBody string `json:"defaultBody,omitempty"`
}

// Title is the heading text without its leading hashes.
func (h Heading) Title() string {
	return strings.TrimSpace(strings.TrimLeft(h.Text, "#"))
}

// Namespace is an identifier namespace the tool allocates from (FR-20).
type Namespace struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
	Width  int    `json:"width"`
}

// Format renders n as this namespace's identifier, e.g. 7 -> "FR-07".
func (ns Namespace) Format(n int) string {
	return fmt.Sprintf("%s-%0*d", ns.Prefix, ns.Width, n)
}

// Schema is one artifact type's complete structural declaration.
type Schema struct {
	Type string `json:"type"`
	// AdditionalSections is "allowed" when undeclared sections at a declared
	// depth are retained in place and reported, or "refused" when they are a
	// hard refusal. Specs legitimately grow sections, so `spec` allows them;
	// making this a declared property keeps it a deliberate decision rather
	// than an accident of the matcher.
	AdditionalSections string `json:"additionalSections"`
	// DocNote explains what this artifact type is for, surfaced by `schema show`
	// and `doctor`. Free-form-body types especially need it: a schema that
	// declares no sections should say why.
	DocNote string `json:"docNote,omitempty"`
	// FrontmatterMode is "managed" when the tool regenerates the frontmatter
	// block from declared flat fields, or "preserve" when it copies the block
	// verbatim and only restamps `updated`. Types carrying nested structure —
	// a plan's phases[], a phase's tasks[], a review's findings[] — must use
	// "preserve": regenerating them from a flat key/value model would silently
	// destroy the nesting. Empty defaults to "managed".
	FrontmatterMode string      `json:"frontmatterMode,omitempty"`
	Frontmatter     []Field     `json:"frontmatter"`
	Headings        []Heading   `json:"headings"`
	Namespaces      []Namespace `json:"namespaces"`
}

// Load returns the embedded schema for an artifact type.
func Load(artifactType string) (*Schema, error) {
	b, err := embedded.ReadFile(artifactType + ".json")
	if err != nil {
		return nil, fmt.Errorf("no embedded schema for artifact type %q", artifactType)
	}
	var s Schema
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("schema %s: %w", artifactType, err)
	}
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("schema %s: %w", artifactType, err)
	}
	return &s, nil
}

// Types lists every embedded artifact type.
func Types() []string {
	ents, _ := embedded.ReadDir(".")
	var out []string
	for _, e := range ents {
		out = append(out, strings.TrimSuffix(e.Name(), ".json"))
	}
	return out
}

func (s *Schema) validate() error {
	if s.Type == "" {
		return fmt.Errorf("missing type")
	}
	switch s.AdditionalSections {
	case "allowed", "refused":
	default:
		return fmt.Errorf("additionalSections must be \"allowed\" or \"refused\", got %q", s.AdditionalSections)
	}
	if s.FrontmatterMode == "" {
		s.FrontmatterMode = "managed"
	}
	switch s.FrontmatterMode {
	case "managed", "preserve":
	default:
		return fmt.Errorf("frontmatterMode must be \"managed\" or \"preserve\", got %q", s.FrontmatterMode)
	}
	// Zero headings is legitimate: the decision ledger is frontmatter plus
	// prose, with no H2 sections at all. A managed-frontmatter type still needs
	// declared fields, which the loop below checks.
	if len(s.Headings) == 0 && len(s.Frontmatter) == 0 {
		return fmt.Errorf("schema declares neither headings nor frontmatter fields")
	}
	seen := map[string]bool{}
	for _, h := range s.Headings {
		if seen[h.Text] {
			return fmt.Errorf("duplicate heading %q", h.Text)
		}
		seen[h.Text] = true
		if h.Depth < 1 || h.Depth > 6 {
			return fmt.Errorf("heading %q has invalid depth %d", h.Text, h.Depth)
		}
		if want := strings.Repeat("#", h.Depth) + " "; !strings.HasPrefix(h.Text, want) {
			return fmt.Errorf("heading %q does not match its declared depth %d", h.Text, h.Depth)
		}
		if h.IDNamespace != "" && s.Namespace(h.IDNamespace) == nil {
			return fmt.Errorf("heading %q references undeclared namespace %q", h.Text, h.IDNamespace)
		}
		if h.IDNamespace != "" && h.FreeProse {
			return fmt.Errorf("heading %q is both free-prose and identifier-bearing", h.Text)
		}
	}
	fields := map[string]bool{}
	for i := range s.Frontmatter {
		f := &s.Frontmatter[i]
		if fields[f.Key] {
			return fmt.Errorf("duplicate frontmatter key %q", f.Key)
		}
		fields[f.Key] = true
		if f.Ownership() != Author && f.Ownership() != Tool {
			return fmt.Errorf("frontmatter %q has invalid ownership %q", f.Key, f.OwnerRaw)
		}
		if err := f.Entry.validate(f.Key); err != nil {
			return err
		}
	}
	return nil
}

// validate checks a declared entry shape and compiles its patterns, so a
// malformed schema fails at Load rather than panicking mid-validation.
func (e *Entry) validate(fieldKey string) error {
	if e == nil {
		return nil
	}
	if len(e.Fields) == 0 {
		return fmt.Errorf("frontmatter %q declares an entry with no fields", fieldKey)
	}
	seen := map[string]bool{}
	for i := range e.Fields {
		ef := &e.Fields[i]
		if ef.Key == "" {
			return fmt.Errorf("frontmatter %q entry has a field with no key", fieldKey)
		}
		if seen[ef.Key] {
			return fmt.Errorf("frontmatter %q entry has duplicate field %q", fieldKey, ef.Key)
		}
		seen[ef.Key] = true
		if ef.Pattern != "" {
			re, err := regexp.Compile(`^(?:` + ef.Pattern + `)$`)
			if err != nil {
				return fmt.Errorf("frontmatter %q entry field %q has invalid pattern: %w", fieldKey, ef.Key, err)
			}
			ef.compiled = re
		}
	}
	// Conditions must name a field the entry actually declares, so a typo is a
	// schema error rather than a rule that silently never fires.
	for _, ef := range e.Fields {
		for _, c := range []*Condition{ef.RequiredWhen, ef.ForbiddenWhen} {
			if c == nil {
				continue
			}
			if c.Field == "" || !seen[c.Field] {
				return fmt.Errorf("frontmatter %q entry field %q references undeclared field %q", fieldKey, ef.Key, c.Field)
			}
			if len(c.Values) == 0 {
				return fmt.Errorf("frontmatter %q entry field %q declares a condition with no values", fieldKey, ef.Key)
			}
		}
	}
	return nil
}

// Preserves reports whether the frontmatter block is copied verbatim.
func (s *Schema) Preserves() bool { return s.FrontmatterMode == "preserve" }

// Namespace returns the named namespace, or nil.
func (s *Schema) Namespace(name string) *Namespace {
	for i := range s.Namespaces {
		if s.Namespaces[i].Name == name {
			return &s.Namespaces[i]
		}
	}
	return nil
}

// Field returns the named frontmatter field, or nil.
func (s *Schema) Field(key string) *Field {
	for i := range s.Frontmatter {
		if s.Frontmatter[i].Key == key {
			return &s.Frontmatter[i]
		}
	}
	return nil
}

// FieldByAlias returns the field a legacy key spelling maps to, or nil.
func (s *Schema) FieldByAlias(key string) *Field {
	for i := range s.Frontmatter {
		for _, a := range s.Frontmatter[i].Aliases {
			if a == key {
				return &s.Frontmatter[i]
			}
		}
	}
	return nil
}

// Heading returns the declared slot with this exact text, or nil.
func (s *Schema) Heading(text string) *Heading {
	for i := range s.Headings {
		if s.Headings[i].Text == text {
			return &s.Headings[i]
		}
	}
	return nil
}

// SlotDepths reports every depth at which this schema declares a slot. A
// heading deeper than all of these is body content, not a slot.
func (s *Schema) SlotDepths() map[int]bool {
	d := map[int]bool{}
	for _, h := range s.Headings {
		d[h.Depth] = true
	}
	return d
}

// MaxSlotDepth is the deepest declared slot depth.
func (s *Schema) MaxSlotDepth() int {
	max := 0
	for _, h := range s.Headings {
		if h.Depth > max {
			max = h.Depth
		}
	}
	return max
}
