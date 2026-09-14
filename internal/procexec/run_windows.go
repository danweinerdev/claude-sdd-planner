//go:build windows

package procexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsBackend is deliberately passed per invocation. Tests can fault an
// individual launch without introducing mutable process-wide production seams.
type windowsBackend struct {
	assign           func(windows.Handle, windows.Handle) error
	resume           func(windows.Handle) (uint32, error)
	terminateJob     func(windows.Handle, uint32) error
	queryActive      func(windows.Handle) (uint32, error)
	observeSuspended func(uint32)
}

func productionWindowsBackend() windowsBackend {
	return windowsBackend{
		assign:       windows.AssignProcessToJobObject,
		resume:       windows.ResumeThread,
		terminateJob: windows.TerminateJobObject,
		queryActive:  activeJobProcesses,
	}
}

type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	PageFaultCount            uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func runPlatform(ctx context.Context, path string, argv []string, p Policy, stdout *machineWriter, stderr *excerptWriter) (platformOutcome, Cause, error) {
	return runWindows(ctx, path, argv, p, stdout, stderr, productionWindowsBackend())
}

func runWindows(ctx context.Context, path string, argv []string, p Policy, stdout *machineWriter, stderr *excerptWriter, backend windowsBackend) (out platformOutcome, cause Cause, retErr error) {
	if err := ctx.Err(); err != nil {
		return platformOutcome{}, CauseCancelled, err
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return out, CauseContainment, fmt.Errorf("containment: create job: %w", err)
	}
	jobOpen := true
	defer func() {
		if jobOpen {
			windows.CloseHandle(job)
		}
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return out, CauseContainment, fmt.Errorf("containment: set kill-on-close: %w", err)
	}

	stdoutR, stdoutChild, err := makeChildPipe()
	if err != nil {
		return out, CauseAccess, fmt.Errorf("stdout pipe: %w", err)
	}
	defer stdoutR.Close()
	defer func() {
		if stdoutChild != 0 {
			windows.CloseHandle(stdoutChild)
		}
	}()
	stderrR, stderrChild, err := makeChildPipe()
	if err != nil {
		return out, CauseAccess, fmt.Errorf("stderr pipe: %w", err)
	}
	defer stderrR.Close()
	defer func() {
		if stderrChild != 0 {
			windows.CloseHandle(stderrChild)
		}
	}()
	nul, err := os.Open("NUL")
	if err != nil {
		return out, CauseAccess, fmt.Errorf("stdin NUL: %w", err)
	}
	defer nul.Close()
	stdinChild, err := duplicateInheritable(windows.Handle(nul.Fd()))
	if err != nil {
		return out, CauseAccess, fmt.Errorf("stdin handle: %w", err)
	}
	defer func() {
		if stdinChild != 0 {
			windows.CloseHandle(stdinChild)
		}
	}()

	handles := []windows.Handle{stdinChild, stdoutChild, stderrChild}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return out, CauseAccess, fmt.Errorf("attribute list: %w", err)
	}
	defer attrs.Delete()
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return out, CauseAccess, fmt.Errorf("handle list: %w", err)
	}
	si := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  stdinChild,
			StdOutput: stdoutChild,
			StdErr:    stderrChild,
		},
		ProcThreadAttributeList: attrs.List(),
	}
	app, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return out, CauseAccess, err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return out, CauseAccess, err
	}
	dir, err := optionalUTF16Ptr(p.Dir)
	if err != nil {
		return out, CauseAccess, err
	}
	env, err := windowsEnvBlock(p.Env)
	if err != nil {
		return out, CauseAccess, err
	}
	var envp *uint16
	if env != nil {
		envp = &env[0]
	}

	pi := windows.ProcessInformation{}
	out.start = time.Now()
	// A fresh console process group prevents inherited CTRL_C delivery from the
	// parent's console. This is not a Job Object breakaway flag: ownership still
	// comes exclusively from suspended creation followed by job enrollment.
	err = windows.CreateProcess(app, cmdline, nil, nil, true,
		windows.CREATE_SUSPENDED|windows.CREATE_NEW_PROCESS_GROUP|windows.CREATE_UNICODE_ENVIRONMENT|windows.EXTENDED_STARTUPINFO_PRESENT,
		envp, dir, &si.StartupInfo, &pi)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(attrs)
	runtime.KeepAlive(env)
	if err != nil {
		// Resolution already proved that the named file exists. CreateProcess
		// file/path errors here therefore mean an unusable image or dependency,
		// not an unavailable command.
		return out, CauseAccess, err
	}
	processOpen, threadOpen := true, true
	defer func() {
		if threadOpen {
			windows.CloseHandle(pi.Thread)
		}
		if processOpen {
			windows.CloseHandle(pi.Process)
		}
	}()
	if backend.observeSuspended != nil {
		backend.observeSuspended(pi.ProcessId)
	}
	stopSuspended := func() error {
		var cleanupErr error
		termErr := windows.TerminateProcess(pi.Process, 253)
		state, waitErr := windows.WaitForSingleObject(pi.Process, durationMillis(p.Cleanup))
		if termErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("terminate suspended process: %w", termErr))
		}
		if waitErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("reap suspended process: %w", waitErr))
		} else if state != windows.WAIT_OBJECT_0 {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("suspended process survived cleanup allowance %v", p.Cleanup))
		}
		return cleanupErr
	}
	failSuspended := func(stage string, stageErr error) (platformOutcome, Cause, error) {
		return out, CauseContainment, fmt.Errorf("containment: %s suspended process: %w", stage, errors.Join(stageErr, stopSuspended()))
	}
	if err := nul.Close(); err != nil {
		return failSuspended("close owned parent stdin source", err)
	}
	if err := backend.assign(job, pi.Process); err != nil {
		return failSuspended("assign", err)
	}
	if err := ctx.Err(); err != nil {
		if cleanupErr := stopSuspended(); cleanupErr != nil {
			return out, CauseContainment, fmt.Errorf("containment: cancel before resume cleanup: %w", cleanupErr)
		}
		return out, CauseCancelled, err
	}
	previous, err := backend.resume(pi.Thread)
	if err != nil {
		return failSuspended("resume", err)
	}
	if previous != 1 {
		return failSuspended("resume", fmt.Errorf("unexpected primary-thread suspend count %d, want 1", previous))
	}
	if err := windows.CloseHandle(pi.Thread); err != nil {
		out.containErr = errors.Join(out.containErr, fmt.Errorf("containment: close owned primary thread handle: %w", err))
	} else {
		threadOpen = false
	}
	if observeStart != nil {
		observeStart(int(pi.ProcessId))
	}
	// The child copies no longer need to remain open in the parent. The pipe
	// readers below are the only parent-side stream handles.
	if err := windows.CloseHandle(stdinChild); err != nil {
		out.containErr = errors.Join(out.containErr, fmt.Errorf("containment: close owned child stdin handle: %w", err))
	} else {
		stdinChild = 0
	}
	if err := windows.CloseHandle(stdoutChild); err != nil {
		out.containErr = errors.Join(out.containErr, fmt.Errorf("containment: close owned child stdout handle: %w", err))
	} else {
		stdoutChild = 0
	}
	if err := windows.CloseHandle(stderrChild); err != nil {
		out.containErr = errors.Join(out.containErr, fmt.Errorf("containment: close owned child stderr handle: %w", err))
	} else {
		stderrChild = 0
	}

	stdoutDone := make(chan error, 1)
	stderrDone := make(chan error, 1)
	go func() { _, e := io.Copy(stdout, stdoutR); stdoutDone <- e }()
	go func() { _, e := io.Copy(stderr, stderrR); stderrDone <- e }()

	cancelled := false
	var terminateErr error
	var cleanupDeadline time.Time
	for {
		state, waitErr := windows.WaitForSingleObject(pi.Process, 10)
		if waitErr != nil {
			out.containErr = errors.Join(out.containErr, fmt.Errorf("containment: wait for leader: %w", waitErr))
			break
		}
		if state == windows.WAIT_OBJECT_0 {
			break
		}
		if ctx.Err() != nil && !cancelled {
			cancelled = true
			cleanupDeadline = time.Now().Add(p.Cleanup)
			if err := backend.terminateJob(job, 254); err != nil {
				terminateErr = fmt.Errorf("containment: terminate cancelled owned job: %w", err)
			}
		}
		if cancelled && time.Now().After(cleanupDeadline) {
			if out.containErr == nil {
				out.containErr = fmt.Errorf("containment: leader survived cancellation cleanup allowance %v", p.Cleanup)
			}
			break
		}
	}

	if cleanupDeadline.IsZero() {
		cleanupDeadline = time.Now().Add(p.Cleanup)
	}
	active, queryErr := backend.queryActive(job)
	if queryErr != nil && out.containErr == nil {
		out.containErr = queryErr
	}
	if active > 0 {
		out.cleaned = true
		if err := backend.terminateJob(job, 254); err != nil {
			terminateErr = errors.Join(terminateErr, fmt.Errorf("containment: terminate remaining owned job members: %w", err))
		}
	}
	observedEmpty := queryErr == nil && active == 0
	for time.Now().Before(cleanupDeadline) {
		if observedEmpty {
			break
		}
		active, err = backend.queryActive(job)
		if err != nil {
			if out.containErr == nil {
				out.containErr = err
			}
			break
		}
		if active == 0 {
			observedEmpty = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !observedEmpty {
		out.containErr = errors.Join(out.containErr, terminateErr, fmt.Errorf("containment: owned job still has %d active processes after %v; emptiness was not verified", active, p.Cleanup))
	}

	var exitCode uint32
	if err := windows.GetExitCodeProcess(pi.Process, &exitCode); err != nil && out.containErr == nil {
		out.containErr = fmt.Errorf("containment: read exit code: %w", err)
	}
	out.exitCode = int(exitCode)
	// Closing the job is safe only after the cancellation path is finished and
	// the accounting query observed zero active members.
	jobState := "verified-empty"
	if !observedEmpty {
		jobState = "unverified; closing to enforce kill-on-close"
	}
	if err := windows.CloseHandle(job); err != nil {
		out.containErr = errors.Join(out.containErr, fmt.Errorf("containment: close %s owned job handle: %w", jobState, err))
	} else {
		jobOpen = false
	}
	if err := windows.CloseHandle(pi.Process); err != nil {
		out.containErr = errors.Join(out.containErr, fmt.Errorf("containment: close owned leader process handle: %w", err))
	} else {
		processOpen = false
	}

	out.drainErr = joinStreams(cleanupDeadline, stdoutR, stderrR, stdoutDone, stderrDone)
	out.end = time.Now()
	return out, CauseUnknown, nil
}

func duplicateInheritable(source windows.Handle) (windows.Handle, error) {
	current := windows.CurrentProcess()
	var duplicate windows.Handle
	err := windows.DuplicateHandle(current, source, current, &duplicate, 0, true, windows.DUPLICATE_SAME_ACCESS)
	return duplicate, err
}

func makeChildPipe() (*os.File, windows.Handle, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, 0, err
	}
	child, err := duplicateInheritable(windows.Handle(w.Fd()))
	if err != nil {
		_ = w.Close()
		_ = r.Close()
		return nil, 0, err
	}
	if err := w.Close(); err != nil {
		_ = windows.CloseHandle(child)
		_ = r.Close()
		return nil, 0, fmt.Errorf("close owned parent pipe writer: %w", err)
	}
	return r, child, nil
}

func optionalUTF16Ptr(s string) (*uint16, error) {
	if s == "" {
		return nil, nil
	}
	return windows.UTF16PtrFromString(s)
}

func windowsEnvBlock(env []string) ([]uint16, error) {
	if env == nil {
		return nil, nil
	}
	for _, value := range env {
		if strings.IndexByte(value, 0) >= 0 {
			return nil, errors.New("environment contains NUL")
		}
	}
	// Cmd.Environ supplies os/exec's Windows contract without launching:
	// case-insensitive last-wins deduplication and required SYSTEMROOT
	// augmentation. Env is copied by Environ and remains caller-owned.
	cmd := &exec.Cmd{Env: env}
	values := cmd.Environ()
	envKey := func(value string) string {
		i := strings.IndexByte(value, '=')
		if i == 0 {
			i = strings.IndexByte(value[1:], '=') + 1
		}
		if i < 0 {
			return ""
		}
		return strings.ToUpper(value[:i])
	}
	sort.SliceStable(values, func(i, j int) bool { return envKey(values[i]) < envKey(values[j]) })
	block := make([]uint16, 0)
	for _, value := range values {
		encoded, err := windows.UTF16FromString(value)
		if err != nil {
			return nil, fmt.Errorf("environment contains NUL: %w", err)
		}
		block = append(block, encoded...)
	}
	block = append(block, 0)
	if len(values) == 0 {
		block = append(block, 0)
	}
	return block, nil
}

func activeJobProcesses(job windows.Handle) (uint32, error) {
	var info jobAccounting
	if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
		return 0, fmt.Errorf("containment: query job accounting: %w", err)
	}
	return info.ActiveProcesses, nil
}

func durationMillis(d time.Duration) uint32 {
	ms := d.Milliseconds()
	if ms < 1 {
		return 1
	}
	if ms > int64(^uint32(0)-1) {
		return ^uint32(0) - 1
	}
	return uint32(ms)
}

func joinStreams(deadline time.Time, stdoutR, stderrR *os.File, stdoutDone, stderrDone <-chan error) error {
	var stdoutErr, stderrErr error
	stdoutPending, stderrPending := true, true
	remaining := time.Until(deadline)
	if remaining <= 0 {
		_ = stdoutR.Close()
		_ = stderrR.Close()
		stdoutErr := <-stdoutDone
		stderrErr := <-stderrDone
		return errors.Join(errors.New("stream drain exceeded shared cleanup allowance; output is incomplete"), stdoutErr, stderrErr)
	}
	timer := time.NewTimer(remaining)
	timedOut := false
	for stdoutPending || stderrPending {
		select {
		case stdoutErr = <-stdoutDone:
			stdoutPending = false
			stdoutDone = nil
		case stderrErr = <-stderrDone:
			stderrPending = false
			stderrDone = nil
		case <-timer.C:
			timedOut = true
			_ = stdoutR.Close()
			_ = stderrR.Close()
			if stdoutPending {
				stdoutErr = <-stdoutDone
				stdoutPending = false
			}
			if stderrPending {
				stderrErr = <-stderrDone
				stderrPending = false
			}
		}
	}
	if !timer.Stop() && !timedOut {
		select {
		case <-timer.C:
		default:
		}
	}
	closeErr := errors.Join(stdoutR.Close(), stderrR.Close())
	if timedOut {
		return errors.Join(errors.New("stream drain exceeded shared cleanup allowance; output is incomplete"), stdoutErr, stderrErr, closeErr)
	}
	if stdoutErr != nil || stderrErr != nil || closeErr != nil {
		return errors.Join(fmt.Errorf("stream drain failed; output is incomplete"), stdoutErr, stderrErr, closeErr)
	}
	return nil
}
