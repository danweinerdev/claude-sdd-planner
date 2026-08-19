package rules

import (
	"reflect"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/schema"
)

// The validator's statusValues map and the embedded schema registry are two
// independent lists of artifact types. When they drift, a type the tool
// itself scaffolds (`sdd template note`, `apply --create`) is refused by
// `sdd validate` with SDD011 "Unknown type" (B-4) — or the validator accepts
// a type no other command can serve. This pins the agreement in both
// directions: every schema-served type is registered in statusValues with
// exactly the schema's own status enum, and every statusValues entry is
// backed by a schema.
func TestStatusValuesAgreeWithSchemaRegistry(t *testing.T) {
	schemaTypes := map[string]bool{}
	for _, typ := range schema.Types() {
		schemaTypes[typ] = true
		s, err := schema.Load(typ)
		if err != nil {
			t.Fatalf("schema.Load(%q): %v", typ, err)
		}
		f := s.Field("status")
		if f == nil || len(f.Enum) == 0 {
			t.Fatalf("schema %q has no status enum; the agreement test assumes every artifact schema declares one", typ)
		}
		got, ok := statusValues[typ]
		if !ok {
			t.Errorf("schema type %q is missing from statusValues; sdd validate raises SDD011 on artifacts the tool itself scaffolds", typ)
			continue
		}
		if !reflect.DeepEqual(got, f.Enum) {
			t.Errorf("statusValues[%q] = %v, want the schema's enum %v", typ, got, f.Enum)
		}
	}
	for typ := range statusValues {
		if !schemaTypes[typ] {
			t.Errorf("statusValues registers %q but no schema serves it; the validator accepts a type no other command can create or edit", typ)
		}
	}
}
