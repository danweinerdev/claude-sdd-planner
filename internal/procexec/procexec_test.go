package procexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// FR-13: a hung command returns within timeout plus the cleanup allowance and
// classifies as a deadline, not as a generic failure.
func TestDeadlineIsOperational(t *testing.T) {
	exe, args, p := helperPolicy(t, "sleep")
	p.Timeout = 300 * time.Millisecond
	p.Cleanup = 500 * time.Millisecond
	start := time.Now()
	_, err := Run(context.Background(), exe, args, p)
	elapsed := time.Since(start)
	if !IsCause(err, CauseDeadline) {
		t.Fatalf("want CauseDeadline, got %v", err)
	}
	if limit := p.Timeout + p.Cleanup + 2*time.Second; elapsed > limit {
		t.Fatalf("returned after %v; policy bound is %v", elapsed, limit)
	}
	// A caller cancellation that is not the policy deadline is reported as
	// such, so a deadline is never confused with an interrupt.
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	exe, args, p = helperPolicy(t, "sleep")
	p.Cleanup = 500 * time.Millisecond
	_, err = Run(ctx, exe, args, p)
	if !IsCause(err, CauseCancelled) {
		t.Fatalf("want CauseCancelled, got %v", err)
	}
}

// FR-15: infinite machine output cannot hang or exhaust memory; overflow is an
// incomplete result, never a parsed success.
func TestOutputOverflow(t *testing.T) {
	exe, args, p := helperPolicy(t, "stdout-forever")
	p.MachineLimit = 1 << 20
	p.Timeout = 10 * time.Second
	p.Cleanup = 1 * time.Second
	start := time.Now()
	res, err := Run(context.Background(), exe, args, p)
	if !IsCause(err, CauseOverflow) {
		t.Fatalf("want CauseOverflow, got %v (stdout %d bytes)", err, len(res.Stdout))
	}
	if len(res.Stdout) != 0 {
		t.Fatalf("an overflowed run must not hand back partial machine output; got %d bytes", len(res.Stdout))
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("overflow took %v; must cancel promptly", elapsed)
	}
}

// FR-15 / DD-9: valid large machine output well above the diagnostic limit
// and below the machine limit arrives complete and byte-exact.
func TestLargeMachineOutput(t *testing.T) {
	const n = 4 << 20 // 4 MiB, 64x the diagnostic limit
	exe, args, p := helperPolicy(t, "stdout-bytes", "PROCEXEC_HELPER_N="+strconv.Itoa(n))
	res, err := Run(context.Background(), exe, args, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stdout) != n {
		t.Fatalf("stdout length %d, want %d", len(res.Stdout), n)
	}
	for i := 0; i < n; i += 4093 {
		if res.Stdout[i] != byte('a'+i%26) {
			t.Fatalf("stdout byte %d corrupted", i)
		}
	}
	if res.StderrTruncated {
		t.Fatal("stderr reported truncated with no stderr output")
	}
}

// FR-15: a bounded stderr reader keeps draining so the child never blocks on
// a full pipe; the excerpt is exactly the limit and flagged truncated.
func TestStderrDrain(t *testing.T) {
	const n = 1 << 20
	exe, args, p := helperPolicy(t, "stderr-bytes", "PROCEXEC_HELPER_N="+strconv.Itoa(n))
	p.DiagnosticLimit = 64 << 10
	p.Timeout = 10 * time.Second
	res, err := Run(context.Background(), exe, args, p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(res.Stdout)) != "done" {
		t.Fatalf("stdout %q, want done", res.Stdout)
	}
	if len(res.Stderr) != p.DiagnosticLimit {
		t.Fatalf("stderr excerpt %d bytes, want exactly %d", len(res.Stderr), p.DiagnosticLimit)
	}
	if !res.StderrTruncated {
		t.Fatal("stderr excerpt not flagged truncated")
	}
	if res.Stderr[0] != 'A' {
		t.Fatalf("excerpt must keep the head of the stream; starts with %q", res.Stderr[0])
	}
}

// DD-2: every failure path carries a distinct typed cause and the argv.
func TestTypedCauses(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "no-such-binary")
	_, err := Run(context.Background(), missing, nil, Policy{})
	if !IsCause(err, CauseUnavailable) {
		t.Fatalf("missing executable: want CauseUnavailable, got %v", err)
	}
	// A bare name that is not on the explicit child PATH is unavailable too;
	// the search uses the intended child path, not an ambient one.
	_, err = Run(context.Background(), "procexec-definitely-not-a-command", nil, Policy{Env: []string{"PATH=" + dir}})
	if !IsCause(err, CauseUnavailable) {
		t.Fatalf("bare name off PATH: want CauseUnavailable, got %v", err)
	}

	notExec := filepath.Join(dir, "not-executable")
	if runtime.GOOS == "windows" {
		notExec += ".exe"
	}
	if err := os.WriteFile(notExec, []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), notExec, nil, Policy{})
	if !IsCause(err, CauseAccess) {
		t.Fatalf("non-executable file: want CauseAccess, got %v", err)
	}

	exe, args, p := helperPolicy(t, "exit", "PROCEXEC_HELPER_CODE=3")
	_, err = Run(context.Background(), exe, args, p)
	var pe *Error
	if !errors.As(err, &pe) || pe.Cause != CauseExit || pe.ExitCode != 3 {
		t.Fatalf("exit 3: want CauseExit/3, got %v", err)
	}
	if !strings.Contains(pe.Stderr, "helper exiting with 3") {
		t.Fatalf("exit error must carry the stderr excerpt; got %q", pe.Stderr)
	}
	if len(pe.Argv) == 0 || pe.Argv[0] != exe {
		t.Fatalf("error argv %v must name the executable", pe.Argv)
	}
	if !strings.Contains(pe.Error(), "exit 3") {
		t.Fatalf("Error() %q must name the cause and code", pe.Error())
	}

	exe, args, p = helperPolicy(t, "exit", "PROCEXEC_HELPER_CODE=0")
	res, err := Run(context.Background(), exe, args, p)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("exit 0: want success, got %v / %+v", err, res)
	}
	if res.Run <= 0 {
		t.Fatal("success must report a positive run duration")
	}
}

// Contract clause added on node revision: Run never re-executes a command on
// its own. A mutating command that fails ambiguously (its write landed, but
// its own exit status is a failure) must be observed to have run exactly
// once — the counter file it appended to carries exactly one line.
func TestMutationNotRetried(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "counter")
	exe, args, p := helperPolicy(t, "mutate-then-fail",
		"PROCEXEC_HELPER_CODE=3", "PROCEXEC_HELPER_COUNTER="+counter)
	_, err := Run(context.Background(), exe, args, p)

	var pe *Error
	if !errors.As(err, &pe) || pe.Cause != CauseExit || pe.ExitCode != 3 {
		t.Fatalf("want CauseExit/3, got %v", err)
	}

	b, readErr := os.ReadFile(counter)
	if readErr != nil {
		t.Fatalf("counter file: %v", readErr)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 1 || lines[0] != "mutated" {
		t.Fatalf("counter file has %d line(s) (%q); want exactly 1 — the command must run exactly once", len(lines), b)
	}
}

// Review F-01: on a platform with no containment adapter the refusal must be
// legible as exactly that. The seam is the only way to reach the path from a
// supported build — GOOS cannot be flipped at runtime — but the assertion is
// about the error a Windows user would actually see: a containment cause
// carrying ErrNoContainmentAdapter, naming the platform and the adapter, and
// never reading as a missing executable.
func TestUnsupportedPlatformRefusalIsDistinct(t *testing.T) {
	restore := ContainmentProbe
	t.Cleanup(func() { ContainmentProbe = restore })
	ContainmentProbe = func() (bool, string) {
		return false, "windows: no process-containment adapter (the Windows Job Object adapter, Designs/TestSuiteReliability DD-3, is a follow-on plan)"
	}

	if ok, reason := ContainmentSupported(); ok || reason == "" {
		t.Fatalf("ContainmentSupported() = (%v, %q), want unsupported with a reason", ok, reason)
	}

	exe, args, p := helperPolicy(t, "exit")
	_, err := Run(context.Background(), exe, args, p)
	if !IsCause(err, CauseContainment) {
		t.Fatalf("Run on an uncontainable platform = %v, want CauseContainment", err)
	}
	if !errors.Is(err, ErrNoContainmentAdapter) {
		t.Fatalf("refusal %v does not wrap ErrNoContainmentAdapter", err)
	}
	msg := err.Error()
	for _, want := range []string{"windows", "adapter", "Job Object"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal %q does not name %q", msg, want)
		}
	}
	if strings.Contains(msg, "executable file not found") {
		t.Errorf("refusal reads as a missing executable: %q", msg)
	}
}
