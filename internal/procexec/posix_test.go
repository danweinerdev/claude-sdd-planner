//go:build unix

package procexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
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

// leaderState reports the single-character process state of pid from
// /proc (Linux). 'Z' means exited but not yet reaped. The second result is
// false when /proc is unavailable or the process is entirely gone.
func leaderState(pid int) (byte, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	// The comm field is parenthesised and may contain spaces; the state
	// character is the first field after the closing parenthesis.
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 || i+2 >= len(b) {
		return 0, false
	}
	return b[i+2], true
}

// FR-14 / DD-4 (review F-02): the descendant sweep must signal the process
// group while the leader is still unreaped, so the leader's pid cannot have
// been recycled by an unrelated new group leader before the sweep lands.
// Under the pre-fix code the SIGKILL to -pgid ran after cmd.Wait reaped the
// leader, so at sweep time the leader had no /proc entry at all and this
// test fails.
func TestGroupSweepPrecedesReap(t *testing.T) {
	if _, ok := leaderState(os.Getpid()); !ok {
		t.Skip("/proc is unavailable; cannot observe the leader's zombie state")
	}

	restore := signalGroup
	type observation struct {
		state byte
		ok    bool
	}
	var mu sync.Mutex
	var kills []observation
	var leaderPID int

	signalGroup = func(pgid int, sig syscall.Signal) error {
		mu.Lock()
		if sig == syscall.SIGKILL && pgid == leaderPID && leaderPID != 0 {
			state, ok := leaderState(pgid)
			kills = append(kills, observation{state: state, ok: ok})
		}
		mu.Unlock()
		return restore(pgid, sig)
	}
	t.Cleanup(func() { signalGroup = restore })

	exe, args, p := helperPolicy(t, "spawn-descendant-exit")
	p.Cleanup = 2 * time.Second

	// The leader pid is the group id; learn it from the started command by
	// observing the only new group the runner creates. Run reports the
	// grandchild, so instead pin the leader via a start hook.
	startedPID := make(chan int, 1)
	observeStart = func(pid int) {
		mu.Lock()
		leaderPID = pid
		mu.Unlock()
		select {
		case startedPID <- pid:
		default:
		}
	}
	t.Cleanup(func() { observeStart = nil })

	res, err := Run(context.Background(), exe, args, p)
	if err != nil {
		t.Fatalf("normal completion: %v", err)
	}
	grandchild := pidFrom(t, res.Stdout)
	killLater(t, grandchild)
	if !waitDead(grandchild, 2*time.Second) {
		t.Errorf("grandchild %d survived the sweep", grandchild)
	}
	if !res.DescendantsCleaned {
		t.Error("result does not report that descendants were cleaned")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(kills) == 0 {
		t.Fatal("no SIGKILL was sent to the command's process group; the descendant sweep never ran")
	}
	first := kills[0]
	if !first.ok {
		t.Fatalf("at the first group SIGKILL the leader pid %d had no /proc entry: it was already reaped, so the pid could have been recycled", leaderPID)
	}
	if first.state != 'Z' {
		t.Fatalf("at the first group SIGKILL the leader was in state %q, want %q (exited but unreaped)", string(first.state), "Z")
	}
}
