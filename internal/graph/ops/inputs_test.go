package ops

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func inputFixture(t *testing.T) (string, string, model.Input) {
	t.Helper()
	r, d := fixtureRoot(t)
	p := filepath.Join(r, "docs", "context.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("# Context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return r, d, model.Input{Root: model.InputRootRepository, Path: "docs/context.md"}
}
func TestSetInputsReplacesInputs(t *testing.T) {
	r, d, in := inputFixture(t)
	if _, err := SetInputs(r, r, "SamplePlan", "helper", []model.Input{in}, false); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(gstore.PathFor(d))
	raw, _ := g.Encode()
	if len(g.NodeByID("helper").Inputs) != 1 || bytes.Contains(raw, []byte("input_hashes")) {
		t.Fatalf("graph=%s", raw)
	}
}
func TestSetInputsDryRunWritesNothing(t *testing.T) {
	r, d, in := inputFixture(t)
	p := gstore.PathFor(d)
	before, _ := os.ReadFile(p)
	if _, err := SetInputs(r, r, "SamplePlan", "helper", []model.Input{in}, true); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("dry run wrote")
	}
}
func TestSetInputsRefusesEligibility(t *testing.T) {
	r, d, _ := inputFixture(t)
	_, _ = gstore.Update(gstore.PathFor(d), func(g *model.Graph) error {
		g.NodeByID("helper").Claim = &model.Claim{By: "h", LeaseExpires: "2099-01-01T00:00:00Z"}
		return nil
	})
	if _, err := SetInputs(r, r, "SamplePlan", "helper", nil, false); err == nil {
		t.Fatal("claimed node edited")
	}
}
func TestSetInputsRefusesUnresolvedInput(t *testing.T) {
	r, _, _ := inputFixture(t)
	if _, err := SetInputs(r, r, "SamplePlan", "helper", []model.Input{{Root: model.InputRootRepository, Path: "missing"}}, false); err == nil {
		t.Fatal("missing input accepted")
	}
}

func TestSetInputsRefusesCompetingClaimOnCASRetry(t *testing.T) {
	r, d, in := inputFixture(t)
	p := gstore.PathFor(d)
	_, err := setInputsWith(r, r, "SamplePlan", "helper", []model.Input{in}, false,
		contendWith(t, p, func(g *model.Graph) error {
			g.NodeByID("helper").Claim = &model.Claim{By: "racer", LeaseExpires: "2099-01-01T00:00:00Z"}
			return nil
		}))
	if err == nil || !strings.Contains(err.Error(), "claimed by") {
		t.Fatalf("the retry must refuse the competing claim: %v", err)
	}
	g, loadErr := gstore.Load(p)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	n := g.NodeByID("helper")
	if n.Claim == nil || n.Claim.By != "racer" || len(n.Inputs) != 0 {
		t.Fatalf("competing write was not preserved atomically: %+v", n)
	}
}
func TestSplitAnchorsChildrenInputs(t *testing.T) {
	_, _, in := inputFixture(t)
	g, p := splitCandidate()
	p.Nodes[0].Inputs = []model.Input{in}
	out, _, err := applySplit(g, "big", p)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.NodeByID("a").Inputs) != 1 {
		t.Fatal("child declaration lost")
	}
}
func TestSetInputsDroppingAnInputOnVerifiedNodeAdvancesRevision(t *testing.T) {
	r, d, in := inputFixture(t)
	p := gstore.PathFor(d)
	_, _ = gstore.Update(p, func(g *model.Graph) error {
		n := g.NodeByID("helper")
		n.Inputs = []model.Input{in}
		n.Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}
		n.RedSeqs = map[string]int{"test_helper": 1}
		return nil
	})
	if _, err := SetInputs(r, r, "SamplePlan", "helper", nil, false); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(p)
	n := g.NodeByID("helper")
	if len(n.Inputs) != 0 || n.EffectiveContractRev() != 2 || n.RedSeqs["test_helper"] != 1 {
		t.Fatalf("node=%+v", n)
	}
}

func TestSetInputsRefusesRedObservationsOnUnverifiedNode(t *testing.T) {
	r, d, in := inputFixture(t)
	p := gstore.PathFor(d)
	if _, err := gstore.Update(p, func(g *model.Graph) error {
		g.NodeByID("helper").RedSeqs = map[string]int{"test_helper": 1}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := SetInputs(r, r, "SamplePlan", "helper", []model.Input{in}, false); err == nil {
		t.Fatal("unverified node carrying red observations was edited")
	}
	g, err := gstore.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.NodeByID("helper").Inputs) != 0 || g.NodeByID("helper").RedSeqs["test_helper"] != 1 {
		t.Fatalf("refusal changed node: %+v", g.NodeByID("helper"))
	}
}
