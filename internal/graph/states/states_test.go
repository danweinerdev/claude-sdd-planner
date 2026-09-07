package states

import (
	"fmt"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func node(id string, deps []string, v *model.Verification) model.Node {
	return model.Node{
		ID: id, Contract: "c", Deps: deps,
		Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1,
		Verification: v,
	}
}

func pass(seq int) *model.Verification {
	return &model.Verification{Result: model.ResultPass, Seq: seq, Isolation: model.IsolationClean}
}

func fail(seq int) *model.Verification {
	return &model.Verification{Result: model.ResultFail, Seq: seq, Isolation: model.IsolationClean}
}

func TestStateTable(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{
		node("green-root", nil, pass(1)),
		node("ready", []string{"green-root"}, nil),
		node("blocked", []string{"ready"}, nil),
		// RED outranks BLOCKED: last verification failed AND a dep is
		// non-GREEN — it must still report RED.
		node("red-with-nongreen-dep", []string{"ready"}, fail(2)),
		// Seq staleness: verified at 3, but its dep re-verified at 5.
		node("reverified-dep", nil, pass(5)),
		node("seq-stale", []string{"reverified-dep"}, pass(3)),
		// Transitive seq staleness: the newer seq travels through a GREEN
		// middle node... middle verified at 6 > 5 keeps chain honest.
		node("middle", []string{"reverified-dep"}, pass(6)),
		node("transitively-stale", []string{"middle"}, pass(4)),
	}}
	s := Derive(Inputs{Graph: g})

	want := map[string]State{
		"green-root":            Green,
		"ready":                 Ready,
		"blocked":               Blocked,
		"red-with-nongreen-dep": Red,
		"reverified-dep":        Green,
		"seq-stale":             Stale,
		"middle":                Green,
		"transitively-stale":    Stale,
	}
	for id, w := range want {
		if s[id].State != w {
			t.Errorf("%s = %s, want %s", id, s[id].State, w)
		}
	}
	if !s["seq-stale"].SeqStale || !s["transitively-stale"].SeqStale {
		t.Error("seq staleness must be attributed as the cause")
	}

	// Workable ≠ frontier: RED with a non-GREEN dep is workable but off the
	// frontier; STALE with GREEN deps is on it.
	if !s["red-with-nongreen-dep"].Workable || s["red-with-nongreen-dep"].OnFrontier {
		t.Error("RED with a non-GREEN dep is workable but not frontier")
	}
	if !s["seq-stale"].Workable || !s["seq-stale"].OnFrontier {
		t.Error("STALE with GREEN deps belongs on the frontier")
	}
	if got := Frontier(s); len(got) == 0 {
		t.Error("frontier must not be empty here")
	}
}

func TestDigestStaleness(t *testing.T) {
	n := node("a", nil, pass(1))
	n.Artifacts = []string{"src/a.ext", "src/b.ext", "src/new.ext"}
	n.Verification.ArtifactDigests = map[string]string{
		"src/a.ext": "sha256:aaaa",
		"src/b.ext": "sha256:bbbb",
		// src/new.ext was declared after the observation: never recorded.
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{n}}

	current := map[string]string{
		"src/a.ext":   "sha256:aaaa", // unchanged
		"src/b.ext":   "sha256:DIFF", // silently edited
		"src/new.ext": "sha256:cccc",
	}
	s := Derive(Inputs{Graph: g, ArtifactDigest: func(rel string) string { return current[rel] }})
	ns := s["a"]
	if ns.State != Stale {
		t.Fatalf("state = %s, want STALE", ns.State)
	}
	if len(ns.DigestStale) != 2 || ns.DigestStale[0] != "src/b.ext" || ns.DigestStale[1] != "src/new.ext" {
		t.Fatalf("digest-stale artifacts = %v", ns.DigestStale)
	}

	// Without a digest source, the axis is disabled and the node is GREEN.
	s = Derive(Inputs{Graph: g})
	if s["a"].State != Green {
		t.Fatalf("digest axis disabled must not stale: %s", s["a"].State)
	}
}

func TestReviewGateDigestStalenessOverRecordedKeys(t *testing.T) {
	// A review gate declares no artifacts of its own; its observation
	// records the aggregate scope diff (every scope artifact's digest at
	// review time). Drift in ANY recorded key is ordinary digest staleness.
	gate := node("g1", nil, pass(1))
	gate.Gate = model.Gate{Type: model.GateReview}
	gate.Verification.ArtifactDigests = map[string]string{
		"src/a.ext": "sha256:aaaa",
		"src/b.ext": "sha256:bbbb",
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{gate}}

	fresh := map[string]string{"src/a.ext": "sha256:aaaa", "src/b.ext": "sha256:bbbb"}
	s := Derive(Inputs{Graph: g, ArtifactDigest: func(rel string) string { return fresh[rel] }})
	if s["g1"].State != Green {
		t.Fatalf("matching aggregate diff must stay GREEN: %+v", s["g1"])
	}

	drifted := map[string]string{"src/a.ext": "sha256:aaaa", "src/b.ext": "sha256:DIFF"}
	s = Derive(Inputs{Graph: g, ArtifactDigest: func(rel string) string { return drifted[rel] }})
	if s["g1"].State != Stale || len(s["g1"].DigestStale) != 1 || s["g1"].DigestStale[0] != "src/b.ext" {
		t.Fatalf("a drifted scope artifact must stale the gate: %+v", s["g1"])
	}

	// A reviewed artifact deleted from disk is drift too.
	gone := map[string]string{"src/a.ext": "sha256:aaaa"}
	s = Derive(Inputs{Graph: g, ArtifactDigest: func(rel string) string { return gone[rel] }})
	if s["g1"].State != Stale {
		t.Fatalf("a deleted reviewed artifact must stale the gate: %+v", s["g1"])
	}
}

func TestIntentStaleness(t *testing.T) {
	n := node("a", nil, pass(1))
	n.IntentHashes = map[string]string{"AC-01": "sha256:old", "FR-01": "sha256:same"}
	g := &model.Graph{Version: 1, Nodes: []model.Node{n}}

	s := Derive(Inputs{Graph: g, CurrentIntentHashes: map[string]string{
		"AC-01": "sha256:new", // requirement reworded
		"FR-01": "sha256:same",
	}})
	ns := s["a"]
	if ns.State != Stale || len(ns.IntentStale) != 1 || ns.IntentStale[0] != "AC-01" {
		t.Fatalf("INTENT-STALE must name exactly the drifted citation: %+v", ns)
	}
	if ns.SeqStale || len(ns.DigestStale) != 0 {
		t.Fatal("intent staleness must be attributed distinctly from the other axes")
	}

	// A citation that no longer resolves at all is also intent-stale.
	s = Derive(Inputs{Graph: g, CurrentIntentHashes: map[string]string{"FR-01": "sha256:same"}})
	if got := s["a"].IntentStale; len(got) != 1 || got[0] != "AC-01" {
		t.Fatalf("an unresolvable citation is intent-stale: %v", got)
	}
}

// TestMissingFingerprintFailsClosed: a recorded-pass node carrying a currently
// fingerprintable justification with no embedded hash must derive STALE, not
// GREEN — the split bug (children compiled without fingerprints) otherwise
// reads GREEN against text the node never anchored to. An accepted decision
// stays GREEN only when positively identified in DecisionExemptions, and a
// nil CurrentIntentHashes disables the axis.
func TestMissingFingerprintFailsClosed(t *testing.T) {
	// Missing entirely: a justification with no hash at all.
	n := node("a", nil, pass(1))
	n.Justifies = []string{"AC-01"}
	g := &model.Graph{Version: 1, Nodes: []model.Node{n}}
	s := Derive(Inputs{Graph: g, CurrentIntentHashes: map[string]string{"AC-01": "sha256:current"}})
	if s["a"].State != Stale || len(s["a"].IntentStale) != 1 || s["a"].IntentStale[0] != "AC-01" {
		t.Fatalf("missing fingerprint must be intent-stale: %+v", s["a"])
	}

	// Empty (present-but-empty): the embedded hash is a no-op anchor.
	ne := node("empty", nil, pass(1))
	ne.Justifies = []string{"AC-01"}
	ne.IntentHashes = map[string]string{"AC-01": ""}
	ge := &model.Graph{Version: 1, Nodes: []model.Node{ne}}
	se := Derive(Inputs{Graph: ge, CurrentIntentHashes: map[string]string{"AC-01": "sha256:current"}})
	if se["empty"].State != Stale || len(se["empty"].IntentStale) != 1 || se["empty"].IntentStale[0] != "AC-01" {
		t.Fatalf("an empty embedded hash must be intent-stale: %+v", se["empty"])
	}

	// Partial: one justification hashed, the other not — the gap is stale.
	n2 := node("b", nil, pass(1))
	n2.Justifies = []string{"AC-01", "FR-01"}
	n2.IntentHashes = map[string]string{"AC-01": "sha256:current"}
	g2 := &model.Graph{Version: 1, Nodes: []model.Node{n2}}
	s2 := Derive(Inputs{Graph: g2, CurrentIntentHashes: map[string]string{
		"AC-01": "sha256:current", "FR-01": "sha256:fr"}})
	if s2["b"].State != Stale || len(s2["b"].IntentStale) != 1 || s2["b"].IntentStale[0] != "FR-01" {
		t.Fatalf("partial map must name exactly the missing ref: %+v", s2["b"])
	}

	// An accepted decision is legitimately exempt: GREEN, but only when the
	// caller positively identifies it in DecisionExemptions (no D-prefix
	// shape exemption).
	n3 := node("c", nil, pass(1))
	n3.Justifies = []string{"D-0001"}
	g3 := &model.Graph{Version: 1, Nodes: []model.Node{n3}}
	s3 := Derive(Inputs{Graph: g3, CurrentIntentHashes: map[string]string{},
		DecisionExemptions: map[string]bool{"D-0001": true}})
	if s3["c"].State != Green {
		t.Fatalf("an accepted decision-only node must stay GREEN: %+v", s3["c"])
	}

	// A nil CurrentIntentHashes disables the axis: GREEN.
	s4 := Derive(Inputs{Graph: g})
	if s4["a"].State != Green {
		t.Fatalf("nil CurrentIntentHashes must disable the axis: %+v", s4["a"])
	}
}

// TestCitationDispositionFailsClosed: a recorded-pass node whose citation is
// absent from the current hash snapshot AND not positively identified as an
// exempt decision derives STALE. This is the deletion/unlinking/ambiguity bug
// the missing-hash loop alone cannot see: a citation that vanishes from the
// current tree leaves no hash entry to mismatch, so without the fail-closed
// disposition check the PASS silently derives GREEN. Ambiguous and deleted
// citations are indistinguishable at the derive boundary — both land in
// neither map — so the one guard covers both.
func TestCitationDispositionFailsClosed(t *testing.T) {
	// Deleted/unlinked: AC-01 was resolvable at compile, the source (or its
	// `related` link) has since vanished — the current snapshot has no entry.
	nd := node("deleted", nil, pass(1))
	nd.Justifies = []string{"AC-01"}
	gd := &model.Graph{Version: 1, Nodes: []model.Node{nd}}
	sd := Derive(Inputs{Graph: gd, CurrentIntentHashes: map[string]string{}})
	if sd["deleted"].State != Stale || len(sd["deleted"].IntentStale) != 1 || sd["deleted"].IntentStale[0] != "AC-01" {
		t.Fatalf("a citation deleted since the observation must derive STALE: %+v", sd["deleted"])
	}

	// Newly ambiguous: the same absence, the same verdict (a bare id that
	// now resolves to more than one source is absent from the snapshot).
	na := node("ambiguous", nil, pass(1))
	na.Justifies = []string{"AC-01"}
	ga := &model.Graph{Version: 1, Nodes: []model.Node{na}}
	sa := Derive(Inputs{Graph: ga, CurrentIntentHashes: map[string]string{}})
	if sa["ambiguous"].State != Stale || len(sa["ambiguous"].IntentStale) != 1 || sa["ambiguous"].IntentStale[0] != "AC-01" {
		t.Fatalf("a citation newly ambiguous must derive STALE: %+v", sa["ambiguous"])
	}

	// Unknown decision-like string: a D-shaped id the ledger does not define
	// is NOT an exemption — no name-shape exemption exists.
	nu := node("unknown-decision", nil, pass(1))
	nu.Justifies = []string{"D-9999"}
	gu := &model.Graph{Version: 1, Nodes: []model.Node{nu}}
	su := Derive(Inputs{Graph: gu, CurrentIntentHashes: map[string]string{},
		DecisionExemptions: map[string]bool{"D-0001": true}})
	if su["unknown-decision"].State != Stale || len(su["unknown-decision"].IntentStale) != 1 || su["unknown-decision"].IntentStale[0] != "D-9999" {
		t.Fatalf("an unknown D-shaped citation must NOT be exempt: %+v", su["unknown-decision"])
	}

	// A non-accepted decision (superseded/rejected) is not a legitimate
	// exemption either: it lands in neither map and derives STALE.
	nsup := node("superseded-decision", nil, pass(1))
	nsup.Justifies = []string{"D-0002"}
	gsup := &model.Graph{Version: 1, Nodes: []model.Node{nsup}}
	ssup := Derive(Inputs{Graph: gsup, CurrentIntentHashes: map[string]string{},
		DecisionExemptions: map[string]bool{"D-0001": true}})
	if ssup["superseded-decision"].State != Stale || len(ssup["superseded-decision"].IntentStale) != 1 {
		t.Fatalf("a superseded decision must NOT be exempt: %+v", ssup["superseded-decision"])
	}
}

func TestEmptyAnchorDoesNotHideLostCitation(t *testing.T) {
	for _, tc := range []struct {
		name, cited string
		exempt      bool
		want        State
	}{
		{"deleted-or-ambiguous", "AC-01", false, Stale},
		{"unknown-decision", "D-9999", false, Stale},
		{"accepted-decision", "D-0001", true, Green},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := node("a", nil, pass(1))
			n.Justifies = []string{tc.cited}
			n.IntentHashes = map[string]string{tc.cited: ""}
			got := Derive(Inputs{
				Graph:               &model.Graph{Version: 1, Nodes: []model.Node{n}},
				CurrentIntentHashes: map[string]string{},
				DecisionExemptions:  map[string]bool{tc.cited: tc.exempt},
			})["a"]
			if got.State != tc.want {
				t.Fatalf("empty anchor with absent source: got %+v, want %s", got, tc.want)
			}
		})
	}
}

func TestCycleMembersAreBlockedDefensively(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{
		node("a", []string{"b"}, nil),
		node("b", []string{"a"}, nil),
		node("ok", nil, nil),
	}}
	s := Derive(Inputs{Graph: g})
	for _, id := range []string{"a", "b"} {
		if s[id].State != Blocked || !s[id].InCycle || s[id].Workable {
			t.Errorf("%s: cycle members are BLOCKED, flagged, never workable: %+v", id, s[id])
		}
	}
	if s["ok"].State != Ready {
		t.Errorf("the acyclic remainder still derives: %+v", s["ok"])
	}
}

// TestInputStaleness pins the INPUT-STALE axis: a recorded-pass node whose
// declared input changed, no longer resolves, or carries no embedded hash
// derives STALE; a nil CurrentInputHashes disables the axis.
func TestInputStaleness(t *testing.T) {
	spec := model.Input{Root: model.InputRootRepository, Path: "docs/readme.md"}
	key := model.InputKey(spec)

	// Changed: the recorded digest no longer matches the current content.
	n := node("a", nil, pass(1))
	n.Inputs = []model.Input{spec}
	n.InputHashes = map[string]string{key: "sha256:old"}
	g := &model.Graph{Version: 1, Nodes: []model.Node{n}}
	s := Derive(Inputs{Graph: g, CurrentInputHashes: map[string]string{key: "sha256:new"}})
	if s["a"].State != Stale || len(s["a"].InputStale) != 1 || s["a"].InputStale[0] != key {
		t.Fatalf("a changed input must be INPUT-STALE: %+v", s["a"])
	}

	// Unresolvable: the input is absent from the current hash map.
	s = Derive(Inputs{Graph: g, CurrentInputHashes: map[string]string{}})
	if s["a"].State != Stale || len(s["a"].InputStale) != 1 {
		t.Fatalf("an unresolvable input must be INPUT-STALE: %+v", s["a"])
	}

	// Missing fingerprint: declared but no embedded hash.
	nm := node("missing", nil, pass(1))
	nm.Inputs = []model.Input{spec}
	gm := &model.Graph{Version: 1, Nodes: []model.Node{nm}}
	sm := Derive(Inputs{Graph: gm, CurrentInputHashes: map[string]string{key: "sha256:cur"}})
	if sm["missing"].State != Stale || len(sm["missing"].InputStale) != 1 {
		t.Fatalf("a declared input with no hash must be INPUT-STALE: %+v", sm["missing"])
	}

	// Matching: GREEN.
	ok := node("ok", nil, pass(1))
	ok.Inputs = []model.Input{spec}
	ok.InputHashes = map[string]string{key: "sha256:cur"}
	gok := &model.Graph{Version: 1, Nodes: []model.Node{ok}}
	sok := Derive(Inputs{Graph: gok, CurrentInputHashes: map[string]string{key: "sha256:cur"}})
	if sok["ok"].State != Green {
		t.Fatalf("a matching input must stay GREEN: %+v", sok["ok"])
	}

	// Axis disabled: GREEN.
	sdis := Derive(Inputs{Graph: g})
	if sdis["a"].State != Green {
		t.Fatalf("nil CurrentInputHashes must disable the input axis: %+v", sdis["a"])
	}
}

// TestNothingDerivableIsStored pins the DD-3 rule at the API level: Derive
// takes a graph and returns states; running it twice over the same inputs is
// pure, and mutating the returned map cannot affect a later derive.
func TestNothingDerivableIsStored(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{node("a", nil, pass(1))}}
	first := Derive(Inputs{Graph: g})
	first["a"] = NodeState{ID: "a", State: Red}
	second := Derive(Inputs{Graph: g})
	if second["a"].State != Green {
		t.Fatal("derive must be pure; a caller's mutation leaked into a later pass")
	}
}

func TestThousandNodePassIsFast(t *testing.T) {
	g := &model.Graph{Version: 1}
	for i := 0; i < 1000; i++ {
		var deps []string
		if i > 0 {
			deps = []string{fmt.Sprintf("n%03d", (i-1)/2)} // a wide-ish DAG
		}
		g.Nodes = append(g.Nodes, node(fmt.Sprintf("n%03d", i), deps, pass(i%7)))
	}
	start := time.Now()
	s := Derive(Inputs{Graph: g})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("1000-node derive took %v, want < 1s", elapsed)
	}
	if len(s) != 1000 {
		t.Fatalf("derived %d states", len(s))
	}
}
