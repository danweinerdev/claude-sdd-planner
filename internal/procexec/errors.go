// Package procexec runs external commands under an explicit execution policy:
// a finite deadline, a bounded cleanup/drain allowance, bounded diagnostic
// retention, a finite machine-output limit, and typed failure causes. It is
// the single owner of subprocess execution for VCS reads, fixture setup and
// owned helper processes (Designs/TestSuiteReliability DD-2, DD-9).
package procexec

import (
	"fmt"
	"strings"
)

// Cause classifies why a command did not produce a complete, successful result.
type Cause int

const (
	// CauseUnknown is the zero value; a returned *Error never carries it.
	CauseUnknown Cause = iota
	// CauseUnavailable: the executable could not be found on the resolved path.
	CauseUnavailable
	// CauseAccess: the executable exists but could not be started (permission,
	// not executable, corrupt image).
	CauseAccess
	// CauseDeadline: the policy timeout elapsed before the command finished.
	CauseDeadline
	// CauseCancelled: the caller's context was cancelled before completion.
	CauseCancelled
	// CauseExit: the command ran to completion and exited nonzero.
	CauseExit
	// CauseOverflow: machine output exceeded the policy limit; the result is
	// incomplete and must never be parsed as success.
	CauseOverflow
	// CauseDrain: the command exited but its pipes could not be drained within
	// the cleanup allowance (a descendant kept them open).
	CauseDrain
	// CauseContainment: either this build has no containment adapter for the
	// platform, so Run refused before launch, or the adapter failed to
	// establish or clean up ownership of the process tree. Callers that must
	// tell the two apart use errors.Is(err, ErrNoContainmentAdapter), which
	// matches only the pre-flight refusal.
	CauseContainment
)

func (c Cause) String() string {
	switch c {
	case CauseUnavailable:
		return "unavailable"
	case CauseAccess:
		return "access"
	case CauseDeadline:
		return "deadline"
	case CauseCancelled:
		return "cancelled"
	case CauseExit:
		return "exit"
	case CauseOverflow:
		return "overflow"
	case CauseDrain:
		return "drain"
	case CauseContainment:
		return "containment"
	}
	return "unknown"
}

// Error is the typed failure every Run returns on a non-success path.
type Error struct {
	Cause     Cause
	Argv      []string
	ExitCode  int    // valid when Cause == CauseExit
	Stderr    string // bounded diagnostic excerpt
	Stdout    string // machine output collected before the failure (bounded excerpt)
	Truncated bool   // Stderr was cut at the diagnostic limit
	Err       error  // underlying cause, if any
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", strings.Join(e.Argv, " "), e.Cause)
	if e.Cause == CauseExit {
		fmt.Fprintf(&b, " %d", e.ExitCode)
	}
	if e.Err != nil {
		fmt.Fprintf(&b, ": %v", e.Err)
	}
	if s := strings.TrimSpace(e.Stderr); s != "" {
		fmt.Fprintf(&b, ": %s", s)
		if e.Truncated {
			b.WriteString(" [truncated]")
		}
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }

// IsCause reports whether err is a procexec Error with the given cause.
func IsCause(err error, c Cause) bool {
	var pe *Error
	return asError(err, &pe) && pe.Cause == c
}
