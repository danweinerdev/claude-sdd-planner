package compile

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
)

// TestRenderFrontmatterRefusesUnmodelableValue pins the guard that a first
// implementation of this renderer lacked.
//
// `created: {{DATE}}` parses as a YAML flow mapping whose key is itself a
// mapping. Decoding into a Node SUCCEEDS, so an encode-only check passes and
// the value re-emits as `{? {DATE: ”} : ”}` — silently corrupting a template
// placeholder in a real file. Only re-parsing the rendered bytes into a strict
// map, and requiring the model to match, catches it.
func TestRenderFrontmatterRefusesUnmodelableValue(t *testing.T) {
	doc := artifact.Parse(`---
title: "T"
type: research
status: draft
created: {{DATE}}
updated: {{DATE}}
tags: []
related: []
---

# T
`)
	if _, ok := renderFrontmatter(doc, "2026-08-10"); ok {
		t.Error("rendered frontmatter containing {{DATE}}; it must refuse rather than mangle the placeholder")
	}
}

// TestRenderFrontmatterQuotedPlaceholderIsFine is the counterpart: quoting the
// placeholder makes it an ordinary string, which round-trips.
func TestRenderFrontmatterQuotedPlaceholderIsFine(t *testing.T) {
	doc := artifact.Parse(`---
title: "T"
type: research
status: draft
created: "{{DATE}}"
updated: "{{DATE}}"
tags: []
related: []
---

# T
`)
	lines, ok := renderFrontmatter(doc, "2026-08-10")
	if !ok {
		t.Fatal("a quoted placeholder must render")
	}
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, `created: "{{DATE}}"`) {
		t.Errorf("quoted placeholder not preserved:\n%s", out)
	}
	if !strings.Contains(out, "updated: 2026-08-10") {
		t.Errorf("updated not restamped:\n%s", out)
	}
}

// TestRenderFrontmatterRestampIsUnquoted guards a subtler defect: forcing the
// restamped value to !!str quotes only `updated`, so every write differs from
// the document it replaced even when nothing else changed.
func TestRenderFrontmatterRestampIsUnquoted(t *testing.T) {
	doc := artifact.Parse(`---
title: "T"
type: plan
status: draft
created: 2026-08-01
updated: 2026-08-01
tags: []
related: []
---

# T
`)
	lines, ok := renderFrontmatter(doc, "2026-08-04")
	if !ok {
		t.Fatal("render failed")
	}
	out := strings.Join(lines, "\n")
	if strings.Contains(out, `updated: "2026-08-04"`) {
		t.Errorf("restamped date was quoted, unlike `created`:\n%s", out)
	}
	if !strings.Contains(out, "updated: 2026-08-04") {
		t.Errorf("updated not restamped:\n%s", out)
	}
}
