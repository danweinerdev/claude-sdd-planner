package reportevidence

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func TestExemplarStrictlyDecodes(t *testing.T) {
	raw, err := ExemplarJSON()
	if err != nil {
		t.Fatal(err)
	}
	var got Metadata
	if err := strictJSON(raw, &got); err != nil {
		t.Fatalf("exemplar must strictly decode: %v", err)
	}
	if !reflect.DeepEqual(got.Before, got.After) {
		t.Fatal("exemplar before and after contexts differ")
	}
	if got.Execution.ExitCode == nil || *got.Execution.ExitCode < 0 {
		t.Fatal("exemplar lacks an explicit nonnegative exit_code")
	}
	profile := &model.ReportProfile{
		Format: "go-test-json-v1", Runner: "repository-unit-tests",
		EnvironmentKeys: []string{"BUILD_CONTEXT"}, TestSupportInputs: []string{}, TestSupportArtifacts: []string{},
	}
	if _, err := Validate(nil, raw, got.Before, profile, time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC)); err == nil || !strings.Contains(err.Error(), "Go report is empty") {
		t.Fatalf("exemplar did not pass metadata and context checks before native report parsing: %v", err)
	}
}

func TestSchemaPropertiesMatchGoTypes(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(SchemaJSON(), &schema); err != nil {
		t.Fatalf("metadata schema is invalid JSON: %v", err)
	}
	defs := schema["$defs"].(map[string]any)
	for _, tc := range []struct {
		name string
		typ  reflect.Type
		obj  map[string]any
	}{
		{"metadata", reflect.TypeOf(Metadata{}), schema},
		{"context", reflect.TypeOf(Context{}), defs["context"].(map[string]any)},
		{"candidate", reflect.TypeOf(Candidate{}), defs["candidate"].(map[string]any)},
		{"runner", reflect.TypeOf(Runner{}), defs["runner"].(map[string]any)},
		{"execution", reflect.TypeOf(Execution{}), defs["execution"].(map[string]any)},
	} {
		wantProps, wantRequired := jsonFields(tc.typ)
		gotProps := sortedMapKeys(tc.obj["properties"].(map[string]any))
		gotRequired := sortedStrings(anyStrings(tc.obj["required"]))
		if strings.Join(gotProps, ",") != strings.Join(wantProps, ",") {
			t.Errorf("%s schema properties %v != Go json fields %v", tc.name, gotProps, wantProps)
		}
		if strings.Join(gotRequired, ",") != strings.Join(wantRequired, ",") {
			t.Errorf("%s schema required %v != non-omitempty Go fields %v", tc.name, gotRequired, wantRequired)
		}
		if extra, ok := tc.obj["additionalProperties"].(bool); !ok || extra {
			t.Errorf("%s must set additionalProperties false", tc.name)
		}
	}
}

func TestSchemaPinsRequiredRefusals(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(SchemaJSON(), &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	execution := defs["execution"].(map[string]any)
	context := defs["context"].(map[string]any)
	if !contains(anyStrings(execution["required"]), "exit_code") {
		t.Error("schema does not require execution.exit_code")
	}
	exit := execution["properties"].(map[string]any)["exit_code"].(map[string]any)
	if exit["type"] != "integer" || exit["minimum"].(float64) != 0 {
		t.Errorf("exit_code schema = %v, want integer minimum 0", exit)
	}
	if !contains(anyStrings(context["required"]), "declared_hazards") {
		t.Error("schema does not require context.declared_hazards")
	}
	if schema["additionalProperties"] != false {
		t.Error("metadata schema permits unknown fields")
	}
	digest := schema["properties"].(map[string]any)["report_digest"].(map[string]any)
	if digest["pattern"] != `^sha256:[0-9a-f]{64}$` {
		t.Errorf("report_digest pattern = %v", digest["pattern"])
	}
}

func TestShippedContractMatchesCanonical(t *testing.T) {
	path := filepath.Join("..", "..", "shared", "test-evidence-contract.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), ContractMarkdown()) {
		t.Fatalf("%s differs from canonical internal/reportevidence/CONTRACT.md; refresh the shared copy from the embedded canonical file", path)
	}
}

func jsonFields(typ reflect.Type) (properties, required []string) {
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name, option, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		properties = append(properties, name)
		if option != "omitempty" {
			required = append(required, name)
		}
	}
	return sortedStrings(properties), sortedStrings(required)
}

func sortedMapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return sortedStrings(out)
}

func sortedStrings(v []string) []string { sort.Strings(v); return v }

func anyStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func contains(v []string, want string) bool {
	for _, got := range v {
		if got == want {
			return true
		}
	}
	return false
}
