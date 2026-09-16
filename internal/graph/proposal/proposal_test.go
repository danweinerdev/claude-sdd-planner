package proposal

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

// TestExemplarDecodesCleanly is the round-trip gate: the skeleton an
// authoring session starts from must be accepted by the strict proposal
// decoder with zero findings. If the model changes shape, this fails before
// any user sees a template that no longer validates.
func TestExemplarDecodesCleanly(t *testing.T) {
	raw, err := ExemplarJSON()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if _, err := model.DecodeProposal(raw); err != nil {
		t.Fatalf("the exemplar must decode with zero findings; got:\n%v", err)
	}
}

func TestExemplarIsDeterministic(t *testing.T) {
	a, err := ExemplarJSON()
	if err != nil {
		t.Fatal(err)
	}
	b, err := ExemplarJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two renders differ; the exemplar must be byte-deterministic")
	}
	if !bytes.HasSuffix(a, []byte("\n")) || bytes.Contains(a, []byte("\r")) {
		t.Fatal("exemplar must be LF-only with a trailing newline")
	}
}

// TestExemplarDemonstratesTheVocabulary pins the exemplar's pedagogical
// content: every gate type, all three hazards shapes, and the terminal full
// review gate covering every sink (the coverage-invariant backstop).
func TestExemplarDemonstratesTheVocabulary(t *testing.T) {
	p := Exemplar()

	gateTypes := map[string]bool{}
	var untriaged, filled, explicitEmpty bool
	for _, n := range p.Nodes {
		gateTypes[n.Gate.Type] = true
		switch {
		case n.Hazards == nil:
			untriaged = true
		case len(n.Hazards) > 0:
			filled = true
		default:
			explicitEmpty = true
		}
	}
	for _, gt := range []string{model.GateTests, model.GateCommand, model.GateReview} {
		if !gateTypes[gt] {
			t.Errorf("exemplar demonstrates no %q gate", gt)
		}
	}
	if !untriaged || !filled || !explicitEmpty {
		t.Errorf("exemplar must demonstrate all three hazards shapes (untriaged=%v filled=%v empty=%v)",
			untriaged, filled, explicitEmpty)
	}

	// Placeholder ids must be present so a filler knows what to replace.
	raw, _ := ExemplarJSON()
	for _, placeholder := range []string{"AC-NN", "FR-NN", "DD-N"} {
		if !bytes.Contains(raw, []byte(placeholder)) {
			t.Errorf("exemplar carries no %s placeholder", placeholder)
		}
	}

	// The terminal full review gate depends on every sink node.
	dependedOn := map[string]bool{}
	for _, n := range p.Nodes {
		for _, d := range n.Deps {
			dependedOn[d] = true
		}
	}
	var terminal *model.Node
	for i := range p.Nodes {
		n := &p.Nodes[i]
		if n.Gate.Type == model.GateReview && n.Gate.Lanes == nil {
			terminal = n
		}
	}
	if terminal == nil {
		t.Fatal("exemplar has no full review gate; the coverage-invariant backstop is missing")
	}
	deps := map[string]bool{}
	for _, d := range terminal.Deps {
		deps[d] = true
	}
	for _, n := range p.Nodes {
		if n.ID == terminal.ID {
			continue
		}
		if !dependedOn[n.ID] && !deps[n.ID] {
			t.Errorf("sink node %q is not covered by the terminal full review gate", n.ID)
		}
	}
}

// --- schema <-> model drift gate ---

// schemaDoc is the parsed embedded schema, decoded once per test that needs
// it. Failures here mean the schema document itself is malformed.
func schemaDoc(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(SchemaJSON(), &doc); err != nil {
		t.Fatalf("schema.json is not valid JSON: %v", err)
	}
	return doc
}

func propKeys(t *testing.T, obj map[string]any, where string) []string {
	t.Helper()
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no properties object", where)
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func definition(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	defs, ok := doc["definitions"].(map[string]any)
	if !ok {
		t.Fatal("schema has no definitions")
	}
	d, ok := defs[name].(map[string]any)
	if !ok {
		t.Fatalf("schema defines no %q", name)
	}
	return d
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// TestSchemaKeysMatchModel is the single-source gate: the schema's
// properties must equal the decoder's allowed-key sets exactly. A key added
// to either side alone fails here.
func TestSchemaKeysMatchModel(t *testing.T) {
	doc := schemaDoc(t)
	keySets := model.KeySets()

	cases := []struct {
		name string
		obj  map[string]any
	}{
		{"proposal", doc},
		{"node", definition(t, doc, "node")},
		{"gate", definition(t, doc, "gate")},
		{"test", definition(t, doc, "test")},
	}
	for _, c := range cases {
		want := sorted(keySets[c.name])
		got := propKeys(t, c.obj, c.name)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: schema properties %v != model keys %v", c.name, got, want)
		}
		if ap, ok := c.obj["additionalProperties"].(bool); !ok || ap {
			t.Errorf("%s: additionalProperties must be false — strict decoding is the contract", c.name)
		}
	}
}

func TestSchemaConstantsMatchModel(t *testing.T) {
	doc := schemaDoc(t)

	// version const == model.SchemaVersion
	version := doc["properties"].(map[string]any)["version"].(map[string]any)
	if c, ok := version["const"].(float64); !ok || int(c) != model.SchemaVersion {
		t.Errorf("schema version const %v != model.SchemaVersion %d", version["const"], model.SchemaVersion)
	}

	// gate.type enum == the model's gate-type constants, in order.
	gate := definition(t, doc, "gate")
	rawEnum := gate["properties"].(map[string]any)["type"].(map[string]any)["enum"].([]any)
	var gotEnum []string
	for _, v := range rawEnum {
		gotEnum = append(gotEnum, v.(string))
	}
	wantEnum := []string{model.GateTests, model.GateCommand, model.GateReview}
	if strings.Join(gotEnum, ",") != strings.Join(wantEnum, ",") {
		t.Errorf("gate.type enum %v != model constants %v", gotEnum, wantEnum)
	}

	// hazards oneOf carries the untriaged sentinel; lanes oneOf carries "full".
	node := definition(t, doc, "node")
	hazardsOneOf, _ := json.Marshal(node["properties"].(map[string]any)["hazards"])
	if !strings.Contains(string(hazardsOneOf), `"`+model.UntriagedSentinel+`"`) {
		t.Errorf("hazards schema does not carry the %q sentinel: %s", model.UntriagedSentinel, hazardsOneOf)
	}
	lanes, _ := json.Marshal(gate["properties"].(map[string]any)["lanes"])
	if !strings.Contains(string(lanes), `"full"`) {
		t.Errorf(`lanes schema does not carry the "full" form: %s`, lanes)
	}

	// input.root enum == the model's input-root constants, in order.
	input := definition(t, doc, "input")
	rawRoots := input["properties"].(map[string]any)["root"].(map[string]any)["enum"].([]any)
	var gotRoots []string
	for _, v := range rawRoots {
		gotRoots = append(gotRoots, v.(string))
	}
	wantRoots := []string{model.InputRootRepository, model.InputRootPlanning}
	if strings.Join(gotRoots, ",") != strings.Join(wantRoots, ",") {
		t.Errorf("input.root enum %v != model constants %v", gotRoots, wantRoots)
	}
}

func TestSchemaRequiredListsMatchDecoder(t *testing.T) {
	doc := schemaDoc(t)
	wantRequired := map[string][]string{
		"proposal": {"nodes", "version"},
		"node":     {"contract", "gate", "hazards", "id"},
		"gate":     {"type"},
		"test":     {"file", "id"},
	}
	objs := map[string]map[string]any{
		"proposal": doc,
		"node":     definition(t, doc, "node"),
		"gate":     definition(t, doc, "gate"),
		"test":     definition(t, doc, "test"),
	}
	for name, obj := range objs {
		raw, ok := obj["required"].([]any)
		if !ok {
			t.Errorf("%s: schema declares no required list", name)
			continue
		}
		var got []string
		for _, v := range raw {
			got = append(got, v.(string))
		}
		if strings.Join(sorted(got), ",") != strings.Join(wantRequired[name], ",") {
			t.Errorf("%s: schema required %v != decoder-required %v", name, sorted(got), wantRequired[name])
		}
	}
}

func TestSchemaGatePredicatesForbidFieldsOnWrongGateTypes(t *testing.T) {
	doc := schemaDoc(t)
	gate := definition(t, doc, "gate")
	raw, _ := json.Marshal(gate["allOf"])
	s := string(raw)
	for _, requiredPredicate := range []string{`"required":["type"]`, `"required":["evidence"]`} {
		if !strings.Contains(s, requiredPredicate) {
			t.Errorf("conditional lacks presence predicate %s: %s", requiredPredicate, s)
		}
	}
	for _, field := range []string{"tests", "evidence", "execution", "command", "lanes"} {
		if !strings.Contains(s, `"required":["`+field+`"]`) {
			t.Errorf("schema conditions never require/forbid %q by presence: %s", field, s)
		}
	}
	if !strings.Contains(s, `"not":{"required":["execution"]}`) {
		t.Errorf("authoring schema does not forbid retired execution profiles: %s", s)
	}
}

func TestSchemaExecutionBoundsAndCollectionsMatchModel(t *testing.T) {
	doc := schemaDoc(t)
	execution := definition(t, doc, "execution")
	props := execution["properties"].(map[string]any)
	timeout := props["timeout_seconds"].(map[string]any)
	if got := int64(timeout["maximum"].(float64)); got != int64(model.MaxExecutionTimeoutSeconds) {
		t.Fatalf("schema timeout maximum=%d, model=%d", got, model.MaxExecutionTimeoutSeconds)
	}
	for _, field := range []string{"environment_keys", "test_support_inputs", "test_support_artifacts"} {
		p := props[field].(map[string]any)
		if unique, _ := p["uniqueItems"].(bool); !unique {
			t.Errorf("%s does not reject duplicates", field)
		}
	}
	envItems := props["environment_keys"].(map[string]any)["items"].(map[string]any)
	if envItems["pattern"] != `^[A-Za-z_][A-Za-z0-9_]*$` {
		t.Fatalf("environment key pattern=%v", envItems["pattern"])
	}
}
