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
func configureContainment(cmd *exec.Cmd) error {
	return fmt.Errorf("containment: no process-containment adapter for %s; refusing to run uncontained", runtime.GOOS)
}

func cleanupGroup(cmd *exec.Cmd, allowance time.Duration) (bool, error) { return false, nil }
