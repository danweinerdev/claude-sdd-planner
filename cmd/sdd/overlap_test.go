package main

import "testing"

// The collision heuristic must catch real near-duplicates without firing on
// short statements that share a common word. A 2-token statement scored 0.50
// against an unrelated paragraph — over the scoped threshold — so a terse
// probe collided with nine unrelated ledger entries.
func TestOverlapStillCatchesRealCollisions(t *testing.T) {
	a := "The plugin ships a SessionStart hook that injects accepted ledger entries as additional context at session start."
	b := "SessionStart ledger injection moves into the Go binary as a hook, injecting accepted ledger entries as additional context at session start."
	if s := termOverlapScore(b, a); s < 0.6 {
		t.Errorf("a real near-duplicate must still score high; got %.2f", s)
	}
	// Short statements sharing one common word must NOT collide.
	if s := termOverlapScore("Check the ledger", a); s >= 0.3 {
		t.Errorf("a 2-token statement must not collide on one word; got %.2f", s)
	}
}
