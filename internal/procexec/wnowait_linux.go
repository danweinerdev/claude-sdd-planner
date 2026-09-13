//go:build linux

package procexec

import "golang.org/x/sys/unix"

// Linux exposes waitid(2), the only wait call that honours WNOWAIT: it
// reports the leader's exit while leaving it unreaped, so the leader stays a
// zombie and its pid cannot be recycled. wait4/waitpid reject WNOWAIT with
// EINVAL, so they cannot be used for this.
const wnowaitSupported = true

// awaitExitNoReap blocks until pid has exited, leaving it unreaped. EINTR is
// retried; any other error is returned so the caller falls back to the
// post-reap path rather than proceeding on an unverified assumption.
func awaitExitNoReap(pid int) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if err == unix.EINTR {
			continue
		}
		return err
	}
}
