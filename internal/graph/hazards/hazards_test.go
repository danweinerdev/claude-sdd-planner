package hazards

import (
	"sort"
	"strings"
	"testing"
)

func TestVocabularyIsCompleteAndCanonical(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("vocabulary must not be empty")
	}
	seen := map[string]bool{}
	for _, h := range all {
		if strings.TrimSpace(h.Name) == "" {
			t.Fatalf("hazard with empty name: %+v", h)
		}
		if strings.TrimSpace(h.RequiresTestThat) == "" {
			t.Fatalf("hazard %q carries no required test shape — an undescribed hazard cannot be discharged", h.Name)
		}
		if seen[h.Name] {
			t.Fatalf("duplicate hazard %q", h.Name)
		}
		seen[h.Name] = true
	}
	if !sort.SliceIsSorted(all, func(i, j int) bool { return all[i].Name < all[j].Name }) {
		t.Fatal("vocabulary must be in canonical (alphabetical) order")
	}
}

func TestAllReturnsACopy(t *testing.T) {
	a := All()
	a[0].Name = "mutated"
	if All()[0].Name == "mutated" {
		t.Fatal("All must return a copy; the vocabulary is closed, not shared mutable state")
	}
}

func TestKnownAndLookup(t *testing.T) {
	for _, name := range Names() {
		if !Known(name) {
			t.Fatalf("Known(%q) = false for a vocabulary entry", name)
		}
		h, ok := Lookup(name)
		if !ok || h.Name != name {
			t.Fatalf("Lookup(%q) failed: %+v %v", name, h, ok)
		}
	}
	if Known("made-up-hazard") {
		t.Fatal("Known must reject a hazard outside the vocabulary")
	}
}

func TestRequireKnownNamesTheVocabulary(t *testing.T) {
	if err := RequireKnown("frame-coupled", "nodes[0]"); err != nil {
		t.Fatalf("known hazard rejected: %v", err)
	}
	err := RequireKnown("race-condition", `node "watch"`)
	if err == nil {
		t.Fatal("unknown hazard must be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, `node "watch"`) {
		t.Fatalf("error must carry the caller's location: %v", msg)
	}
	if !strings.Contains(msg, `"race-condition" is not a hazard`) {
		t.Fatalf("error must name the offender: %v", msg)
	}
	for _, name := range Names() {
		if !strings.Contains(msg, name) {
			t.Fatalf("error must name the full vocabulary (missing %q): %v", name, msg)
		}
	}
}

func TestRequireKnownAllBatchesDeterministically(t *testing.T) {
	errs := RequireKnownAll([]string{"zzz", "frame-coupled", "aaa"}, "nodes[3]")
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), `"aaa"`) || !strings.Contains(errs[1].Error(), `"zzz"`) {
		t.Fatalf("errors must be deterministic and sorted: %v", errs)
	}
}
