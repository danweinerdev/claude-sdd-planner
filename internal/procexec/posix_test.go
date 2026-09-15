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

func TestCaptureCompletenessAfterDescendantCleanup(t *testing.T) {
	exe, args, p := helperPolicy(t, "spawn-inherit-exit")
	p.Timeout = 10 * time.Second
	p.Cleanup = 500 * time.Millisecond
	res, err := Capture(context.Background(), exe, args, p)
	if wnowaitSupported {
		// A pre-reap sweep closes the descendant's pipes before Cmd.Wait:
		// capture is complete, so rejecting it as a cut stream would be wrong.
		if err != nil {
			t.Fatalf("pre-reap capture: %v", err)
		}
		grandchild := pidFrom(t, res.Stdout)
		killLater(t, grandchild)
		if !res.DescendantsCleaned || !waitDead(grandchild, 2*time.Second) {
			t.Fatal("complete capture did not clean its pipe-holding descendant")
		}
		return
	}
	// Without a pre-reap sweep, WaitDelay closes the inherited pipes first.
	// Subsequent descendant cleanup cannot make that cut stream complete.
	if !IsCause(err, CauseDrain) || !emptyResult(res) {
		t.Fatalf("Capture returned (%+v, %v), want empty CauseDrain result", res, err)
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

// FR-14 / DD-4 (review F-01): a transient failure of the sweep's pre-kill
// probe must not be recorded as a completed sweep. Under the pre-fix code
// the probe's non-ESRCH error returned swept=true before any SIGKILL was
// sent, and the post-reap path then only polled: the live descendant ran out
// the whole cleanup allowance and the run reported that it had "survived
// after SIGKILL" although none was ever issued.
func TestTransientProbeFailureStillKills(t *testing.T) {
	restore := signalGroup
	var mu sync.Mutex
	probeFailed := false
	signalGroup = func(pgid int, sig syscall.Signal) error {
		mu.Lock()
		if sig == 0 && !probeFailed {
			probeFailed = true
			mu.Unlock()
			return syscall.EPERM
		}
		mu.Unlock()
		return restore(pgid, sig)
	}
	t.Cleanup(func() { signalGroup = restore })

	exe, args, p := helperPolicy(t, "spawn-descendant-exit")
	p.Cleanup = 700 * time.Millisecond

	start := time.Now()
	res, err := Run(context.Background(), exe, args, p)
	elapsed := time.Since(start)

	stdout := res.Stdout
	var pe *Error
	if errors.As(err, &pe) {
		stdout = []byte(pe.Stdout)
	}
	grandchild := pidFrom(t, stdout)
	killLater(t, grandchild)

	if !waitDead(grandchild, p.Cleanup+2*time.Second) {
		t.Errorf("grandchild %d survived: a failed pre-kill probe suppressed the SIGKILL", grandchild)
	}
	if err != nil && strings.Contains(err.Error(), "after SIGKILL") {
		t.Errorf("error claims descendants survived a SIGKILL that was never sent: %v", err)
	}
	if elapsed > p.Cleanup+3*time.Second {
		t.Errorf("returned after %v; the cleanup allowance must bound the run", elapsed)
	}
	mu.Lock()
	defer mu.Unlock()
	if !probeFailed {
		t.Fatal("the stubbed probe never ran; the test did not exercise the pre-kill probe path")
	}
}

// FR-14 / DD-4 (review F-01): a pre-reap probe failure that the post-reap
// fallback then resolved — it probed, killed, and confirmed the group empty —
// is not an operational failure. Under the pre-fix code Run merged the stale
// probe error into containErr whenever it was non-nil, so a command that
// exited 0 with its descendants demonstrably cleaned was reported as
// CauseContainment and every caller discarded a valid Result.Stdout.
func TestResolvedProbeFailureIsNotAnError(t *testing.T) {
	restore := signalGroup
	var mu sync.Mutex
	probeFailed := false
	signalGroup = func(pgid int, sig syscall.Signal) error {
		mu.Lock()
		if sig == 0 && !probeFailed {
			probeFailed = true
			mu.Unlock()
			return syscall.EPERM
		}
		mu.Unlock()
		return restore(pgid, sig)
	}
	t.Cleanup(func() { signalGroup = restore })

	exe, args, p := helperPolicy(t, "spawn-descendant-exit")
	p.Cleanup = 2 * time.Second

	res, err := Run(context.Background(), exe, args, p)
	if err != nil {
		var pe *Error
		if errors.As(err, &pe) && pe.Stdout != "" {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(pe.Stdout)); convErr == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		t.Fatalf("a probe failure the fallback resolved must not fail the run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code %d, want 0", res.ExitCode)
	}
	grandchild := pidFrom(t, res.Stdout)
	killLater(t, grandchild)
	if !waitDead(grandchild, p.Cleanup+2*time.Second) {
		t.Errorf("grandchild %d survived the fallback cleanup", grandchild)
	}
	if !res.DescendantsCleaned {
		t.Error("result does not report that descendants were cleaned")
	}
	mu.Lock()
	defer mu.Unlock()
	if !probeFailed {
		t.Fatal("the stubbed probe never ran; the test did not exercise the pre-kill probe path")
	}
}

// FR-14 / DD-4 (review F-01): a pre-reap SIGKILL that reported a non-ESRCH
// error but whose group the post-reap poll then observed empty is not an
// operational failure. kill(-pgid) reports one error for a whole group, so a
// refusal for one member coexists with delivery to the rest: the authority on
// whether descendants leaked is the emptiness poll, not the kill's return
// value. Under the pre-fix code sweepGroupBeforeReap returned swept=true
// alongside that error, and Run's guard kept the stale error and discarded the
// poll's answer, so a command that exited 0 with its descendants gone was
// reported as CauseContainment. The swept path must never re-signal the group
// after the reap (the leader's pid is recyclable from that instant), so the
// stub below models the real shape: the refused kill is still delivered.
func TestResolvedKillFailureIsNotAnError(t *testing.T) {
	restore := signalGroup
	var mu sync.Mutex
	killRefused := false
	signalGroup = func(pgid int, sig syscall.Signal) error {
		mu.Lock()
		if sig == syscall.SIGKILL && !killRefused {
			killRefused = true
			mu.Unlock()
			// The signal lands; the reported error does not describe that.
			_ = restore(pgid, sig)
			return syscall.EPERM
		}
		mu.Unlock()
		return restore(pgid, sig)
	}
	t.Cleanup(func() { signalGroup = restore })

	exe, args, p := helperPolicy(t, "spawn-descendant-exit")
	p.Cleanup = 2 * time.Second

	res, err := Run(context.Background(), exe, args, p)
	if err != nil {
		var pe *Error
		if errors.As(err, &pe) && pe.Stdout != "" {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(pe.Stdout)); convErr == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		t.Fatalf("a kill failure the post-reap poll resolved must not fail the run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code %d, want 0", res.ExitCode)
	}
	grandchild := pidFrom(t, res.Stdout)
	killLater(t, grandchild)
	if !waitDead(grandchild, p.Cleanup+2*time.Second) {
		t.Errorf("grandchild %d survived the sweep", grandchild)
	}
	if !res.DescendantsCleaned {
		t.Error("result does not report that descendants were cleaned")
	}
	mu.Lock()
	defer mu.Unlock()
	if !killRefused {
		t.Fatal("the stubbed kill never ran; the test did not exercise the pre-reap kill path")
	}
}
