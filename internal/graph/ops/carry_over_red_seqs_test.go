package ops

import (
	"reflect"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

// TestCarryOverRedSeqs: a test unchanged in (id, file, satisfies) between the
// preimage and the revised gate keeps its recorded red; a test whose file
// changed loses it; a new test id has none.
func TestCarryOverRedSeqs(t *testing.T) {
	oldTests := []model.Test{
		{ID: "test_unchanged", File: "t.ext", Satisfies: []string{"AC-01"}},
		{ID: "test_moved", File: "old.ext", Satisfies: []string{"AC-01"}},
	}
	newTests := []model.Test{
		{ID: "test_unchanged", File: "t.ext", Satisfies: []string{"AC-01"}},
		{ID: "test_moved", File: "new.ext", Satisfies: []string{"AC-01"}},
		{ID: "test_new", File: "new2.ext", Satisfies: []string{"AC-01"}},
	}
	oldRed := map[string]int{"test_unchanged": 3, "test_moved": 3}

	got := carryOverRedSeqs(oldTests, newTests, oldRed)
	want := map[string]int{"test_unchanged": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("carryOverRedSeqs = %v, want %v", got, want)
	}
}

func TestCarryOverRedSeqsNoOldRed(t *testing.T) {
	oldTests := []model.Test{{ID: "test_a", File: "t.ext"}}
	newTests := []model.Test{{ID: "test_a", File: "t.ext"}}
	if got := carryOverRedSeqs(oldTests, newTests, nil); got != nil {
		t.Fatalf("carryOverRedSeqs with no old red = %v, want nil", got)
	}
}
