package compile

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
)

// FR-46: a `complete` artifact is history-bearing and must not be silently
// renormalized by an ordinary apply.
func TestFrozenRefusesCompleteStatus(t *testing.T) {
	existing := artifact.Parse(strings.Replace(payload(nil), "---\ntitle:", "---\nstatus: complete\ntitle:", 1))
	r := Compile(load(t), payload(nil), Options{Today: "2026-08-04", Existing: existing})
	if r.OK() {
		t.Fatal("compile succeeded rewriting a complete artifact")
	}
	if !hasCode(r, "SPK051") {
		t.Errorf("codes = %v, want SPK051", codes(r))
	}
	var found bool
	for _, ref := range r.Refusals {
		if ref.Code == "SPK051" && strings.Contains(ref.Message, "complete") {
			found = true
		}
	}
	if !found {
		t.Error("refusal does not name the status line")
	}
}

// A `frozen: true` artifact is refused the same way, distinct from `complete`.
func TestFrozenRefusesFrozenTrue(t *testing.T) {
	existing := artifact.Parse(strings.Replace(payload(nil), "---\ntitle:", "---\nfrozen: true\ntitle:", 1))
	r := Compile(load(t), payload(nil), Options{Today: "2026-08-04", Existing: existing})
	if r.OK() {
		t.Fatal("compile succeeded rewriting a frozen artifact")
	}
	if !hasCode(r, "SPK050") {
		t.Errorf("codes = %v, want SPK050", codes(r))
	}
}

// FR-47: the normalization migration is the one caller allowed to rewrite a
// frozen or complete artifact, via Options.AllowFrozen.
func TestAllowFrozenPermitsBothCases(t *testing.T) {
	t.Run("complete", func(t *testing.T) {
		existing := artifact.Parse(strings.Replace(payload(nil), "---\ntitle:", "---\nstatus: complete\ntitle:", 1))
		r := Compile(load(t), payload(nil), Options{Today: "2026-08-04", Existing: existing, AllowFrozen: true})
		mustOK(t, r)
	})
	t.Run("frozen", func(t *testing.T) {
		existing := artifact.Parse(strings.Replace(payload(nil), "---\ntitle:", "---\nfrozen: true\ntitle:", 1))
		r := Compile(load(t), payload(nil), Options{Today: "2026-08-04", Existing: existing, AllowFrozen: true})
		mustOK(t, r)
	})
}

// A normal draft spec is unaffected by the FR-46 guard entirely.
func TestFrozenDoesNotAffectDraftStatus(t *testing.T) {
	existing := artifact.Parse(payload(nil)) // status defaults to draft
	r := Compile(load(t), payload(nil), Options{Today: "2026-08-04", Existing: existing})
	mustOK(t, r)
}
