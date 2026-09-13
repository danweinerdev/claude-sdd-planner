package procexec

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

// helperEnv selects a cooperative helper behavior when the test binary is
// re-executed as a child. TestMain intercepts it before any test runs, so the
// helper never touches the Go test framework.
const helperEnv = "PROCEXEC_HELPER_MODE"

func TestMain(m *testing.M) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		os.Exit(m.Run())
	}
	os.Exit(runHelper(mode))
}

// runHelper implements the child behaviors. Arguments follow os.Args[1:] but
// the test binary's own flags never reach here because the parent passes
// only helper arguments.
func runHelper(mode string) int {
	switch mode {
	case "sleep":
		time.Sleep(time.Hour)
		return 0
	case "exit":
		code, _ := strconv.Atoi(os.Getenv("PROCEXEC_HELPER_CODE"))
		fmt.Fprintln(os.Stderr, "helper exiting with", code)
		return code
	case "stdout-forever":
		w := bufio.NewWriterSize(os.Stdout, 1<<16)
		line := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n")
		for {
			if _, err := w.Write(line); err != nil {
				return 0
			}
		}
	case "stdout-bytes":
		n, _ := strconv.Atoi(os.Getenv("PROCEXEC_HELPER_N"))
		w := bufio.NewWriterSize(os.Stdout, 1<<16)
		for i := 0; i < n; i++ {
			w.WriteByte(byte('a' + i%26))
		}
		w.Flush()
		return 0
	case "stderr-bytes":
		n, _ := strconv.Atoi(os.Getenv("PROCEXEC_HELPER_N"))
		w := bufio.NewWriterSize(os.Stderr, 1<<16)
		for i := 0; i < n; i++ {
			w.WriteByte(byte('A' + i%26))
		}
		w.Flush()
		fmt.Fprintln(os.Stdout, "done")
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown helper mode", mode)
	return 99
}

// helperPolicy returns a policy whose child runs this test binary in the
// named helper mode with an explicit, minimal environment.
func helperPolicy(t *testing.T, mode string, extra ...string) (string, []string, Policy) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append([]string{helperEnv + "=" + mode, "PATH=" + os.Getenv("PATH")}, extra...)
	return exe, nil, Policy{Env: env}
}
