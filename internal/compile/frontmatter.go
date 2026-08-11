package compile

import (
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"gopkg.in/yaml.v3"
)

// Frontmatter is written by marshaling a parsed YAML node tree rather than by
// copying the author's lines.
//
// The previous renderer emitted FrontmatterRaw verbatim and rewrote only
// `updated`. That kept bytes stable but meant the tool never actually
// understood what it wrote: a malformed sequence entry, a duplicated key, or a
// value of the wrong type passed straight through, because nothing between
// parse and write inspected them. Marshaling makes the written form a
// consequence of the parsed model, so anything the model cannot represent is
// refused before it lands.
//
// The cost is that a written artifact adopts yaml.v3's canonical form: key
// order within a mapping is preserved (the node tree keeps it), but quoting,
// indentation, and flow/block style are normalized. Measured against this
// repository's own artifacts that is no change at all — 0 of 15 reflow — and
// across the fixture corpus it is 63 of 284, almost entirely `followups: []`
// gaining or losing a line.

// renderFrontmatter marshals a document's frontmatter, restamping `updated`.
//
// It returns ok=false when the frontmatter cannot be modeled, which the caller
// turns into a refusal rather than writing a mangled document. That case is
// real: a template placeholder like `created: {{DATE}}` parses as a flow
// mapping with an unhashable key, and yaml.Unmarshal fails on it while
// silently discarding every other key. Writing that result would delete the
// author's frontmatter, so the write is refused instead.
func renderFrontmatter(doc *artifact.Doc, today string) ([]string, bool) {
	source := strings.Join(doc.FrontmatterRaw, "\n")

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(source), &root); err != nil {
		return nil, false
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, false
	}
	mapping := root.Content[0]

	// Restamp `updated` in place, so it keeps its position rather than moving
	// to the end the way a delete-and-append would.
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != "updated" {
			continue
		}
		// Leave Tag empty so the encoder re-resolves it: a bare YYYY-MM-DD
		// resolves to !!timestamp and emits unquoted, matching how `created`
		// is written. Forcing !!str would quote only the restamped field,
		// making every write differ from the document it replaced.
		value := mapping.Content[i+1]
		value.Kind = yaml.ScalarNode
		value.Tag = ""
		value.Style = 0
		value.Value = today
		value.Content = nil
		break
	}

	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(mapping); err != nil {
		return nil, false
	}
	if err := enc.Close(); err != nil {
		return nil, false
	}

	out := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(out) == 1 && out[0] == "" {
		return nil, false
	}

	// Round-trip check. Decoding into a Node succeeds for constructs that a
	// map decode rejects, so a successful Encode above is not proof the write
	// is faithful: `created: {{DATE}}` parses as a flow mapping with an
	// unhashable key and re-emits as `{? {DATE: ''} : ''}`, silently
	// corrupting the value. Re-parsing what we are about to write and
	// requiring it to model identically is what catches that.
	if !modelsIdentically(source, b.String(), today) {
		return nil, false
	}
	return out, true
}

// modelsIdentically reports whether rendered frontmatter decodes to the same
// model as its source, ignoring the restamped `updated`.
//
// Both sides are decoded into a generic map, which is the strict path: a
// construct that cannot be modeled fails here rather than surviving as a
// mangled node.
func modelsIdentically(source, rendered, today string) bool {
	var before, after map[string]any
	if err := yaml.Unmarshal([]byte(source), &before); err != nil {
		return false
	}
	if err := yaml.Unmarshal([]byte(rendered), &after); err != nil {
		return false
	}
	delete(before, "updated")
	delete(after, "updated")
	beforeBytes, err1 := yaml.Marshal(before)
	afterBytes, err2 := yaml.Marshal(after)
	return err1 == nil && err2 == nil && string(beforeBytes) == string(afterBytes)
}

// appendDefaults adds a required author field the upgrade path can fill in,
// as preserveFrontmatterUpgraded did. It operates on rendered lines because a
// default is declared as its YAML text.
func appendDefaults(lines []string, key, value string) []string {
	return append(lines, key+": "+value)
}
