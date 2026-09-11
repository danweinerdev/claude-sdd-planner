package decisionview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrYAMLAliasBudget is returned before aliases can amplify a small YAML
// document into unbounded decoder work.
var ErrYAMLAliasBudget = errors.New("decisionview: YAML alias expansion exceeds work budget")

// This budget counts only node evaluations performed through aliases. Direct
// nodes remain governed by the collection's 64 MiB input limit, so a legitimate
// maximum-size ledger is not rejected merely for being large. One hundred
// thousand expanded evaluations is ample for ordinary reuse while bounding a
// compact exponential alias graph well below memory-exhaustion territory.
const maxYAMLAliasNodeVisits = 100_000

type yamlDecodeGuard struct {
	active    map[*yaml.Node]bool
	aliasWork int
}

func newYAMLDecodeGuard() *yamlDecodeGuard {
	return &yamlDecodeGuard{active: map[*yaml.Node]bool{}}
}

func (g *yamlDecodeGuard) enter(n *yaml.Node, depth int, throughAlias bool) (func(), error) {
	if n == nil || depth > 256 || g.active[n] {
		return nil, fmt.Errorf("decisionview: cyclic or excessively nested YAML")
	}
	if throughAlias {
		g.aliasWork++
		if g.aliasWork > maxYAMLAliasNodeVisits {
			return nil, ErrYAMLAliasBudget
		}
	}
	g.active[n] = true
	return func() { delete(g.active, n) }, nil
}

// ConfigDeclaresDecisionLog recognizes only a semantic top-level decisionLog
// key. Token decoding handles escaped key bytes. For malformed JSON, a key
// already observed at top level remains authoritative; incomplete top-level
// syntax is treated conservatively, while strings buried in an incomplete
// nested value never manufacture repository authority.
func ConfigDeclaresDecisionLog(raw []byte) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err == nil {
		for key := range fields {
			if strings.EqualFold(key, "decisionLog") {
				return true
			}
		}
		return false
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return true
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return true
		}
		name, ok := key.(string)
		if !ok {
			return true
		}
		if strings.EqualFold(name, "decisionLog") {
			return true
		}
		if err := skipConfigJSONValue(decoder, 0); err != nil {
			return true
		}
	}
	if _, err := decoder.Token(); err != nil {
		return true
	}
	if _, err := decoder.Token(); err != io.EOF {
		return true
	}
	return false
}

func skipConfigJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 256 {
		return fmt.Errorf("decisionview: JSON nesting exceeds 256")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '{' && delim != '[' {
		return fmt.Errorf("decisionview: unexpected JSON delimiter %q", delim)
	}
	for decoder.More() {
		if delim == '{' {
			if _, err := decoder.Token(); err != nil {
				return err
			}
		}
		if err := skipConfigJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
