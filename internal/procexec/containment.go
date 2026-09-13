package procexec

import "errors"

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

// noAdapterError is the refusal Run returns when this build has no containment
// adapter. It unwraps to ErrNoContainmentAdapter so callers can match the case
// with errors.Is, but renders only the platform reason: the reason already
// opens with the sentinel's own wording, so wrapping it with %w printed "no
// process-containment adapter" twice in the line a user reads
// (review-execution facd924 F-02).
type noAdapterError struct{ reason string }

func (e *noAdapterError) Error() string {
	return e.reason + "; refusing to run uncontained"
}

func (e *noAdapterError) Unwrap() error { return ErrNoContainmentAdapter }

// errNoAdapter renders the refusal Run returns when this build has no
// containment adapter. It carries the platform and the follow-on plan in the
// message a user actually reads, so a CLI can prefix a label without having to
// restate the reason itself.
func errNoAdapter() error {
	_, reason := ContainmentSupported()
	if reason == "" {
		reason = "this platform has no process-containment adapter"
	}
	return &noAdapterError{reason: reason}
}
