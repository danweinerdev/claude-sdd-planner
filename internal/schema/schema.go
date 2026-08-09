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
}

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
	for _, f := range s.Frontmatter {
		if fields[f.Key] {
			return fmt.Errorf("duplicate frontmatter key %q", f.Key)
		}
		fields[f.Key] = true
		if f.Ownership() != Author && f.Ownership() != Tool {
			return fmt.Errorf("frontmatter %q has invalid ownership %q", f.Key, f.OwnerRaw)
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
