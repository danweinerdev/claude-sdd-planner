package testgate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makefile(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func makeGateRunsFreshTests(src string) bool {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	return strings.Contains(src, "test: check-templates\n\t@go test -count=1 ./...\n")
}

func gnuMakePath(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"make", "mingw32-make", "gmake"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
		timedOut := ctx.Err() == context.DeadlineExceeded
		cancel()
		if timedOut {
			t.Logf("%s --version timed out", name)
			continue
		}
		if err == nil && strings.Contains(string(out), "GNU Make") {
			return path
		}
		t.Logf("%s is not a compatible GNU make: %v", name, err)
	}
	t.Skip("GNU make unavailable or incompatible: static checks passed; dynamic command-expansion probes skipped")
	return ""
}

func TestMakeGateRunsFreshTests(t *testing.T) {
	src := makefile(t)
	if !makeGateRunsFreshTests(src) {
		t.Fatal("authoritative gate must retain template checks and run every Go package fresh, with no test filter")
	}
}

func TestMakeGateHandlesCheckoutLineEndings(t *testing.T) {
	lf := "test: check-templates\n\t@go test -count=1 ./...\n"
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")

	lfPasses := makeGateRunsFreshTests(lf)
	crlfPasses := makeGateRunsFreshTests(crlf)
	if lfPasses != crlfPasses {
		t.Fatalf("same valid make gate differs by checkout line endings: LF=%t CRLF=%t", lfPasses, crlfPasses)
	}
	if !lfPasses {
		t.Fatal("valid make gate must retain template checks and run every Go package fresh")
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
	makePath := gnuMakePath(t)
	// GNU make -n checks command expansion only. It does not execute the
	// cross-platform binaries or prove that they run on their native platforms.
	// Keep probes to non-recursive build/test goals: bump and recursive targets
	// can execute commands even under -n and are deliberately out of scope.
	for _, target := range []struct{ os, suffix string }{{"windows", ".exe"}, {"linux", ""}, {"darwin", ""}} {
		for _, goal := range []string{"test", "build-release"} {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			cmd := exec.CommandContext(ctx, makePath, "-n", "GOOS="+target.os, "GOARCH=amd64", goal)
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
			timedOut := ctx.Err() == context.DeadlineExceeded
			cancel()
			if timedOut {
				t.Fatalf("make dry run %s/%s timed out after 15s\n%s", target.os, goal, out)
			}
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
