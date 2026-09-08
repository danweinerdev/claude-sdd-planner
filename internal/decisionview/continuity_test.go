package decisionview_test

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func continuityCollection() *decisionview.Collection {
	return &decisionview.Collection{ID: selectionLedger, Locator: decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/decisions.md", Archives: []string{"Decisions/archive-*.md"}}, Entries: map[string]map[string]any{
		"D-0001": collectionEntry("D-0001", "accepted", "original rule"),
		"D-0002": collectionEntry("D-0002", "rejected", "rejected alternative"),
	}}
}
func TestForkContinuityIndependentMatrix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*decisionview.Collection)
		invalid bool
	}{
		{"unchanged", func(c *decisionview.Collection) {}, false},
		{"unrelated-append", func(c *decisionview.Collection) {
			c.Entries["D-0003"] = collectionEntry("D-0003", "accepted", "new rule")
		}, false},
		{"legal-supersession", func(c *decisionview.Collection) {
			c.Entries["D-0001"]["status"] = "superseded"
			c.Entries["D-0001"]["superseded_by"] = "D-0003"
			c.Entries["D-0003"] = collectionEntry("D-0003", "accepted", "successor")
			c.Entries["D-0003"]["supersedes"] = "D-0001"
		}, false},
		{"accepted-statement-edit", func(c *decisionview.Collection) { c.Entries["D-0001"]["statement"] = "silent rewrite" }, true},
		{"rejected-edit", func(c *decisionview.Collection) { c.Entries["D-0002"]["rationale"] = "different history" }, true},
		{"removed-id", func(c *decisionview.Collection) { delete(c.Entries, "D-0001") }, true},
		{"identity-substitution", func(c *decisionview.Collection) { c.ID = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff" }, true},
		{"locator-change", func(c *decisionview.Collection) { c.Locator.Path = "other/decisions.md" }, true},
		{"status-rollback", func(c *decisionview.Collection) { c.Entries["D-0001"]["status"] = "proposed" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := continuityCollection()
			binding, err := decisionview.BindCollection("binding-1", selectionOwner, original)
			if err != nil {
				t.Fatal(err)
			}
			current := continuityCollection()
			tc.mutate(current)
			diags, err := decisionview.CheckContinuity(binding, current)
			if err != nil {
				t.Fatal(err)
			}
			if (len(diags) > 0) != tc.invalid {
				t.Fatalf("independent expected invalid=%v, got %+v", tc.invalid, diags)
			}
		})
	}
	c := continuityCollection()
	basis, err := decisionview.CreateBasis("binding-1", c, "ledger:"+selectionLedger+":D-0001", []string{"binding-1"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := decisionview.CompareBasis(basis, c, "binding-1", []string{"binding-1"})
	if err != nil || !got.Current {
		t.Fatalf("unchanged basis: %+v %v", got, err)
	}
	c.Entries["D-0003"] = collectionEntry("D-0003", "accepted", "unrelated")
	got, err = decisionview.CompareBasis(basis, c, "binding-1", []string{"binding-1"})
	if err != nil || !got.Current {
		t.Fatal("whole-collection changes invalidated target basis")
	}
	c.Entries["D-0001"]["confirmation"] = "new verification requirement"
	got, err = decisionview.CompareBasis(basis, c, "binding-1", []string{"binding-1"})
	if err != nil || got.Current || !reflect.DeepEqual(got.ChangedFields, []string{"confirmation"}) {
		t.Fatalf("relevant change missed: %+v %v", got, err)
	}
	got, err = decisionview.CompareBasis(basis, continuityCollection(), "binding-2", []string{"binding-2"})
	if err != nil || got.Current || !got.BindingChanged {
		t.Fatalf("rebind kept authority: %+v %v", got, err)
	}
}

func TestForkContinuitySeedReplay(t *testing.T) {
	replay := func(seed int64) []byte {
		r := rand.New(rand.NewSource(seed))
		c := continuityCollection()
		b, err := decisionview.BindCollection("binding-1", selectionOwner, c)
		if err != nil {
			t.Fatal(err)
		}
		var trace []any
		for i := 0; i < 30; i++ {
			next := continuityCollection()
			if r.Intn(2) == 0 {
				next.Entries["D-0001"]["confirmation"] = "changed"
			}
			d, err := decisionview.CheckContinuity(b, next)
			if err != nil {
				t.Fatal(err)
			}
			trace = append(trace, d)
		}
		out, err := json.Marshal(trace)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if string(replay(23)) != string(replay(23)) {
		t.Fatal("same seed produced different continuity trace")
	}
	if string(replay(23)) == string(replay(24)) {
		t.Fatal("source differences were ignored")
	}
}
