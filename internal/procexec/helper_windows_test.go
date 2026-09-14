//go:build windows

package procexec

import (
	"context"
	"os"
	"strconv"

	"golang.org/x/sys/windows"
)

func runHandleProbe() int {
	v, err := strconv.ParseUint(os.Getenv("PROCEXEC_PROBE_HANDLE"), 10, 64)
	if err != nil {
		return 92
	}
	data := []byte("inherited")
	var written uint32
	if err := windows.WriteFile(windows.Handle(v), data, &written, nil); err == nil {
		return 91
	}
	return 0
}

func runNestedProbe() int {
	exe, err := os.Executable()
	if err != nil {
		return 94
	}
	_, err = Run(context.Background(), exe, nil, Policy{Env: []string{helperEnv + "=exit", "PATH=" + os.Getenv("PATH")}})
	if err != nil {
		return 93
	}
	return 0
}
