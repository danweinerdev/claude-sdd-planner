package compile

// Frozen phase views retain their renderer-owned Overview sequence stamp when
// only the graph-global counter advances. The counter is useful current
// metadata for open views, but is not part of a frozen phase's own history.
// These regressions pin the contract around that normalization:
//   1. an unchanged render of a frozen phase is a byte-preserving no-op;
//   2. an unrelated phase's observation (which advances the global counter)
//      must not refuse or rewrite a frozen phase;
//   3. a pure global counter increment must not refuse or rewrite a frozen
//      phase;
//   4. a malformed Overview stamp must not be normalized as counter metadata,
//      and the normalization token must not authorize a reclose escape;
//   5. sequence-shaped text inside node content is genuine frozen history and
//      still refuses.
//
// The genuine "a real phase-1 contract edit still refuses" case is covered by
// TestFrozenViewLifecycle and deliberately not duplicated here. The baseline
// frozen phase is produced by the real renderer via evidencePlanFixture +
// renderViews — no hand-written `FROZEN VIEW` marker is forged anywhere in
// this file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
)

// frozenSequenceFixture builds the shape the existing
// TestFrozenViewLifecycle fixture uses — a phase with a work node and a full
// review node, both closed (closed map true), which the renderer therefore
// projects as a frozen view — under a real phase label ("01-core"), plus a
// second, still-open phase ("02-extra") carrying one unrelated node. The
// baseline render goes through renderViews, so phase 1's frozen marker comes
// from the renderer, never a foreign hand-written marker.
func frozenSequenceFixture(t *testing.T) (root, planDir string, g *model.Graph, st map[string]states.NodeState, closed map[string]bool) {
	t.Helper()
	root, planDir = evidencePlanFixture(t)

	pass := func(seq int) *model.Verification {
		return &model.Verification{Result: model.ResultPass, Seq: seq, Isolation: model.IsolationClean}
	}
	g = &model.Graph{Version: 1, SeqCounter: 2, Nodes: []model.Node{
		{ID: "work", Contract: "works", Phase: "01-core", Gate: model.Gate{Type: model.GateTests},
			Hazards: model.Hazards{}, Estimate: 1, Verification: pass(1)},
		{ID: "review", Contract: "reviewed", Phase: "01-core", Deps: []string{"work"},
			Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(2)},
		{ID: "extra", Contract: "unrelated later work", Phase: "02-extra",
			Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1},
	}}
	st = states.Derive(states.Inputs{Graph: g})
	closed = states.Closed(g, st)
	if !closed["work"] || !closed["review"] {
		t.Fatalf("fixture must derive phase 1 closed: states=%v closed=%v", st, closed)
	}

	if _, err := renderViews(root, "P", "", g, st, closed); err != nil {
		t.Fatalf("baseline close render: %v", err)
	}
	return root, planDir, g, st, closed
}

// TestFrozenViewUnchangedRenderPreservesFrozenBytes is the control: with the
// graph byte-for-byte unchanged, a re-render is a no-op and the frozen
// phase-1 doc is preserved exactly (the ordinary no-op path in planWrite).
func TestFrozenViewUnchangedRenderPreservesFrozenBytes(t *testing.T) {
	root, planDir, g, st, closed := frozenSequenceFixture(t)
	phase1 := filepath.Join(planDir, "01-core.md")

	frozen := rendererRead(t, phase1)
	if !strings.Contains(frozen, frozenViewMarker) {
		t.Fatalf("fixture phase 1 must render frozen:\n%s", frozen)
	}

	written, err := renderViews(root, "P", "", g, st, closed)
	if err != nil {
		t.Fatalf("an unchanged render must not refuse: %v", err)
	}
	if len(written) != 0 {
		t.Fatalf("an unchanged render must write nothing, wrote %v", written)
	}
	if got := rendererRead(t, phase1); got != frozen {
		t.Fatalf("an unchanged render must preserve the frozen bytes exactly")
	}
}

// TestFrozenViewUnrelatedPhaseObservationRenders: an observation recorded on
// a node in the unrelated, still-open phase 2 advances the GLOBAL seq
// counter. Phase 1's own projected history does not change at all — only the
// global `seq %d` Overview line moves. The render must succeed, phase 1's
// frozen bytes must survive untouched, and the unrelated observation must
// actually land in phase 2.
func TestFrozenViewUnrelatedPhaseObservationRenders(t *testing.T) {
	root, planDir, g, st, closed := frozenSequenceFixture(t)
	phase1 := filepath.Join(planDir, "01-core.md")
	phase2 := filepath.Join(planDir, "02-extra.md")

	frozen := rendererRead(t, phase1)
	if !strings.Contains(frozen, frozenViewMarker) {
		t.Fatalf("fixture phase 1 must render frozen:\n%s", frozen)
	}

	// The unrelated node observes: the global counter advances, and only
	// phase 2's own projected content changes.
	g.SeqCounter = 3
	g.Nodes[2].Verification = &model.Verification{Result: model.ResultPass, Seq: 3, Isolation: model.IsolationClean}
	st = states.Derive(states.Inputs{Graph: g})
	closed = states.Closed(g, st)

	_, err := renderViews(root, "P", "", g, st, closed)
	if err != nil {
		t.Fatalf("an unrelated phase-2 observation must not refuse the frozen phase-1 render: %v", err)
	}
	if got := rendererRead(t, phase1); got != frozen {
		t.Fatalf("phase 1's frozen bytes must be preserved across an unrelated phase-2 observation")
	}
	if got := rendererRead(t, phase2); !strings.Contains(got, "at seq 3") ||
		!strings.Contains(got, "(schema v1, seq 3).") {
		t.Fatalf("the unrelated phase-2 observation must be recorded:\n%s", got)
	}
}

// TestFrozenViewPureCounterIncrementRenders: the global counter advances
// while the graph content is otherwise completely unchanged. Nothing in
// phase 1's projected history moved, so the render must succeed and phase
// 1's frozen bytes must be preserved (the seq line is global metadata, not
// this phase's history).
func TestFrozenViewPureCounterIncrementRenders(t *testing.T) {
	root, planDir, g, st, closed := frozenSequenceFixture(t)
	phase1 := filepath.Join(planDir, "01-core.md")

	frozen := rendererRead(t, phase1)
	if !strings.Contains(frozen, frozenViewMarker) {
		t.Fatalf("fixture phase 1 must render frozen:\n%s", frozen)
	}

	g.SeqCounter++ // 2 -> 3; no node, contract, gate, or observation moves.

	if _, err := renderViews(root, "P", "", g, st, closed); err != nil {
		t.Fatalf("a pure global seq-counter increment must not refuse the frozen phase: %v", err)
	}
	if got := rendererRead(t, phase1); got != frozen {
		t.Fatalf("phase 1's frozen bytes must be preserved across a pure counter increment")
	}
}

// TestFrozenViewMalformedOverviewDoesNotHideChanges: normalization only
// excuses the renderer-owned stamp shape. A corrupt Overview stamp is a
// frozen-byte disagreement and must refuse, never be normalized away.
func TestFrozenViewMalformedOverviewDoesNotHideChanges(t *testing.T) {
	root, planDir, g, st, closed := frozenSequenceFixture(t)
	phase1 := filepath.Join(planDir, "01-core.md")
	frozen := rendererRead(t, phase1)
	malformed := strings.Replace(frozen, "(schema v1, seq 2).", "(schema v1, seq unknown).", 1)
	if malformed == frozen {
		t.Fatal("fixture did not contain the renderer-owned Overview stamp")
	}
	rendererWrite(t, phase1, malformed)
	g.SeqCounter++

	if _, err := renderViews(root, "P", "", g, st, closed); err == nil {
		t.Fatal("a malformed frozen Overview stamp must not be normalized as counter metadata")
	}
	if got := rendererRead(t, phase1); got != malformed {
		t.Fatal("a refused render must preserve the malformed frozen bytes")
	}
}

// TestFrozenViewNormalizationTokenDoesNotAuthorizeReclose: a frozen view
// hand-edited to carry the normalization token literally in its Overview
// stamp must not get the reclose escape granted on the token's strength.
func TestFrozenViewNormalizationTokenDoesNotAuthorizeReclose(t *testing.T) {
	root, planDir, g, _, _ := frozenSequenceFixture(t)
	phase1 := filepath.Join(planDir, "01-core.md")
	frozen := rendererRead(t, phase1)
	malformed := strings.Replace(frozen, "(schema v1, seq 2).", "(schema v1, seq {SEQ}).", 1)
	if malformed == frozen {
		t.Fatal("fixture did not contain the renderer-owned Overview stamp")
	}
	rendererWrite(t, phase1, malformed)

	// Legitimately reclose both work and its covering full review. Even though
	// the Observation lines may use the normal reclose escape, the malformed
	// existing Overview stamp must remain a genuine frozen-byte disagreement.
	g.SeqCounter = 4
	g.Nodes[0].Verification = &model.Verification{Result: model.ResultPass, Seq: 3, Isolation: model.IsolationClean}
	g.Nodes[1].Verification = &model.Verification{Result: model.ResultPass, Seq: 4, Isolation: model.IsolationClean}
	st := states.Derive(states.Inputs{Graph: g})
	closed := states.Closed(g, st)
	if !closed["work"] || !closed["review"] {
		t.Fatalf("reclose fixture must derive closed: states=%v closed=%v", st, closed)
	}

	if _, err := renderViews(root, "P", "", g, st, closed); err == nil {
		t.Fatal("a malformed {SEQ} Overview stamp must not authorize the reclose escape")
	}
	if got := rendererRead(t, phase1); got != malformed {
		t.Fatal("a refused malformed-stamp reclose must preserve the frozen bytes")
	}
}

// TestFrozenViewSequenceShapedNodeTextStillRefuses: normalization targets
// the Overview line only. Node text that merely looks like a sequence stamp
// is genuine frozen history and a change to it must refuse.
func TestFrozenViewSequenceShapedNodeTextStillRefuses(t *testing.T) {
	root, planDir, g, _, _ := frozenSequenceFixture(t)
	phase1 := filepath.Join(planDir, "01-core.md")
	g.Nodes[0].Contract = "works while mentioning seq 2"
	st := states.Derive(states.Inputs{Graph: g})
	closed := states.Closed(g, st)
	if err := os.Remove(phase1); err != nil {
		t.Fatal(err)
	}
	if _, err := renderViews(root, "P", "", g, st, closed); err != nil {
		t.Fatalf("render contract fixture: %v", err)
	}
	frozen := rendererRead(t, phase1)

	g.SeqCounter++
	g.Nodes[0].Contract = "works while mentioning seq 3"
	st = states.Derive(states.Inputs{Graph: g})
	closed = states.Closed(g, st)
	if _, err := renderViews(root, "P", "", g, st, closed); err == nil {
		t.Fatal("a seq-like string in node text is a genuine frozen-history change and must refuse")
	}
	if got := rendererRead(t, phase1); got != frozen {
		t.Fatal("a refused node-text change must preserve the frozen bytes")
	}
}
