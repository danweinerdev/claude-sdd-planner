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
// validated positive group and nothing else. Group-staying descendants that
// outlive the command are swept *before* the leader is reaped: while the
// leader is a zombie its pid is still allocated to it, so the group id
// cannot have been recycled onto an unrelated new group leader and the
// SIGKILL can only reach the group this runner owns. After the reap only an
// emptiness probe remains — never another kill, because from that instant
// the pid is recyclable. A process group is not a sandbox: a descendant
// that calls setsid escapes this contract, which ordinary Git fixtures
// prevent by disabling background services.

// platformContainmentSupported: POSIX process groups are the adapter
// (Designs/TestSuiteReliability DD-4).
func platformContainmentSupported() (bool, string) { return true, "" }

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

// groupPGID validates the leader pid as a signalable group id.
func groupPGID(cmd *exec.Cmd) (int, bool, error) {
	if cmd.Process == nil {
		return 0, false, nil
	}
	pgid := cmd.Process.Pid
	if pgid <= 1 {
		return 0, false, fmt.Errorf("containment: refusing to inspect process group %d", pgid)
	}
	return pgid, true, nil
}

// sweepGroupBeforeReap blocks until the group leader has exited without
// reaping it, then kills any descendant still in the group. Holding the
// leader as a zombie is what makes the kill safe: the pid it is signalling
// is still owned by this command's leader, so -pgid cannot name a stranger.
// It reports whether live descendants were found and killed. swept is the
// caller's licence to stop signalling, so it is reported only once a SIGKILL
// has actually been attempted; when the platform cannot observe an exit
// without reaping, or the pre-kill probe fails before any kill is sent, it
// reports swept=false so the caller keeps the documented post-reap fallback
// that still probes and kills (review-execution facd924 F-01). An error returned alongside
// swept=true is provisional: the kill was attempted at the only safe moment
// and cannot be retried after the reap, so whether it left anything behind is
// settled by the caller's emptiness poll, not by this return value.
func sweepGroupBeforeReap(cmd *exec.Cmd) (cleaned, swept bool, err error) {
	pgid, ok, err := groupPGID(cmd)
	if err != nil || !ok {
		return false, false, err
	}
	if !wnowaitSupported {
		return false, false, nil
	}
	if waitErr := awaitExitNoReap(pgid); waitErr != nil {
		// ECHILD means something already reaped the leader (it cannot be this
		// runner, which has not called Wait yet); either way the safe sweep is
		// no longer possible and the caller falls back.
		return false, false, nil
	}
	probe := signalGroup(pgid, 0)
	if errors.Is(probe, syscall.ESRCH) {
		return false, true, nil // the leader zombie aside, the group is empty
	}
	if probe != nil {
		// The probe failed before any SIGKILL was sent, so nothing has been
		// swept: reporting otherwise would licence the caller to only poll a
		// group that is still live and unsignalled. The failure is still an
		// operational error, and the caller's post-reap fallback retries the
		// probe and the kill within the cleanup allowance.
		return false, false, fmt.Errorf("containment: probe group %d: %w", pgid, probe)
	}
	if killErr := signalGroup(pgid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		// The probe already observed live members, so this is a sweep of a
		// non-empty group whatever the kill reported: kill(-pgid) yields a
		// single error for the whole group, so a refusal for one member says
		// nothing about the rest. cleaned stays true and the error is
		// provisional — the caller's post-reap poll decides whether anything
		// actually leaked (review-execution 57d4ffb F-01).
		return true, true, fmt.Errorf("containment: kill group %d: %w", pgid, killErr)
	}
	return true, true, nil
}

// cleanupGroup runs after Wait reaped the direct child. When the pre-reap
// sweep ran (swept), a SIGKILL has already been delivered to the group and
// the leader's pid is now recyclable, so this only polls the group for
// emptiness and never signals it again — which is why the timeout there may
// truthfully say the descendants survived a SIGKILL. When the sweep could
// not run — the platform lacks WNOWAIT, or its pre-kill probe failed before
// sending anything — it falls back to the original probe-and-kill, which
// carries the residual pid-reuse window documented in wnowait_other.go.
func cleanupGroup(cmd *exec.Cmd, allowance time.Duration, swept bool) (cleaned bool, err error) {
	pgid, ok, err := groupPGID(cmd)
	if err != nil || !ok {
		return false, err
	}
	if !swept {
		return cleanupGroupPostReap(pgid, allowance)
	}
	deadline := time.Now().Add(allowance)
	for {
		probe := signalGroup(pgid, 0)
		if errors.Is(probe, syscall.ESRCH) {
			return false, nil
		}
		if probe != nil {
			return false, fmt.Errorf("containment: probe group %d: %w", pgid, probe)
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("containment: descendants in group %d survived %v after SIGKILL", pgid, allowance)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// cleanupGroupPostReap is the WNOWAIT-less fallback: it probes, kills, and
// polls after the leader was reaped. Reaching -pgid here is only correct
// while the kernel has not recycled the leader's pid.
func cleanupGroupPostReap(pgid int, allowance time.Duration) (bool, error) {
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
