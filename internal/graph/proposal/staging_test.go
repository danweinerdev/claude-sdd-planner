package proposal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func stagedPlanDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Plans", "SamplePlan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Init(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

const validFragment = `{
  "version": 1,
  "nodes": [
    {
      "id": "node-a",
      "contract": "does a",
      "gate": {"type": "tests", "tests": [{"id": "test_a", "file": "tests/a.ext"}]},
      "hazards": []
    }
  ]
}
`

func fragmentWith(id string) string {
	return strings.ReplaceAll(validFragment, "node-a", id)
}

// stagedFragments counts the .json fragments in the staging dir, ignoring
// the advisory-lock sidecars WriteAtomic leaves behind by design (they live
// in the gitignored workspace and are never reaped).
func stagedFragments(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(FragmentsDir(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			n++
		}
	}
	return n
}

func TestStageParksAValidFragment(t *testing.T) {
	dir := stagedPlanDir(t)
	path, err := Stage(dir, []byte(validFragment))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if filepath.Dir(path) != FragmentsDir(dir) {
		t.Fatalf("fragment staged outside the fragments dir: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != validFragment {
		t.Fatalf("staged bytes must be the payload verbatim: %v\n%s", err, raw)
	}
}

func TestStageRefusesWithoutStaging(t *testing.T) {
	dir := stagedPlanDir(t)

	// Invalid payload: strict-decode findings, nothing staged.
	_, err := Stage(dir, []byte(`{"version": 1, "nodes": [{"id": "x", "contrct": "typo", "gate": {"type": "tests"}, "hazards": []}]}`))
	if err == nil || !strings.Contains(err.Error(), `did you mean "contract"?`) {
		t.Fatalf("stage must refuse with the strict-decode findings: %v", err)
	}
	if !strings.Contains(err.Error(), "nothing staged") {
		t.Fatalf("refusal must state the atomicity: %v", err)
	}
	if n := stagedFragments(t, dir); n != 0 {
		t.Fatalf("a refused payload must stage nothing; found %d fragments", n)
	}

	// Tool-owned field: refused at the same gate.
	_, err = Stage(dir, []byte(`{"version": 1, "nodes": [{"id": "x", "contract": "c", "gate": {"type": "tests"}, "hazards": [], "claim": {"by": "me", "lease_expires": "2026-01-01T00:00:00Z"}}]}`))
	if err == nil || !strings.Contains(err.Error(), "tool-owned field") {
		t.Fatalf("stage must refuse tool-owned fields: %v", err)
	}
}

func TestStageRefusesRedefiningAMasterNode(t *testing.T) {
	dir := stagedPlanDir(t)
	graphPath := gstore.PathFor(dir)
	if _, err := gstore.Update(graphPath, func(g *model.Graph) error {
		g.Nodes = append(g.Nodes, model.Node{
			ID: "node-a", Contract: "already landed",
			Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := Stage(dir, []byte(validFragment))
	if err == nil || !strings.Contains(err.Error(), `node "node-a" already exists`) {
		t.Fatalf("stage must refuse redefining a master-graph node: %v", err)
	}
}

func TestStageRequiresAnInitializedGraph(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Plans", "NoGraph")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Stage(dir, []byte(validFragment))
	if err == nil || !strings.Contains(err.Error(), "sdd graph init") {
		t.Fatalf("stage without a graph must point at init: %v", err)
	}
}

func TestAssembleMergesDisjointFragmentsInStagingOrder(t *testing.T) {
	dir := stagedPlanDir(t)
	for _, id := range []string{"node-a", "node-b", "node-c"} {
		if _, err := Stage(dir, []byte(fragmentWith(id))); err != nil {
			t.Fatalf("stage %s: %v", id, err)
		}
	}
	assembled, merged, err := Assemble(dir)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(merged.Nodes) != 3 {
		t.Fatalf("merged %d nodes, want 3", len(merged.Nodes))
	}
	// Staging order is merge order: fragment ids are time-ordered.
	got := []string{merged.Nodes[0].ID, merged.Nodes[1].ID, merged.Nodes[2].ID}
	if strings.Join(got, ",") != "node-a,node-b,node-c" {
		t.Fatalf("merge order must be staging order, got %v", got)
	}
	// The merged set decodes as a proposal and the fragments are consumed.
	raw, err := os.ReadFile(assembled)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.DecodeProposal(raw); err != nil {
		t.Fatalf("assembled proposal must decode strictly: %v", err)
	}
	if n := stagedFragments(t, dir); n != 0 {
		t.Fatalf("fragments must be consumed on success; %d remain", n)
	}
}

func TestAssembleRefusesCollisionsNamingBothFragments(t *testing.T) {
	dir := stagedPlanDir(t)
	first, err := Stage(dir, []byte(fragmentWith("dup-node")))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Stage(dir, []byte(fragmentWith("dup-node")))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Assemble(dir)
	if err == nil {
		t.Fatal("assemble must refuse a node-id collision")
	}
	msg := err.Error()
	if !strings.Contains(msg, filepath.Base(first)) || !strings.Contains(msg, filepath.Base(second)) {
		t.Fatalf("collision must name both fragments: %v", msg)
	}
	// Refusal leaves every fragment untouched and writes no merged set.
	if n := stagedFragments(t, dir); n != 2 {
		t.Fatalf("refusal must leave fragments untouched; found %d", n)
	}
	if _, err := os.Stat(AssembledPath(dir)); !os.IsNotExist(err) {
		t.Fatal("refusal must not write the merged proposal")
	}
}

func TestAssembleWithNothingStagedPointsAtPropose(t *testing.T) {
	dir := stagedPlanDir(t)
	_, _, err := Assemble(dir)
	if err == nil || !strings.Contains(err.Error(), "sdd graph propose") {
		t.Fatalf("empty staging area must point at propose: %v", err)
	}
}

func TestFragmentIDsAreTimeOrdered(t *testing.T) {
	a, b := newFragmentID(), newFragmentID()
	if !(a < b) && a[:13] != b[:13] {
		t.Fatalf("fragment ids must be time-ordered: %s then %s", a, b)
	}
	if a[14] != '7' {
		t.Fatalf("fragment id must be UUIDv7, got %s", a)
	}
}
