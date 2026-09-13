package provision

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
)

// TestProvisionUsesOwnedRunner pins the package's child-process discipline:
// doctor reaches this package on every run, so a fork that escapes the
// bounded runner is an uncontained, unbounded child on a user-facing path
// (review 06-review-fixtures-facd924-b F-01).
func TestProvisionUsesOwnedRunner(t *testing.T) {
	t.Run("no direct os/exec", func(t *testing.T) {
		fset := token.NewFileSet()
		pkgFiles, err := filepath.Glob("*.go")
		if err != nil {
			t.Fatalf("globbing package files: %v", err)
		}
		checked := 0
		for _, name := range pkgFiles {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", name, err)
			}
			checked++
			for _, imp := range file.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatalf("%s: unquoting import %s: %v", name, imp.Path.Value, err)
				}
				if path == "os/exec" {
					t.Errorf("%s:%d imports os/exec; every child process in this package must go through procexec.Run under a policy",
						name, fset.Position(imp.Pos()).Line)
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok || ident.Name != "exec" {
					return true
				}
				t.Errorf("%s:%d calls exec.%s; use procexec.Run under a policy instead",
					name, fset.Position(call.Pos()).Line, sel.Sel.Name)
				return true
			})
		}
		if checked == 0 {
			t.Fatal("no non-test package files were parsed; the static check proved nothing")
		}
	})

	t.Run("hanging git is bounded on the doctor path", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the fake git stub is a POSIX shell script")
		}
		dir := t.TempDir()
		stub := filepath.Join(dir, "bin")
		if err := os.MkdirAll(stub, 0o755); err != nil {
			t.Fatalf("creating stub dir: %v", err)
		}
		// The stub must hang using only shell builtins: PATH is replaced with
		// the stub directory alone, so an external `sleep` would not resolve
		// and the child would exit 127 instead of hanging, proving nothing
		// about boundedness. A read from a fifo nobody writes blocks in the
		// kernel without burning CPU.
		fifo, err := blockingReadSource(filepath.Join(dir, "block.fifo"))
		if err != nil {
			t.Fatalf("creating blocking read source: %v", err)
		}
		if err := os.WriteFile(filepath.Join(stub, "git"), []byte("#!/bin/sh\nread -r _ < "+fifo+"\n"), 0o755); err != nil {
			t.Fatalf("writing fake git: %v", err)
		}
		t.Setenv("PATH", stub)

		policy := gitPolicy
		t.Cleanup(func() { gitPolicy = policy })
		gitPolicy = procexec.Policy{Timeout: 300 * time.Millisecond, Cleanup: 500 * time.Millisecond}

		type outcome struct{ err error }
		done := make(chan outcome, 1)
		go func() {
			_, err := CheckPostRewrite(dir)
			done <- outcome{err}
		}()

		bound := gitPolicy.Timeout + gitPolicy.Cleanup + 3*time.Second
		select {
		case got := <-done:
			if got.err == nil {
				t.Fatalf("CheckPostRewrite succeeded against a hanging git; expected a bounded failure")
			}
			var pe *procexec.Error
			if !errors.As(got.err, &pe) {
				t.Fatalf("error does not wrap a procexec cause: %v", got.err)
			}
			if pe.Cause != procexec.CauseDeadline {
				t.Fatalf("cause = %v, want %v (error: %v)", pe.Cause, procexec.CauseDeadline, got.err)
			}
		case <-time.After(bound):
			t.Fatalf("CheckPostRewrite did not return within %v against a hanging git", bound)
		}
	})
}

// TestGitOutputRendersStderrOnce pins the diagnostic shape of a failed git.
// procexec.Error.Error() already renders the stderr excerpt, so appending it
// again printed the fatal line twice in every doctor report that hit a
// non-repository (review 06-review-fixtures-63da43f F-01).
func TestGitOutputRendersStderrOnce(t *testing.T) {
	if _, err := procexec.LookPath("git", nil); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	dir := t.TempDir()
	_, err := gitOutput(dir, "rev-parse", "--is-bare-repository")
	if err == nil {
		t.Fatal("gitOutput succeeded in a non-repository; expected a failure to inspect")
	}
	const phrase = "not a git repository"
	if n := strings.Count(strings.ToLower(err.Error()), phrase); n != 1 {
		t.Fatalf("%q occurs %d times in the error, want exactly 1:\n%s", phrase, n, err)
	}
}
