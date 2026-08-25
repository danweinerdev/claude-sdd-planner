package rules

import (
	"os"
	"testing"
)

// TestMain disables the Perforce detection probe for this package's tests.
// The suite runs many full validation passes over temp-dir fixture roots
// that can never lie inside a Perforce client mapping, yet on a machine with
// a configured P4 server every vcs.Detect of a non-git directory paid a
// `p4 info` network RPC before the containment check could reject it —
// measured as the dominant share of suite wall-clock. No test here asserts
// positive Perforce detection (that needs a live server, which CI lacks),
// so the knob makes P4-configured workstations behave like CI, not
// differently from it. See internal/vcs/p4.go probeP4.
func TestMain(m *testing.M) {
	os.Setenv("SDD_VCS_DISABLE_P4", "1")
	os.Exit(m.Run())
}
