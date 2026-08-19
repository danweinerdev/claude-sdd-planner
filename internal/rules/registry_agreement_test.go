package rules

import (
	"reflect"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/schema"
)

// The validator's statusValues map and the embedded schema registry are two
// independent lists of artifact types. When they drift, a type the tool
// itself scaffolds (`sdd template note`, `apply --create`) is refused by
// `sdd validate` with SDD011 "Unknown type" (B-4). This pins the agreement:
// every schema-served type must be registered in statusValues with exactly
// the schema's own status enum. statusValues may carry validator-only legacy
// extras (`diagram`) — the agreement is one-way by design.
func TestStatusValuesAgreeWithSchemaRegistry(t *testing.T) {
	for _, typ := range schema.Types() {
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
}
