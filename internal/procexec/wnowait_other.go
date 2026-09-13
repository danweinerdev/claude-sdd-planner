//go:build !linux

package procexec

import "errors"

// On every platform other than Linux this package has no supported way to
// observe the group leader's exit without reaping it: x/sys/unix exports
// Waitid only on Linux, and syscall.Wait4 rejects WNOWAIT. The descendant
// sweep therefore still runs after the leader has been reaped, which leaves
// the residual window the Linux path is free of: between the reap and the
// sweep the kernel may recycle the leader's pid onto an unrelated new
// process-group leader, and the sweep would then signal that unrelated
// group. The window is narrow (the sweep's probe immediately follows Wait)
// but it is real, and closing it needs a per-platform waitid binding with
// the correct siginfo layout. Cancellation — signals sent while the leader
// is still alive — is unaffected on every platform.
const wnowaitSupported = false

var errNoWNOWAIT = errors.New("observing an exit without reaping is unsupported on this platform")

func awaitExitNoReap(pid int) error { return errNoWNOWAIT }
