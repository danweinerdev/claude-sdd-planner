//go:build !unix && !windows

package procexec

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// No containment adapter exists for this platform. Per decision pd-52a6f11c
// the runner refuses to run uncontained rather than silently weakening the
// guarantee. Windows has its own Job Object adapter in contain_windows.go.
func platformContainmentSupported() (bool, string) {
	return false, fmt.Sprintf("%s: no process-containment adapter", runtime.GOOS)
}

// configureContainment is unreachable on this platform: Run refuses before
// launch on the shared ContainmentSupported check, so every caller gets the
// one diagnosed refusal rather than two differently-worded ones.
func configureContainment(cmd *exec.Cmd) error { return errNoAdapter() }

func sweepGroupBeforeReap(cmd *exec.Cmd) (bool, bool, error) { return false, false, nil }

func cleanupGroup(cmd *exec.Cmd, allowance time.Duration, swept bool) (bool, error) {
	return false, nil
}
