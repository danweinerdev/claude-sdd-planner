//go:build unix

package procexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// alive reports whether pid still exists (a zombie counts until reaped).
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// waitDead polls until pid is gone or the bound elapses.
func waitDead(pid int, bound time.Duration) bool {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !alive(pid)
}

// sentinel starts an unrelated sleeping process in its own group and
// registers cleanup; the runner must never touch it.
func sentinel(t *testing.T) int {
	t.Helper()
	exe, _ := os.Executable()
	cmd := exec.Command(exe)
	cmd.Env = []string{helperEnv + "=sleep"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return pid
}

func pidFrom(t *testing.T, stdout []byte) int {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(string(stdout)))
	if err != nil || pid <= 1 {
		t.Fatalf("helper did not report a grandchild pid: %q", stdout)
	}
	return pid
}

func killLater(t *testing.T, pid int) {
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
}

// FR-14 / DD-4 (concurrent-access): an owned group-staying descendant is
// cleaned up both when the deadline kills a hung parent and when the parent
// completes normally; an unrelated process in another group survives. With
// a no-op containment adapter the grandchild outlives the run and this
// test fails.
func TestOwnedProcessLifecycle(t *testing.T) {
	unrelated := sentinel(t)

	// Deadline path: the parent hangs after spawning.
	exe, args, p := helperPolicy(t, "spawn-descendant")
	p.Timeout = 300 * time.Millisecond
	p.Cleanup = 2 * time.Second
	_, err := Run(context.Background(), exe, args, p)
	var pe *Error
	if !errors.As(err, &pe) || pe.Cause != CauseDeadline {
		t.Fatalf("hung parent: want CauseDeadline, got %v", err)
	}
	grandchild := pidFrom(t, []byte(pe.Stdout))
	killLater(t, grandchild)
	if !waitDead(grandchild, 2*time.Second) {
		t.Errorf("grandchild %d survived the deadline cleanup", grandchild)
	}

	// Normal completion: the parent exits 0 with a descendant still running.
	exe, args, p = helperPolicy(t, "spawn-descendant-exit")
	p.Cleanup = 2 * time.Second
	res, err := Run(context.Background(), exe, args, p)
	if err != nil {
		t.Fatalf("normal completion: %v", err)
	}
	grandchild = pidFrom(t, res.Stdout)
	killLater(t, grandchild)
	if !waitDead(grandchild, 2*time.Second) {
		t.Errorf("grandchild %d survived normal-completion cleanup", grandchild)
	}
	if !res.DescendantsCleaned {
		t.Error("result does not report that descendants were cleaned")
	}

	if !alive(unrelated) {
		t.Fatal("the unrelated sentinel process was killed; containment reached outside the owned group")
	}
}

// FR-13 / FR-15: a parent that exits successfully while a descendant holds
// its inherited stdout/stderr returns within the cleanup allowance with a
// successful result and the descendant cleaned, instead of blocking on the
// open pipe.
func TestEarlyExitInheritedPipes(t *testing.T) {
	exe, args, p := helperPolicy(t, "spawn-inherit-exit")
	p.Timeout = 10 * time.Second
	p.Cleanup = 500 * time.Millisecond
	start := time.Now()
	res, err := Run(context.Background(), exe, args, p)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("early exit with inherited pipes: %v", err)
	}
	if elapsed > p.Cleanup+3*time.Second {
		t.Fatalf("returned after %v; must not wait for the descendant beyond the cleanup allowance", elapsed)
	}
	grandchild := pidFrom(t, res.Stdout)
	killLater(t, grandchild)
	if !waitDead(grandchild, 2*time.Second) {
		t.Errorf("grandchild %d holding the pipes survived", grandchild)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code %d, want 0", res.ExitCode)
	}
}

// FR-14: a detectable containment or cleanup failure is an operational
// error, never a silent weakening of the guarantee.
func TestContainmentFailure(t *testing.T) {
	restore := signalGroup
	signalGroup = func(pgid int, sig syscall.Signal) error { return syscall.EPERM }
	t.Cleanup(func() { signalGroup = restore })

	exe, args, p := helperPolicy(t, "spawn-descendant-exit")
	p.Cleanup = 500 * time.Millisecond
	_, err := Run(context.Background(), exe, args, p)
	var pe *Error
	if !errors.As(err, &pe) || pe.Cause != CauseContainment {
		t.Fatalf("want CauseContainment, got %v", err)
	}
	if pe.Stdout != "" {
		if pid, convErr := strconv.Atoi(strings.TrimSpace(pe.Stdout)); convErr == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	if !strings.Contains(pe.Error(), "containment") {
		t.Errorf("error text must name containment: %q", pe.Error())
	}
}
