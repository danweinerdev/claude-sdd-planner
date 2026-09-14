//go:build windows

package procexec

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func windowsProcessAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	state, err := windows.WaitForSingleObject(h, 0)
	return err == nil && state == uint32(windows.WAIT_TIMEOUT)
}

func waitWindowsDead(pid int, bound time.Duration) bool {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if !windowsProcessAlive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !windowsProcessAlive(pid)
}

func windowsPIDFrom(t *testing.T, out []byte) int {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 1 {
		t.Fatalf("invalid descendant pid %q", out)
	}
	return pid
}

func TestWindowsArgumentsEnvironmentAndDirectory(t *testing.T) {
	exe, _, p := helperPolicy(t, "echo-args")
	want := []string{"space here", `quote"here`, `slash\\\"quote`, "雪だるま", ""}
	res, err := Run(context.Background(), exe, want, p)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(string(res.Stdout), "\x00"); !equalStrings(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}

	dir := t.TempDir()
	exe, _, p = helperPolicy(t, "env-dir", "PROCEXEC_FIDELITY=héllø-世界")
	p.Dir = dir
	res, err = Run(context.Background(), exe, nil, p)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(res.Stdout), "\x00")
	if len(parts) != 2 || !strings.EqualFold(parts[0], dir) || parts[1] != "héllø-世界" {
		t.Fatalf("dir/env output = %#v", parts)
	}
}

func envBlockEntries(t *testing.T, block []uint16) []string {
	t.Helper()
	if len(block) < 2 || block[len(block)-1] != 0 || block[len(block)-2] != 0 {
		t.Fatalf("environment block is not double-NUL terminated: %#v", block)
	}
	var entries []string
	start := 0
	for i, r := range block[:len(block)-1] {
		if r == 0 {
			if i > start {
				entries = append(entries, windows.UTF16ToString(block[start:i]))
			}
			start = i + 1
		}
	}
	return entries
}

func TestWindowsEnvironmentBlockMatchesExecSemantics(t *testing.T) {
	source := []string{"Zoo=first", "foo=z-last-lexically", "=C:=C:\\first", "FOO=a-actual-last", "=c:=C:\\last"}
	wantSource := append([]string(nil), source...)
	block, err := windowsEnvBlock(source)
	if err != nil {
		t.Fatal(err)
	}
	entries := envBlockEntries(t, block)
	joined := strings.Join(entries, "\n")
	if strings.Contains(strings.ToLower(joined), "foo=z-last-lexically") || !strings.Contains(joined, "FOO=a-actual-last") {
		t.Fatalf("case-insensitive last-wins dedup failed: %#v", entries)
	}
	if strings.Contains(joined, "=C:=C:\\first") || !strings.Contains(joined, "=c:=C:\\last") {
		t.Fatalf("drive-current-directory key dedup failed: %#v", entries)
	}
	if !equalStrings(source, wantSource) {
		t.Fatalf("source environment mutated: %#v", source)
	}
	for i := 1; i < len(entries); i++ {
		key := func(s string) string {
			at := strings.Index(s, "=")
			if at == 0 {
				at = strings.Index(s[1:], "=") + 1
			}
			return strings.ToLower(s[:at])
		}
		if key(entries[i-1]) > key(entries[i]) {
			t.Fatalf("environment not sorted by key: %#v", entries)
		}
	}

	empty, err := windowsEnvBlock([]string{})
	if err != nil {
		t.Fatal(err)
	}
	emptyEntries := envBlockEntries(t, empty)
	if root := os.Getenv("SYSTEMROOT"); root != "" {
		if len(emptyEntries) != 1 || !strings.EqualFold(emptyEntries[0], "SYSTEMROOT="+root) {
			t.Fatalf("explicit empty environment = %#v", emptyEntries)
		}
	} else if len(emptyEntries) != 1 || !strings.EqualFold(emptyEntries[0], "SYSTEMROOT=") {
		t.Fatalf("explicit empty environment = %#v", emptyEntries)
	}
	if inherited, err := windowsEnvBlock(nil); err != nil || inherited != nil {
		t.Fatalf("nil environment = %#v, %v; want inherited nil block", inherited, err)
	}
	if _, err := windowsEnvBlock([]string{"BAD=x\x00y"}); err == nil {
		t.Fatal("NUL-containing environment was accepted")
	}
}

func TestWindowsStreamJoinTimeoutClosesAndJoins(t *testing.T) {
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdoutW.Close()
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stderrW.Close()
	stdoutDone := make(chan error, 1)
	stderrDone := make(chan error, 1)
	stdoutExited := make(chan struct{})
	stderrExited := make(chan struct{})
	// os.Pipe reader Close cancels pending Windows I/O. Publish the exit
	// signal before the result so receiving the result orders both assertions.
	go func() { _, err := io.Copy(io.Discard, stdoutR); close(stdoutExited); stdoutDone <- err }()
	go func() { _, err := io.Copy(io.Discard, stderrR); close(stderrExited); stderrDone <- err }()
	start := time.Now()
	err = joinStreams(time.Now().Add(50*time.Millisecond), stdoutR, stderrR, stdoutDone, stderrDone)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("join error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("join took %v", elapsed)
	}
	select {
	case <-stdoutExited:
	default:
		t.Fatal("stdout copier still running")
	}
	select {
	case <-stderrExited:
	default:
		t.Fatal("stderr copier still running")
	}
}

func TestWindowsStreamJoinReportsCopyError(t *testing.T) {
	stdoutR, stdoutW, _ := os.Pipe()
	stderrR, stderrW, _ := os.Pipe()
	defer stdoutW.Close()
	defer stderrW.Close()
	want := errors.New("deliberate copy failure")
	stdoutDone := make(chan error, 1)
	stderrDone := make(chan error, 1)
	stdoutDone <- want
	stderrDone <- nil
	err := joinStreams(time.Now().Add(time.Second), stdoutR, stderrR, stdoutDone, stderrDone)
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("join error = %v", err)
	}
}

func TestWindowsLargeCombinedOutputIsComplete(t *testing.T) {
	const n = 4 << 20
	exe, args, p := helperPolicy(t, "stdout-stderr", "PROCEXEC_HELPER_N="+strconv.Itoa(n))
	res, err := Run(context.Background(), exe, args, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stdout) != n || res.Stderr != "stderr-sentinel" {
		t.Fatalf("stdout/stderr = %d/%q", len(res.Stdout), res.Stderr)
	}
}

func TestWindowsInheritedEnvironment(t *testing.T) {
	t.Setenv(helperEnv, "env-value")
	t.Setenv("PROCEXEC_ENV_VALUE", "inherited-value")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), exe, nil, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "inherited-value" {
		t.Fatalf("inherited value = %q", res.Stdout)
	}
}

func TestWindowsDirectPathWithoutExtensionUsesPATHEXT(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tool := filepath.Join(dir, "tool.exe")
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, b, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(helperEnv, "exit")
	p := Policy{}
	if _, err := Run(context.Background(), filepath.Join(dir, "tool"), nil, p); err != nil {
		t.Fatalf("extensionless direct path: %v", err)
	}
}

func TestWindowsDirRelativeExecutable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tool := filepath.Join(dir, "relative-tool.exe")
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, b, 0o755); err != nil {
		t.Fatal(err)
	}
	p := Policy{Dir: dir, Env: []string{helperEnv + "=echo-args", "PATH=" + os.Getenv("PATH")}}
	want := []string{"space arg", `quoted"arg`, "雪"}
	res, err := Run(context.Background(), `.\relative-tool.exe`, want, p)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(string(res.Stdout), "\x00"); !equalStrings(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWindowsOwnedLifecycleAndUnrelatedSentinel(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	sentinel := exec.Command(exe)
	sentinel.Env = []string{helperEnv + "=sleep"}
	if err := sentinel.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sentinel.Process.Kill(); _, _ = sentinel.Process.Wait() })

	for _, tc := range []struct {
		mode    string
		timeout time.Duration
	}{
		{"spawn-descendant", 300 * time.Millisecond},
		{"spawn-descendant-exit", 10 * time.Second},
		{"spawn-inherit-exit", 10 * time.Second},
	} {
		exe, args, p := helperPolicy(t, tc.mode)
		p.Timeout, p.Cleanup = tc.timeout, 2*time.Second
		res, runErr := Run(context.Background(), exe, args, p)
		out := res.Stdout
		var pe *Error
		if errors.As(runErr, &pe) {
			out = []byte(pe.Stdout)
		}
		if tc.mode == "spawn-descendant" {
			if !IsCause(runErr, CauseDeadline) {
				t.Fatalf("%s: %v", tc.mode, runErr)
			}
		} else if runErr != nil {
			t.Fatalf("%s: %v", tc.mode, runErr)
		}
		if runErr == nil && !res.DescendantsCleaned {
			t.Fatalf("%s did not report cleaned descendants", tc.mode)
		}
		pid := windowsPIDFrom(t, out)
		if !waitWindowsDead(pid, p.Cleanup+time.Second) {
			t.Fatalf("%s descendant %d survived", tc.mode, pid)
		}
	}
	if !windowsProcessAlive(sentinel.Process.Pid) {
		t.Fatal("unrelated sentinel was killed")
	}
}

func TestWindowsPlainSuccessDoesNotReportDescendants(t *testing.T) {
	exe, args, p := helperPolicy(t, "exit")
	res, err := Run(context.Background(), exe, args, p)
	if err != nil {
		t.Fatal(err)
	}
	if res.DescendantsCleaned {
		t.Fatal("plain child reported descendant cleanup")
	}
}

func TestWindowsAlreadyCancelledDoesNotLaunch(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	exe, args, p := helperPolicy(t, "mutate-then-fail", "PROCEXEC_HELPER_COUNTER="+marker)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, exe, args, p)
	if !IsCause(err, CauseCancelled) {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancelled command ran: %v", statErr)
	}
}

func TestWindowsAlreadyExpiredPolicyDeadlineDoesNotLaunch(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	exe, args, p := helperPolicy(t, "mutate-then-fail", "PROCEXEC_HELPER_COUNTER="+marker)
	p.Timeout = time.Nanosecond
	_, err := Run(context.Background(), exe, args, p)
	if !IsCause(err, CauseDeadline) {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expired command ran: %v", statErr)
	}
}

func TestWindowsCancellationBeforeResumeNeverRuns(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	exe, _, p := helperPolicy(t, "mutate-then-fail", "PROCEXEC_HELPER_COUNTER="+marker)
	p = p.withDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	backend := productionWindowsBackend()
	backend.observeSuspended = func(uint32) { cancel() }
	stdout := &machineWriter{limit: p.MachineLimit, onOverflow: func() {}}
	stderr := &excerptWriter{limit: p.DiagnosticLimit}
	_, cause, err := runWindows(ctx, exe, []string{exe}, p, stdout, stderr, backend)
	if cause != CauseCancelled || !errors.Is(err, context.Canceled) {
		t.Fatalf("cause/error = %v/%v", cause, err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancelled suspended child ran: %v", statErr)
	}
}

func TestWindowsUnexpectedSuspendCountIsContainmentFailure(t *testing.T) {
	exe, _, p := helperPolicy(t, "exit")
	p = p.withDefaults()
	p.Cleanup = 300 * time.Millisecond
	backend := productionWindowsBackend()
	backend.resume = func(windows.Handle) (uint32, error) { return 2, nil }
	stdout := &machineWriter{limit: p.MachineLimit, onOverflow: func() {}}
	stderr := &excerptWriter{limit: p.DiagnosticLimit}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, cause, err := runWindows(ctx, exe, []string{exe}, p, stdout, stderr, backend)
	if cause != CauseContainment || err == nil || !strings.Contains(err.Error(), "suspend count") {
		t.Fatalf("cause/error = %v/%v", cause, err)
	}
	if elapsed := time.Since(start); elapsed > p.Cleanup+time.Second {
		t.Fatalf("unexpected suspend cleanup took %v", elapsed)
	}
}

func TestWindowsSuspendedEnrollmentFailuresNeverRun(t *testing.T) {
	for _, tc := range []struct {
		name    string
		backend windowsBackend
	}{
		{
			name: "assign",
			backend: windowsBackend{
				assign: func(windows.Handle, windows.Handle) error { return windows.ERROR_ACCESS_DENIED },
				resume: windows.ResumeThread,
			},
		},
		{
			name: "resume",
			backend: windowsBackend{
				assign: windows.AssignProcessToJobObject,
				resume: func(windows.Handle) (uint32, error) { return 0, windows.ERROR_ACCESS_DENIED },
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "marker")
			exe, _, p := helperPolicy(t, "mutate-then-fail", "PROCEXEC_HELPER_COUNTER="+marker)
			p = p.withDefaults()
			p.Cleanup = time.Second
			stdout := &machineWriter{limit: p.MachineLimit, onOverflow: func() {}}
			stderr := &excerptWriter{limit: p.DiagnosticLimit}
			started := make(chan uint32, 1)
			tc.backend.observeSuspended = func(pid uint32) { started <- pid }
			start := time.Now()
			_, cause, err := runWindows(context.Background(), exe, []string{exe}, p, stdout, stderr, tc.backend)
			if cause != CauseContainment || err == nil {
				t.Fatalf("cause/error = %v/%v", cause, err)
			}
			if time.Since(start) > p.Cleanup+time.Second {
				t.Fatalf("cleanup exceeded allowance: %v", time.Since(start))
			}
			if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("suspended child ran user code: %v", statErr)
			}
			pid := <-started
			if !waitWindowsDead(int(pid), p.Cleanup) {
				t.Fatalf("failed-enrollment child %d was not reaped", pid)
			}
		})
	}
}

func TestWindowsHandleListRestrictsInheritance(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "probe"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h, err := duplicateInheritable(windows.Handle(f.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	exe, args, p := helperPolicy(t, "handle-probe", "PROCEXEC_PROBE_HANDLE="+strconv.FormatUint(uint64(h), 10))
	if _, err := Run(context.Background(), exe, args, p); err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatal("an inheritable handle outside HANDLE_LIST reached the child")
	}
}

func TestWindowsCleanupBackendFailuresRequireVerifiedEmptiness(t *testing.T) {
	for _, tc := range []struct {
		name        string
		query       func(windows.Handle) (uint32, error)
		wantFailure bool
	}{
		{
			name: "terminate error resolved by emptiness",
			query: func() func(windows.Handle) (uint32, error) {
				calls := 0
				return func(windows.Handle) (uint32, error) {
					calls++
					if calls == 1 {
						return 1, nil
					}
					return 0, nil
				}
			}(),
		},
		{
			name: "accounting failure",
			query: func(windows.Handle) (uint32, error) {
				return 0, errors.New("deliberate accounting failure")
			},
			wantFailure: true,
		},
		{
			name: "members persist",
			query: func(windows.Handle) (uint32, error) {
				return 1, nil
			},
			wantFailure: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exe, _, p := helperPolicy(t, "exit")
			p = p.withDefaults()
			p.Cleanup = 100 * time.Millisecond
			backend := productionWindowsBackend()
			backend.queryActive = tc.query
			backend.terminateJob = func(windows.Handle, uint32) error {
				return windows.ERROR_ACCESS_DENIED
			}
			stdout := &machineWriter{limit: p.MachineLimit, onOverflow: func() {}}
			stderr := &excerptWriter{limit: p.DiagnosticLimit}
			out, cause, err := runWindows(context.Background(), exe, []string{exe}, p, stdout, stderr, backend)
			if err != nil || cause != CauseUnknown {
				t.Fatalf("launch cause/error = %v/%v", cause, err)
			}
			if tc.wantFailure && out.containErr == nil {
				t.Fatal("cleanup failure silently succeeded")
			}
			if !tc.wantFailure && out.containErr != nil {
				t.Fatalf("resolved terminate error remained fatal: %v", out.containErr)
			}
		})
	}
}

func TestWindowsNestedJobAndResourceStability(t *testing.T) {
	// The outer Run enrolls the helper in one job; nested-run calls Run again and
	// must enroll its own child in a second, nested job.
	exe, args, p := helperPolicy(t, "nested-run")
	if _, err := Run(context.Background(), exe, args, p); err != nil {
		t.Fatalf("nested job execution: %v", err)
	}
	for i := 0; i < 3; i++ {
		exe, args, p := helperPolicy(t, "exit")
		if _, err := Run(context.Background(), exe, args, p); err != nil {
			t.Fatal(err)
		}
	}
	beforeG := runtime.NumGoroutine()
	beforeHandles := processHandleCount(t)
	for i := 0; i < 20; i++ {
		exe, args, p := helperPolicy(t, "exit")
		if _, err := Run(context.Background(), exe, args, p); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		exe, _, p := helperPolicy(t, "exit")
		p = p.withDefaults()
		backend := productionWindowsBackend()
		backend.assign = func(windows.Handle, windows.Handle) error { return windows.ERROR_ACCESS_DENIED }
		stdout := &machineWriter{limit: p.MachineLimit, onOverflow: func() {}}
		stderr := &excerptWriter{limit: p.DiagnosticLimit}
		if _, cause, err := runWindows(context.Background(), exe, []string{exe}, p, stdout, stderr, backend); cause != CauseContainment || err == nil {
			t.Fatalf("fault launch = %v/%v", cause, err)
		}
	}
	settleDeadline := time.Now().Add(time.Second)
	for processHandleCount(t) > beforeHandles+8 && time.Now().Before(settleDeadline) {
		runtime.GC()
		runtime.Gosched()
	}
	if after := runtime.NumGoroutine(); after > beforeG+6 {
		t.Fatalf("goroutines grew from %d to %d", beforeG, after)
	}
	if after := processHandleCount(t); after > beforeHandles+8 {
		t.Fatalf("handles grew from %d to %d", beforeHandles, after)
	}
}

var getProcessHandleCount = syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcessHandleCount")

func processHandleCount(t *testing.T) uint32 {
	t.Helper()
	var count uint32
	ok, _, callErr := getProcessHandleCount.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&count)))
	if ok == 0 {
		t.Fatal(callErr)
	}
	return count
}
