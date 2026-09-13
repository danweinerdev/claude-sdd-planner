package procexec

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Result is a complete, successful command result.
type Result struct {
	Stdout          []byte // complete machine output (never truncated on success)
	Stderr          string // bounded diagnostic excerpt
	StderrTruncated bool
	ExitCode        int
	Run             time.Duration // start to exit
	Cleanup         time.Duration // cancel/exit to drained and reaped (0 when nothing was pending)
	// DescendantsCleaned reports that owned descendants outlived the command
	// and were cleaned by the containment adapter.
	DescendantsCleaned bool
}

// Run executes name with args (never through a shell) under the policy and
// returns either a complete Result or a typed *Error. The executable is
// resolved before launch against the child's PATH (the policy environment
// when explicit, the process environment otherwise). Mutations are never
// retried: a failure is reported once with its cause and evidence.
func Run(ctx context.Context, name string, args []string, p Policy) (Result, error) {
	p = p.withDefaults()
	argv := append([]string{name}, args...)
	// A platform with no containment adapter refuses before anything else, so
	// the user is told what is actually wrong instead of learning that some
	// executable could not be found (review F-01).
	if ok, _ := ContainmentSupported(); !ok {
		return Result{}, &Error{Cause: CauseContainment, Argv: argv, Err: errNoAdapter()}
	}
	var stdout *machineWriter
	fail := func(c Cause, err error, stderr *excerptWriter) (Result, error) {
		e := &Error{Cause: c, Argv: argv, Err: err}
		if stderr != nil {
			e.Stderr, e.Truncated = stderr.excerpt()
		}
		if stdout != nil {
			if b := stdout.bytes(); len(b) > 0 {
				if len(b) > p.DiagnosticLimit {
					b = b[:p.DiagnosticLimit]
				}
				e.Stdout = string(b)
			}
		}
		return Result{}, e
	}

	path, err := lookPath(name, p.Env)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) || errors.Is(err, errNotExecutable) {
			return fail(CauseAccess, err, nil)
		}
		return fail(CauseUnavailable, err, nil)
	}

	runCtx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	var cancelledAt atomicTime
	stderr := &excerptWriter{limit: p.DiagnosticLimit}
	stdout = &machineWriter{limit: p.MachineLimit, onOverflow: func() {
		cancelledAt.mark(time.Now())
		cancel()
	}}

	cmd := exec.CommandContext(runCtx, path, args...)
	cmd.Args = argv
	cmd.Dir = p.Dir
	cmd.Env = p.Env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// WaitDelay bounds the drain of pipes a descendant may still hold, so
	// Wait can never block forever; the containment adapter owns the
	// descendants themselves (DD-4) and makes cancellation kill the group.
	cmd.WaitDelay = p.Cleanup
	if err := configureContainment(cmd); err != nil {
		return fail(CauseContainment, err, nil)
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fail(CauseUnavailable, err, nil)
		}
		return fail(CauseAccess, err, nil)
	}
	if observeStart != nil {
		observeStart(cmd.Process.Pid)
	}
	// Owned descendants are swept before the leader is reaped: while the
	// leader is a zombie its pid is still its own, so signalling -pgid cannot
	// reach a group the kernel handed that pid to after a reap (review F-02).
	cleaned, swept, containErr := sweepGroupBeforeReap(cmd)
	waitErr := cmd.Wait()
	// After the reap the group is only polled for emptiness, never signalled
	// again; when the sweep could not run the fallback still probes and kills
	// here, within the cleanup allowance. A failure to clean up is reported
	// ahead of the command's own result.
	postCleaned, postErr := cleanupGroup(cmd, p.Cleanup, swept)
	if containErr == nil {
		containErr = postErr
	}
	cleaned = cleaned || postCleaned
	end := time.Now()

	run := end.Sub(start)
	var cleanup time.Duration
	if t, ok := cancelledAt.get(); ok {
		cleanup = end.Sub(t)
		run = t.Sub(start)
	} else if runCtx.Err() != nil {
		// The deadline or the caller fired; Wait returned after the kill.
		if dl, ok := runCtx.Deadline(); ok && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			cleanup = end.Sub(dl)
			run = dl.Sub(start)
		}
	}

	switch {
	case containErr != nil:
		return fail(CauseContainment, containErr, stderr)
	case stdout.overflowed():
		return fail(CauseOverflow, errMachineOverflow, stderr)
	case ctx.Err() != nil:
		return fail(CauseCancelled, ctx.Err(), stderr)
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return fail(CauseDeadline, runCtx.Err(), stderr)
	case waitErr == nil:
	case errors.Is(waitErr, exec.ErrWaitDelay):
		// The command itself exited successfully; only inherited pipes were
		// still open. They belonged to descendants the adapter has now
		// cleaned (cleaned == true) — a success with the cleanup recorded.
		// If nothing was left to clean, the pipe holder escaped ownership.
		if !cleaned {
			return fail(CauseDrain, waitErr, stderr)
		}
	default:
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			e := &Error{Cause: CauseExit, Argv: argv, ExitCode: exitErr.ExitCode(), Err: waitErr}
			e.Stderr, e.Truncated = stderr.excerpt()
			return Result{}, e
		}
		return fail(CauseAccess, waitErr, stderr)
	}

	res := Result{Stdout: stdout.bytes(), ExitCode: 0, Run: run, Cleanup: cleanup, DescendantsCleaned: cleaned}
	res.Stderr, res.StderrTruncated = stderr.excerpt()
	return res, nil
}

// observeStart lets a test learn the leader pid the instant the command is
// started; production never sets it.
var observeStart func(pid int)

var (
	errMachineOverflow = errors.New("machine output exceeded the policy limit; result incomplete")
	errNotExecutable   = errors.New("file is not executable")
)

// LookPath resolves an executable name exactly as Run would, so a caller that
// needs the resolved path (to report which binary it will run) gets the same
// answer the runner will use, without reaching for os/exec itself.
func LookPath(name string, env []string) (string, error) { return lookPath(name, env) }

// lookPath resolves name the way the child would see it. A name containing a
// path separator is checked directly; a bare name is searched on the child's
// PATH. Go's own LookPath is used when the child inherits the environment so
// its executable-path security behavior (ErrDot) is retained.
func lookPath(name string, env []string) (string, error) {
	if env == nil {
		return exec.LookPath(name)
	}
	if strings.ContainsRune(name, os.PathSeparator) || (runtime.GOOS == "windows" && strings.ContainsRune(name, '/')) {
		return name, checkExecutable(name)
	}
	var pathVar string
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") {
			pathVar = v
		}
	}
	var lastErr error = exec.ErrNotFound
	for _, dir := range filepath.SplitList(pathVar) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if runtime.GOOS == "windows" && filepath.Ext(candidate) == "" {
			candidate += ".exe"
		}
		err := checkExecutable(candidate)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			lastErr = err
		}
	}
	return "", &exec.Error{Name: name, Err: lastErr}
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return &exec.Error{Name: path, Err: errNotExecutable}
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return &exec.Error{Name: path, Err: errNotExecutable}
	}
	return nil
}

func asError(err error, target **Error) bool { return errors.As(err, target) }

// machineWriter collects the machine stream up to a finite limit. Exceeding
// it flags overflow, asks the runner to cancel, and stops accepting bytes;
// exec's copier then closes the pipe so the child cannot block on it.
type machineWriter struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	limit      int64
	overflow   bool
	onOverflow func()
}

func (w *machineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.overflow {
		return 0, errMachineOverflow
	}
	if int64(w.buf.Len())+int64(len(p)) > w.limit {
		w.overflow = true
		w.buf.Reset()
		w.mu.Unlock()
		w.onOverflow()
		w.mu.Lock()
		return 0, errMachineOverflow
	}
	return w.buf.Write(p)
}

func (w *machineWriter) overflowed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.overflow
}

func (w *machineWriter) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

// excerptWriter retains the head of a diagnostic stream up to a limit and
// keeps draining everything after it, so a chatty child never blocks on a
// full pipe and the caller still learns the stream was cut.
type excerptWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *excerptWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	room := w.limit - w.buf.Len()
	if room >= len(p) {
		w.buf.Write(p)
		return len(p), nil
	}
	if room > 0 {
		w.buf.Write(p[:room])
	}
	w.truncated = true
	return len(p), nil
}

func (w *excerptWriter) excerpt() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String(), w.truncated
}

type atomicTime struct {
	mu  sync.Mutex
	t   time.Time
	set bool
}

func (a *atomicTime) mark(t time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.set {
		a.t, a.set = t, true
	}
}

func (a *atomicTime) get() (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.t, a.set
}
