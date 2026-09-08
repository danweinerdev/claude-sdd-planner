package decisionview_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func citationView(t *testing.T) *decisionview.ResolvedView {
	t.Helper()
	parent, local := resolvedFixture(t, selectionLedger, resolvedLocal)
	v, err := decisionview.Compose(local.ID, "fork", map[decisionview.CollectionID]*decisionview.Collection{parent.ID: parent, local.ID: local})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestForkCitationScopeIndependentSets(t *testing.T) {
	v := citationView(t)
	q := "ledger:" + selectionLedger + ":D-0001"
	r, err := decisionview.LookupReference(v, q, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Original.ID) != q || r.Original.Original["statement"] != "original rule" || r.Effective == nil || string(r.Effective.ID) != "ledger:"+resolvedLocal+":D-0001" {
		t.Fatalf("historical identity was rewritten: %+v", r)
	}
	related := map[string][]string{"Specs/A": {"Designs/A"}, "Designs/A": {"Plans/A"}, "Plans/A": {"Research/Far"}}
	for _, tc := range []struct {
		left, right []string
		want        bool
	}{
		{nil, []string{"src"}, true}, {[]string{"src"}, []string{"src/a.go"}, true}, {[]string{"src/a"}, []string{"src/ab"}, false},
		{[]string{"Specs/A"}, []string{"Designs/A"}, true}, {[]string{"Specs/A"}, []string{"Plans/A"}, true}, {[]string{"Specs/A"}, []string{"Research/Far"}, false},
	} {
		if got := decisionview.ScopesOverlap(tc.left, tc.right, related); got != tc.want {
			t.Errorf("scope %v / %v: got %v want %v", tc.left, tc.right, got, tc.want)
		}
	}
}
func TestForkCitationHostileRoundTrip(t *testing.T) {
	v := citationView(t)
	for i := range v.Records {
		v.Records[i].Original["rationale"] = "yes null \"quoted\"\n雪"
	}
	r, err := decisionview.LookupReference(v, "ledger:"+resolvedLocal+":D-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var again decisionview.CitationResult
	if err := json.Unmarshal(raw, &again); err != nil {
		t.Fatal(err)
	}
	if again.Original.Original["rationale"] != "yes null \"quoted\"\n雪" {
		t.Fatal("hostile values lost in citation output")
	}
	for _, bad := range []string{"D-0001", "ledger:bad:D-0001", "ledger:" + selectionLedger + ":D-0001\n", "\"D-0001\""} {
		if _, err := decisionview.LookupReference(v, bad, nil); err == nil {
			t.Errorf("invalid or ambiguous reference accepted: %q", bad)
		}
	}
}
func TestForkLegacyContextAndExternalOwners(t *testing.T) {
	v := citationView(t)
	legacy := &decisionview.LegacyContext{Root: decisionview.SourceRootPlanning, Path: "Specs/A/README.md", Namespace: selectionLedger, LocalIDs: []string{"D-0001"}}
	r, err := decisionview.LookupReference(v, "D-0001", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Original.ID) != "ledger:"+selectionLedger+":D-0001" {
		t.Fatal("local numbering captured inherited citation")
	}
	if err := decisionview.ValidateReferenceChange([]string{"D-0001"}, []string{"D-0001"}, legacy); err != nil {
		t.Fatal(err)
	}
	if err := decisionview.ValidateReferenceChange([]string{"D-0001"}, []string{"D-0001", "D-0001"}, legacy); err == nil {
		t.Fatal("new bare occurrence was treated as historical")
	}
	if err := decisionview.ValidateReferenceChange(nil, []string{"ledger:" + resolvedLocal + ":D-0001"}, nil); err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	planning := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repository, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(planning, "Specs", "A"), 0o755); err != nil {
		t.Fatal(err)
	}
	view := &decisionview.ResolvedView{OwnerID: selectionOwner, Resolution: decisionview.ResolutionComplete, Records: []decisionview.ResolvedDecision{{ID: "ledger:" + resolvedLocal + ":D-0001", Applicability: "binding", Original: map[string]any{"scope": []any{"src", "Specs/A"}}}}}
	ctx := decisionview.ScopeContext{Roots: decisionview.Roots{Repository: repository, Planning: planning}, Owner: selectionOwner, ArtifactOwners: map[string]decisionview.OwnerID{"Specs/A": selectionOwner}}
	if got := decisionview.CheckScopes(view, ctx); len(got) != 0 {
		t.Fatalf("existing owned scope: %+v", got)
	}
	ctx.ArtifactOwners["Specs/A"] = "22222222-3333-4444-5555-666666666666"
	if got := decisionview.CheckScopes(view, ctx); len(got) == 0 {
		t.Fatal("foreign external owner was accepted")
	}
	if !reflect.DeepEqual(view.Records[0].Original["scope"], []any{"src", "Specs/A"}) {
		t.Fatal("source scope was rewritten")
	}
	view.Records[0].Original["scope"] = []any{"missing"}
	if got := decisionview.CheckScopes(view, ctx); len(got) == 0 {
		t.Fatal("missing scope silently ignored")
	}
}
