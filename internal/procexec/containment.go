package procexec

import (
	"errors"
	"fmt"
)

// ErrNoContainmentAdapter is the sentinel every refusal on an uncontainable
// platform wraps. Callers match it with errors.Is to tell "this platform has
// no adapter yet" apart from "git is missing", which are otherwise both an
// operational could-not-run.
var ErrNoContainmentAdapter = errors.New("no process-containment adapter")

// ContainmentProbe reports whether this build has a containment adapter and,
// when it does not, why. It is a variable so a test on a supported platform
// can exercise the unsupported path; production never replaces it.
var ContainmentProbe = platformContainmentSupported

// ContainmentSupported reports whether commands can be run under process
// containment here. When it reports false the reason names the platform, the
// missing adapter, and the follow-on plan that would supply it.
func ContainmentSupported() (bool, string) { return ContainmentProbe() }

// errNoAdapter renders the refusal Run returns when this build has no
// containment adapter. It wraps ErrNoContainmentAdapter so a CLI can tell the
// case apart from a missing executable, and carries the platform and the
// follow-on plan in the message a user actually reads.
func errNoAdapter() error {
	_, reason := ContainmentSupported()
	if reason == "" {
		reason = "this platform has no process-containment adapter"
	}
	return fmt.Errorf("%w: %s; refusing to run uncontained", ErrNoContainmentAdapter, reason)
}
