// Package hazards defines the closed vocabulary of failure classes a graph
// node may declare, and the shape of test each class demands before it is
// discharged (Designs/SddGraph DD-9, DD-13).
//
// The premise: every one of these defect classes has shipped past "passing
// tests" somewhere — the requirement is never merely *a* test, but a test
// built to fail in a particular way. A node declaring a hazard does not
// compile until one of its tests claims to satisfy it (`satisfies`), and a
// hazard-discharging test does not count as green until it has been seen to
// fail (`red_seq`, DD-5).
//
// The vocabulary is CLOSED. It is extended only by evidence — a defect class
// this project actually shipped — never by taxonomy-building. There is no
// plugin surface and no per-repo extension file, because a vocabulary anyone
// can grow stops being a checklist and becomes a folksonomy.
//
// The "nobody has looked yet" state is not in this package: the untriaged
// sentinel is a wire concern owned by the model package
// (model.UntriagedSentinel). This package only answers "is this a failure
// class we recognize, and what does discharging it require?"
package hazards

import (
	"fmt"
	"sort"
	"strings"
)

// Hazard is one failure class and the test shape that discharges it.
type Hazard struct {
	Name string `json:"name"`
	// RequiresTestThat completes the sentence "requires a test that …".
	RequiresTestThat string `json:"requires_test_that"`
}

// vocabulary is the closed set, in canonical (alphabetical) order.
var vocabulary = []Hazard{
	{"computes-number", "proves the quantity before equals the quantity after"},
	{"concurrent-access", "is proven to fail against a no-op implementation of the guard"},
	{"derives-state", "ties the derived set to an independent definition, not a restatement"},
	{"deterministic-replay", "asserts one seed gives a byte-identical trace across two runs"},
	{"external-format", "throws reserved words, quotes, and newlines at the output and reparses it"},
	{"frame-coupled", "asserts the tick delta between cause and effect, not eventual consistency"},
	{"order-sensitive", "uses input whose natural sort order is the reverse of its semantic order"},
	{"persists-state", "round-trips through the public API: construct, save, load"},
	{"ships-prose", "runs every command in the prose and checks every referenced artifact"},
	{"user-entrypoint", "exercises the real entry point as a subprocess"},
}

// All returns the vocabulary in canonical order. The returned slice is a
// copy; callers cannot mutate the vocabulary.
func All() []Hazard {
	out := make([]Hazard, len(vocabulary))
	copy(out, vocabulary)
	return out
}

// Names returns just the hazard names, in canonical order.
func Names() []string {
	names := make([]string, len(vocabulary))
	for i, h := range vocabulary {
		names[i] = h.Name
	}
	return names
}

// Known reports whether name is in the vocabulary.
func Known(name string) bool {
	for _, h := range vocabulary {
		if h.Name == name {
			return true
		}
	}
	return false
}

// Lookup returns the hazard with the given name.
func Lookup(name string) (Hazard, bool) {
	for _, h := range vocabulary {
		if h.Name == name {
			return h, true
		}
	}
	return Hazard{}, false
}

// RequireKnown returns an error naming the full vocabulary when name is not
// a recognized failure class. `where` locates the offending declaration
// (a node id, a JSON path) so the finding lands where the fix goes.
func RequireKnown(name, where string) error {
	if Known(name) {
		return nil
	}
	return fmt.Errorf("%s: %q is not a hazard in the closed vocabulary; known hazards are %s",
		where, name, strings.Join(Names(), ", "))
}

// RequireKnownAll checks every name and returns one error per unknown, in a
// deterministic order, so a batched caller (compile) reports them together.
func RequireKnownAll(names []string, where string) []error {
	var errs []error
	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Strings(sorted)
	for _, n := range sorted {
		if err := RequireKnown(n, where); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
