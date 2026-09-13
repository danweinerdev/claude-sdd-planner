package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// gitSpawnPattern matches the ways a *_test.go file in this module launches
// a real git process: exec.Command("git", ...), exec.CommandContext(ctx,
// "git", ...), procexec.Run(ctx, "git", ...), procexec.LookPath("git", ...),
// and exec.LookPath("git") — resolving the git binary is only ever followed
// by spawning it via the resolved path (e.g. exec.Command(gitExe, ...)) in
// this module's tests, so it counts as a spawn site too.
var gitSpawnPattern = regexp.MustCompile(
	`exec\.Command\(\s*"git"` +
		`|exec\.CommandContext\([^,]*,\s*"git"` +
		`|procexec\.Run\([^,]*,\s*"git"` +
		`|procexec\.LookPath\(\s*"git"` +
		`|exec\.LookPath\(\s*"git"`,
)

// selfFile is this test file's own base name. Its doc comment names the
// git-spawn patterns as text, which would otherwise match the scan and
// spuriously flag the testenv package as a git-spawning, TestMain-less
// package (it neither spawns git nor needs the policy on its own account).
const selfFile = "adoption_test.go"

// exemptDirs are package directories excused from the TestMain requirement
// despite spawning git in their tests, each with the reason a package-level
// TestMain would break them.
var exemptDirs = map[string]string{
	// internal/testenv: testenv_test.go tests Install/cleanup's own env
	// mutation and restoration (TestHermeticGitPolicy asserts
	// GIT_CONFIG_GLOBAL is unset before Install, set after, and restored
	// after cleanup). A package-level TestMain would call Install before
	// these tests run, pre-setting that variable and making "restore to
	// previous" restore to the outer policy instead of the unset state the
	// assertions require, so this package's tests must own Install/cleanup
	// directly rather than go through testenv.Main.
	"internal/testenv": "tests Install/cleanup's own before/after env state; a package-level TestMain would pre-install and break those assertions",
}

// TestGitSpawningTestPackagesInstallPolicy requires that every package in
// this module whose tests spawn a real git process also installs the
// hermetic policy via a TestMain that calls testenv.Main, so test-owned Git
// activity never depends on the workstation's ambient configuration
// (Designs/TestSuiteReliability DD-1). It walks the module from its own
// package outward, groups *_test.go files by directory, and flags any
// directory that spawns git without declaring the required TestMain.
func TestGitSpawningTestPackagesInstallPolicy(t *testing.T) {
	moduleRoot := findModuleRoot(t)

	spawningDirs := map[string]bool{}
	testMainDirs := map[string]bool{}

	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base != filepath.Base(moduleRoot) && strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || filepath.Base(path) == selfFile {
			return nil
		}
		dir := filepath.Dir(path)
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if gitSpawnPattern.Match(src) {
			spawningDirs[dir] = true
		}
		if declaresHermeticTestMain(t, path, src) {
			testMainDirs[dir] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking module: %v", err)
	}

	var offenders []string
	for dir := range spawningDirs {
		if testMainDirs[dir] {
			continue
		}
		rel, err := filepath.Rel(moduleRoot, dir)
		if err == nil {
			if _, exempt := exemptDirs[filepath.ToSlash(rel)]; exempt {
				continue
			}
		}
		offenders = append(offenders, dir)
	}
	if len(offenders) == 0 {
		return
	}
	sort.Strings(offenders)
	rel := make([]string, len(offenders))
	for i, dir := range offenders {
		r, err := filepath.Rel(moduleRoot, dir)
		if err != nil {
			r = dir
		}
		rel[i] = filepath.ToSlash(r)
	}
	t.Fatalf(
		"packages spawn git in tests without installing the hermetic policy "+
			"(add a TestMain that calls testenv.Main): %s",
		strings.Join(rel, ", "),
	)
}

// declaresHermeticTestMain reports whether src declares
// func TestMain(m *testing.M) whose body calls testenv.Main.
func declaresHermeticTestMain(t *testing.T, path string, src []byte) bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "TestMain" || fn.Body == nil {
			continue
		}
		callsTestenvMain := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			// Within package testenv itself, Main is called unqualified.
			if (pkg.Name == "testenv" && sel.Sel.Name == "Main") ||
				(file.Name.Name == "testenv" && sel.Sel.Name == "Main") {
				callsTestenvMain = true
			}
			return true
		})
		// Also handle an unqualified Main(m) call from within package testenv.
		if !callsTestenvMain && file.Name.Name == "testenv" {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "Main" {
					callsTestenvMain = true
				}
				return true
			})
		}
		if callsTestenvMain {
			return true
		}
	}
	return false
}

// findModuleRoot walks up from this package's directory to the nearest
// go.mod.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from package directory")
		}
		dir = parent
	}
}
