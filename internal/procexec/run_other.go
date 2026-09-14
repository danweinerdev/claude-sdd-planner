//go:build !unix && !windows

package procexec

import "context"

func runPlatform(context.Context, string, []string, Policy, *machineWriter, *excerptWriter) (platformOutcome, Cause, error) {
	return platformOutcome{}, CauseContainment, errNoAdapter()
}
