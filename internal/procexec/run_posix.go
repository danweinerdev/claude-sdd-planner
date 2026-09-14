//go:build unix

package procexec

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

func runPlatform(ctx context.Context, path string, argv []string, p Policy, stdout *machineWriter, stderr *excerptWriter) (platformOutcome, Cause, error) {
	cmd := exec.CommandContext(ctx, path, argv[1:]...)
	cmd.Args = argv
	cmd.Dir = p.Dir
	cmd.Env = p.Env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = p.Cleanup
	if err := configureContainment(cmd); err != nil {
		return platformOutcome{}, CauseContainment, err
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return platformOutcome{}, CauseUnavailable, err
		}
		return platformOutcome{}, CauseAccess, err
	}
	if observeStart != nil {
		observeStart(cmd.Process.Pid)
	}
	cleaned, swept, _ := sweepGroupBeforeReap(cmd)
	waitErr := cmd.Wait()
	postCleaned, postErr := cleanupGroup(cmd, p.Cleanup, swept)
	return platformOutcome{
		start:      start,
		end:        time.Now(),
		waitErr:    waitErr,
		exitCode:   cmd.ProcessState.ExitCode(),
		cleaned:    cleaned || postCleaned,
		containErr: postErr,
	}, CauseUnknown, nil
}
