//go:build !unix

package procexec

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// No containment adapter exists for this platform yet (the Windows Job
// Object adapter, Designs/TestSuiteReliability DD-3, is a follow-on plan).
// Per the plan's decision pd-52a6f11c the runner refuses to run uncontained
// rather than silently weakening the guarantee.
func platformContainmentSupported() (bool, string) {
	return false, fmt.Sprintf("%s: no process-containment adapter (the Windows Job Object adapter, Designs/TestSuiteReliability DD-3, is a follow-on plan)", runtime.GOOS)
}

// configureContainment is unreachable on this platform: Run refuses before
// launch on the shared ContainmentSupported check, so every caller gets the
// one diagnosed refusal rather than two differently-worded ones.
func configureContainment(cmd *exec.Cmd) error { return errNoAdapter() }

func sweepGroupBeforeReap(cmd *exec.Cmd) (bool, bool, error) { return false, false, nil }

func cleanupGroup(cmd *exec.Cmd, allowance time.Duration, swept bool) (bool, error) {
	return false, nil
}
