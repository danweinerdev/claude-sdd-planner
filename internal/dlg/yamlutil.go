package dlg

import (
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

func utf8Valid(b []byte) bool { return utf8.Valid(b) }

// splitKeepends splits on "\n" keeping each terminator, matching Python's
// splitlines(keepends=True) for the LF-only content this validator accepts.
func splitKeepends(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.SplitAfter(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// nodeToMap converts a mapping node to a generic map, or reports false when
// the node is not a mapping.
func nodeToMap(n *yaml.Node) (map[string]any, bool) {
	if n.Kind != yaml.MappingNode {
		return nil, false
	}
	m, ok := nodeValue(n).(map[string]any)
	return m, ok
}

// nodeValue walks a node tree into strings, []any, and map[string]any.
//
// Scalars keep their source text rather than being decoded into Go types,
// because this validator compares dates and ids as the strings the author
// wrote. The one exception is booleans, which several checks test as bools.
func nodeValue(n *yaml.Node) any {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil
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
		return nil
	default:
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
			return nil
		}
		return n.Value
	}
}

// isNonemptyString ports nonempty_string().
func isNonemptyString(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) != ""
}

// isMissing ports Python's `field not in meta or meta[field] in (None, "")`.
func isMissing(m map[string]any, field string) bool {
	v, present := m[field]
	return !present || v == nil || v == ""
}
