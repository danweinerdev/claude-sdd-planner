package decisionview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestForkBoundedYAMLAliasExpansion(t *testing.T) {
	aliasYAML := boundedAliasExpansion(8, 5)

	t.Run("typed model conversion has a work budget", func(t *testing.T) {
		original := append([]byte(nil), aliasYAML...)
		var doc yaml.Node
		if err := yaml.Unmarshal(aliasYAML, &doc); err != nil {
			t.Fatalf("small alias fixture is invalid YAML: %v", err)
		}
		_, err := modelYAMLJSON(&doc)
		requireYAMLBudgetError(t, err)
		if !bytes.Equal(aliasYAML, original) {
			t.Fatal("typed model conversion changed source bytes")
		}
	})

	t.Run("collection frontmatter has a work budget", func(t *testing.T) {
		raw := append([]byte("---\n"), aliasYAML...)
		raw = append(raw, []byte("---\nbody\n")...)
		original := append([]byte(nil), raw...)
		_, err := collectionFrontmatter(raw)
		requireYAMLBudgetError(t, err)
		if !bytes.Equal(raw, original) {
			t.Fatal("collection decoder changed source bytes")
		}
	})

	t.Run("ordinary aliases remain supported", func(t *testing.T) {
		modelSource := []byte("version: 1\nmode: &mode fork\npath: *mode\nledgerId: 01234567-89ab-cdef-0123-456789abcdef\n")
		originalModel := append([]byte(nil), modelSource...)
		config, err := DecodeForkConfigYAML(modelSource)
		if err != nil {
			t.Fatalf("ordinary typed alias was rejected: %v", err)
		}
		if config.Mode != "fork" || config.Path != "fork" {
			t.Fatalf("ordinary typed alias decoded incorrectly: %#v", config)
		}
		if !bytes.Equal(modelSource, originalModel) {
			t.Fatal("typed alias decode changed source bytes")
		}

		collectionSource := []byte("---\nvalue: &value ordinary\ncopy: *value\n---\nbody\n")
		originalCollection := append([]byte(nil), collectionSource...)
		nodes, err := collectionFrontmatter(collectionSource)
		if err != nil {
			t.Fatalf("ordinary collection alias was rejected: %v", err)
		}
		data, err := modelYAMLJSON(nodes["copy"])
		if err != nil {
			t.Fatalf("ordinary collection alias could not be converted: %v", err)
		}
		var copied string
		if err := json.Unmarshal(data, &copied); err != nil || copied != "ordinary" {
			t.Fatalf("ordinary collection alias decoded as %q (%v)", copied, err)
		}
		if !bytes.Equal(collectionSource, originalCollection) {
			t.Fatal("collection alias decode changed source bytes")
		}
	})
}

func boundedAliasExpansion(levels, width int) []byte {
	var out strings.Builder
	out.WriteString("seed: &a0 [value]\n")
	for level := 1; level <= levels; level++ {
		fmt.Fprintf(&out, "level%d: &a%d [", level, level)
		for item := 0; item < width; item++ {
			if item > 0 {
				out.WriteString(", ")
			}
			fmt.Fprintf(&out, "*a%d", level-1)
		}
		out.WriteString("]\n")
	}
	fmt.Fprintf(&out, "expanded: *a%d\n", levels)
	return []byte(out.String())
}

func requireYAMLBudgetError(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "budget") {
		t.Fatalf("alias expansion error = %v, want explicit work budget error", err)
	}
}
