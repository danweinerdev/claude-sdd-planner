package testgate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func makefile(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMakeGateRunsFreshTests(t *testing.T) {
	src := makefile(t)
	if !strings.Contains(src, "test: check-templates\n\t@go test -count=1 ./...\n") {
		t.Fatal("authoritative gate must retain template checks and run every Go package fresh, with no test filter")
	}
}

func TestMakeHostExecutableMatchesPlatform(t *testing.T) {
	src := makefile(t)
	for _, want := range []string{"SDD := $(BUILD_DIR)/$(HOST_TUPLE)-debug/sdd$(EXE_SUFFIX)", "SDD_RELEASE := $(BUILD_DIR)/$(HOST_TUPLE)-release/sdd$(EXE_SUFFIX)"} {
		if !strings.Contains(src, want) {
			t.Fatalf("host executable must carry its platform suffix: missing %q", want)
		}
	}
	if strings.Contains(src, "@rm -f $(SDD).exe") {
		t.Fatal("must not delete a potentially authoritative executable after building")
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Log("make absent: static contract checked; native smoke runs through make test on supported build hosts")
		return
	}
	for _, target := range []struct{ os, suffix string }{{"windows", ".exe"}, {"linux", ""}, {"darwin", ""}} {
		for _, goal := range []string{"test", "build-release"} {
			cmd := exec.Command(makePath, "-n", "GOOS="+target.os, "GOARCH=amd64", goal)
			cmd.Dir = filepath.Join("..", "..")
			// Recursive make overrides belong to the parent invocation, not this
			// independent platform probe (notably a caller's SDD= override).
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				switch strings.ToUpper(key) {
				case "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "MAKEOVERRIDES":
				default:
					cmd.Env = append(cmd.Env, entry)
				}
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("make dry run %s/%s: %v\n%s", target.os, goal, err, out)
			}
			variant := "debug"
			if goal == "build-release" {
				variant = "release"
			}
			binary := "build/" + target.os + "-amd64-" + variant + "/sdd" + target.suffix
			if !strings.Contains(string(out), "-o "+binary+" ./cmd/sdd") {
				t.Fatalf("wrong build destination:\n%s", out)
			}
			if goal == "test" && !strings.Contains(string(out), binary+" template --check") {
				t.Fatalf("gate does not execute freshly built platform binary:\n%s", out)
			}
		}
	}
}
