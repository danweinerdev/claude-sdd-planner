package rules

import (
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
)

// The decisions package carries a copy of the DD definition pattern so it
// can extract Design Decisions without importing rules (rules imports it
// for the citation index). The two must never drift.
func TestDesignDecisionPatternParity(t *testing.T) {
	if got, want := decisions.DDPattern().String(), DefinitionPattern("DD").String(); got != want {
		t.Fatalf("DD pattern drift:\n decisions: %s\n rules:     %s", got, want)
	}
}
