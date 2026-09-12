package main

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// This file carries the two things cmd/sdd needs from frontmatter that a YAML
// decoder alone does not give it:
//
//   - fmSequence decodes a named block sequence into ordered string maps. All
//     reads go through this.
//   - fmBlockBounds reports the *line range* a block sequence occupies, which
//     a frontmatter writer needs to splice new entries into place while leaving
//     every other byte of the frontmatter untouched. That is line arithmetic
//     over raw text, not parsing — a decoder cannot answer it, because
//     decoding discards layout.

// fmItem is one entry of a block sequence, with values decoded to strings.
// Str and List keep the accessor shape the consuming logic already uses.
type fmItem map[string]any

// Str returns a scalar field as a string. Non-scalars yield "".
func (it fmItem) Str(key string) string {
	switch v := it[key].(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []any:
		return ""
	case map[string]any:
		return ""
	default:
		return ""
	}
}

// List returns a sequence field's elements. A scalar is returned as a
// single-element list, matching the previous behavior for authors who wrote a
// bare word where a list was expected.
func (it fmItem) List(key string) []string {
	switch v := it[key].(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			out = append(out, scalarString(e))
		}
		return out
	default:
		s := scalarString(v)
		if s == "" {
			return nil
		}
		return []string{s}
	}
}

func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

// fmSequence decodes the named top-level block sequence out of raw frontmatter
// lines. Entries that are not mappings are skipped, since every caller here
// wants keyed fields.
func fmSequence(fm []string, key string) []fmItem {
	var doc map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(fm, "\n")), &doc); err != nil {
		return nil
	}
	node, ok := doc[key]
	if !ok || node.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]fmItem, 0, len(node.Content))
	for _, entry := range node.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		m, ok := nodeValue(entry).(map[string]any)
		if !ok {
			continue
		}
		out = append(out, fmItem(m))
	}
	return out
}

// nodeValue converts a yaml.Node tree into strings, []any, and map[string]any.
//
// It walks nodes rather than calling Node.Decode into `any`, because YAML's
// implicit typing would resolve `date: 2024-01-01` to a time.Time and an id
// like `1.10` to a float — both of which these callers compare as the text the
// author wrote. Reading Value off the scalar node keeps that text intact.
func nodeValue(n *yaml.Node) any {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return ""
		}
		return nodeValue(n.Content[0])
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			out[n.Content[i].Value] = nodeValue(n.Content[i+1])
		}
		return out
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, nodeValue(c))
		}
		return out
	case yaml.AliasNode:
		if n.Alias != nil {
			return nodeValue(n.Alias)
		}
		return ""
	default: // scalar
		if n.Style == yaml.SingleQuotedStyle || n.Style == yaml.DoubleQuotedStyle {
			return n.Value
		}
		switch n.Tag {
		case "!!bool":
			var b bool
			if err := n.Decode(&b); err == nil {
				return b
			}
		case "!!null":
			return ""
		}
		return n.Value
	}
}

// fmMeta decodes raw frontmatter lines into the map[string]any shape the
// internal/rules checks consume, via nodeValue so scalars keep the text the
// author wrote (except genuine !!bool values, which rules compare as bool).
func fmMeta(fm []string) map[string]any {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(fm, "\n")), &node); err != nil {
		return nil
	}
	m, _ := nodeValue(&node).(map[string]any)
	return m
}
