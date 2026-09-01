package claims

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func testPlanDir(t *testing.T, nodes ...model.Node) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Plans", "SamplePlan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Init(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Update(gstore.PathFor(dir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes, nodes...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func n(id string, deps []string, estimate int, artifacts ...string) model.Node {
	return model.Node{
		ID: id, Contract: "c", Deps: deps,
		Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{},
		Estimate: estimate, Artifacts: artifacts,
	}
}

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

// bigProvider lifts the capacity screen so tests can isolate other rules.
type bigProvider struct{ StubProvider }

func (bigProvider) Capacity() int { return 100 }

func TestClaimPicksTheHeaviestFrontierNode(t *testing.T) {
	// light (est 1, nothing downstream) vs heavy (est 1 but a 5-weight
	// dependant hangs off it): heavy keeps the wall-clock floor down.
	dir := testPlanDir(t,
		n("light", nil, 1),
		n("heavy", nil, 1),
		n("downstream", []string{"heavy"}, 5),
	)
	got, err := Claim(dir, Options{By: "a1"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got.Node.ID != "heavy" {
		t.Fatalf("claimed %q, want the critical-path head", got.Node.ID)
	}
	if got.LeaseExpires == "" || got.Node.Claim == nil {
		t.Fatalf("claim record incomplete: %+v", got)
	}
	// The record landed in the store, not just the return value.
	g, _ := gstore.Load(gstore.PathFor(dir))
	if g.NodeByID("heavy").Claim == nil || g.NodeByID("heavy").Claim.By != "a1" {
		t.Fatal("the claim must be committed to the graph")
	}
}

func TestClaimScreensClaimsArtifactsAndCapacity(t *testing.T) {
	dir := testPlanDir(t,
		n("a", nil, 3, "src/shared.ext"),
		n("b", nil, 2, "src/shared.ext"), // overlaps a's artifact
		n("c", nil, 1, "src/other.ext"),
	)
	first, err := Claim(dir, Options{By: "a1", Provider: bigProvider{}})
	if err != nil || first.Node.ID != "a" {
		t.Fatalf("first claim: %v %+v", err, first)
	}
	// b overlaps the claimed artifact; c is the only claimable node left.
	second, err := Claim(dir, Options{By: "a2", Provider: bigProvider{}})
	if err != nil || second.Node.ID != "c" {
		t.Fatalf("second claim must skip the artifact overlap: %v %+v", err, second)
	}
	// Nothing claimable now — and the explanation says why.
	_, err = Claim(dir, Options{By: "a3", Provider: bigProvider{}})
	if err == nil || !strings.Contains(err.Error(), "actively claimed") {
		t.Fatalf("exhausted frontier must explain: %v", err)
	}

	// Capacity screen: the stub provider's capacity of 1 refuses a second
	// claim even where nodes remain claimable.
	dir2 := testPlanDir(t, n("x", nil, 1), n("y", nil, 1))
	if _, err := Claim(dir2, Options{By: "a1"}); err != nil {
		t.Fatal(err)
	}
	_, err = Claim(dir2, Options{By: "a2"})
	if err == nil || !strings.Contains(err.Error(), "capacity 1 is fully claimed") {
		t.Fatalf("capacity refusal must name itself: %v", err)
	}
}

func TestExpiryAtReadReturnsTheNodeAndKeepsTheWorkspace(t *testing.T) {
	dir := testPlanDir(t, n("a", nil, 1))
	t0 := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	first, err := Claim(dir, Options{By: "dead-agent", TTL: 10 * time.Minute, Now: fixedNow(t0)})
	if err != nil {
		t.Fatal(err)
	}
	_ = first

	// Before expiry: refused as claimed.
	if _, err := Claim(dir, Options{By: "a2", Now: fixedNow(t0.Add(5 * time.Minute))}); err == nil {
		t.Fatal("an unexpired claim must block a takeover")
	}
	// After expiry: reclaimed, reported, re-claimable.
	got, err := Claim(dir, Options{By: "a2", Now: fixedNow(t0.Add(11 * time.Minute))})
	if err != nil {
		t.Fatalf("expired claim must return the node to the frontier: %v", err)
	}
	if len(got.ReclaimedExpired) != 1 || got.ReclaimedExpired[0] != "a" {
		t.Fatalf("the takeover must report what it reclaimed: %+v", got.ReclaimedExpired)
	}
	if got.Node.Claim.By != "a2" {
		t.Fatalf("new claimant must own the claim: %+v", got.Node.Claim)
	}
}

func TestRenewIsHolderOnly(t *testing.T) {
	dir := testPlanDir(t, n("a", nil, 1))
	t0 := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	if _, err := Claim(dir, Options{By: "a1", TTL: 10 * time.Minute, Now: fixedNow(t0)}); err != nil {
		t.Fatal(err)
	}
	renewed, err := Renew(dir, "a", "a1", 10*time.Minute, fixedNow(t0.Add(8*time.Minute)))
	if err != nil {
		t.Fatalf("holder renewal: %v", err)
	}
	if want := t0.Add(18 * time.Minute).UTC().Format(time.RFC3339); renewed != want {
		t.Fatalf("lease = %s, want %s", renewed, want)
	}
	if _, err := Renew(dir, "a", "impostor", 10*time.Minute, fixedNow(t0)); err == nil {
		t.Fatal("a lease is renewed only by its holder")
	}
	if _, err := Renew(dir, "unclaimed", "a1", 0, nil); err == nil {
		t.Fatal("renewing a nonexistent node must refuse")
	}
}

func TestReleaseIsHolderOnlyUnlessForced(t *testing.T) {
	dir := testPlanDir(t, n("a", nil, 1))
	if _, err := Claim(dir, Options{By: "a1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Release(dir, "a", "impostor", false); err == nil {
		t.Fatal("release is holder-only without --force")
	}
	if _, err := Release(dir, "a", "impostor", true); err != nil {
		t.Fatalf("forced release must succeed: %v", err)
	}
	g, _ := gstore.Load(gstore.PathFor(dir))
	if g.NodeByID("a").Claim != nil {
		t.Fatal("release must clear the claim")
	}
	if _, err := Release(dir, "a", "a1", false); err == nil {
		t.Fatal("releasing an unclaimed node must refuse")
	}
}

// failingProvider proves allocation failure refuses the claim and records
// nothing.
type failingProvider struct{ StubProvider }

func (failingProvider) Allocate(string) (string, error) {
	return "", errors.New("no worktree for you")
}

func TestAllocationFailureLeavesTheNodeUnclaimed(t *testing.T) {
	dir := testPlanDir(t, n("a", nil, 1))
	_, err := Claim(dir, Options{By: "a1", Provider: failingProvider{}})
	if err == nil || !strings.Contains(err.Error(), "node left unclaimed") {
		t.Fatalf("allocation failure must refuse the claim: %v", err)
	}
	g, _ := gstore.Load(gstore.PathFor(dir))
	if g.NodeByID("a").Claim != nil {
		t.Fatal("no claim record may name a workspace that does not exist")
	}
}

func TestEmptyFrontierExplainsStateCounts(t *testing.T) {
	blocked := n("b", []string{"a"}, 1)
	ready := n("a", nil, 1)
	ready.Verification = &model.Verification{Result: model.ResultFail, Seq: 1, Isolation: model.IsolationClean}
	// a is RED (workable, on frontier)... make it truly empty: claim a first.
	dir := testPlanDir(t, ready, blocked)
	if _, err := Claim(dir, Options{By: "a1"}); err != nil {
		t.Fatal(err)
	}
	_, err := Claim(dir, Options{By: "a2", Provider: bigProvider{}})
	if err == nil {
		t.Fatal("expected an empty-frontier refusal")
	}
	msg := err.Error()
	for _, want := range []string{"RED", "BLOCKED", "actively claimed"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("explanation must carry state counts (%q missing): %v", want, msg)
		}
	}
}
