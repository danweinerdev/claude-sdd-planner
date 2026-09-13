//go:build unix

package procexec

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// POSIX containment (Designs/TestSuiteReliability DD-4): every command
// starts as the leader of its own process group. Cancellation signals that
// validated positive group and nothing else; after Wait, any group-staying
// descendant that outlived the command is killed and the group is polled
// until empty within the cleanup allowance. A process group is not a
// sandbox: a descendant that calls setsid escapes this contract, which
// ordinary Git fixtures prevent by disabling background services.

// signalGroup sends sig to the whole group. It is a variable so a test can
// simulate a refused signal; production never replaces it.
var signalGroup = func(pgid int, sig syscall.Signal) error { return syscall.Kill(-pgid, sig) }

// configureContainment puts the child in its own group and makes context
// cancellation kill that group rather than only the direct child.
func configureContainment(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		pgid := cmd.Process.Pid
		if pgid <= 1 {
			return fmt.Errorf("containment: refusing to signal process group %d", pgid)
		}
		if err := signalGroup(pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("containment: kill group %d: %w", pgid, err)
		}
		return nil
	}
	return nil
}

// cleanupGroup runs after Wait reaped the direct child. It reports whether
// descendants were still alive and had to be cleaned, and fails when the
// group cannot be signalled or does not empty within the allowance.
func cleanupGroup(cmd *exec.Cmd, allowance time.Duration) (cleaned bool, err error) {
	if cmd.Process == nil {
		return false, nil
	}
	pgid := cmd.Process.Pid
	if pgid <= 1 {
		return false, fmt.Errorf("containment: refusing to inspect process group %d", pgid)
	}
	probe := signalGroup(pgid, 0)
	if errors.Is(probe, syscall.ESRCH) {
		return false, nil // nothing left in the group
	}
	if probe != nil {
		return false, fmt.Errorf("containment: probe group %d: %w", pgid, probe)
	}
	if err := signalGroup(pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return false, fmt.Errorf("containment: kill group %d: %w", pgid, err)
	}
	deadline := time.Now().Add(allowance)
	for {
		err := signalGroup(pgid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("containment: probe group %d: %w", pgid, err)
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("containment: descendants in group %d survived %v after SIGKILL", pgid, allowance)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
