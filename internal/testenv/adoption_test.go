package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// selfFile is this test file's own base name, excluded from scanning so its
// own doc comments (which name git-spawn call shapes as text) can never be
// mistaken for code that spawns git.
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

// gitSpawnCalls names the selector expressions (pkg.Func) that spawn or
// resolve a real git binary when their first argument is the string literal
// "git": exec.Command, exec.CommandContext (first argument after the
// context), exec.LookPath, procexec.Run (first argument after the
// context), and procexec.LookPath. Resolving the git binary is only ever
// followed by spawning it via the resolved path in this module's code, so
// LookPath counts as a spawn site too.
var gitSpawnCalls = map[string]map[string]int{
	"exec": {
		"Command":        0,
		"CommandContext": 1,
		"LookPath":       0,
	},
	"procexec": {
		"Run":      1,
		"LookPath": 0,
	},
}

// TestGitSpawningTestPackagesInstallPolicy requires that every package in
// this module whose tests spawn a real git process also installs the
// hermetic policy via a TestMain that calls testenv.Main, so test-owned Git
// activity never depends on the workstation's ambient configuration
// (Designs/TestSuiteReliability DD-1). It walks the module from its own
// package outward and flags any package that spawns git — directly in a
// test file, in its own production sources, or transitively through a
// module package it (or its tests) import — without declaring the required
// TestMain.
func TestGitSpawningTestPackagesInstallPolicy(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	offenders := gitSpawnOffenders(t, moduleRoot, selfFile, exemptDirs)
	if len(offenders) == 0 {
		return
	}
	t.Fatalf(
		"packages spawn git in tests without installing the hermetic policy "+
			"(add a TestMain that calls testenv.Main): %s",
		strings.Join(offenders, ", "),
	)
}

// pkgInfo holds one package directory's parsed shape: its module import
// path, whether its own production sources spawn git directly, whether any
// of its test files spawn git directly, whether it declares the required
// TestMain, and the module-internal import paths reachable from its
// production sources and from its test files respectively.
type pkgInfo struct {
	dir             string
	importPath      string
	prodSpawns      bool
	testSpawns      bool
	hasTestMain     bool
	prodImports     map[string]bool
	testImports     map[string]bool
	hasTestFiles    bool
	hasProductionGo bool
}

// gitSpawnOffenders walks moduleRoot, parses every .go file with go/parser,
// and returns the module-relative, slash-separated, sorted list of package
// directories whose tests spawn git — directly, through a local wrapper,
// or transitively through an imported module package's production sources
// — without declaring the required TestMain. skipTestFile names a test
// file (by base name) to exclude from scanning entirely (this test's own
// file). exempt maps module-relative directories to their excuse for
// skipping the TestMain requirement despite spawning git.
func gitSpawnOffenders(t *testing.T, moduleRoot, skipTestFile string, exempt map[string]string) []string {
	t.Helper()

	modulePath := readModulePath(t, moduleRoot)
	pkgs := map[string]*pkgInfo{} // keyed by import path

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
		if !strings.HasSuffix(path, ".go") || filepath.Base(path) == skipTestFile {
			return nil
		}
		dir := filepath.Dir(path)
		rel, err := filepath.Rel(moduleRoot, dir)
		if err != nil {
			return err
		}
		importPath := modulePath
		if rel != "." {
			importPath = modulePath + "/" + filepath.ToSlash(rel)
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			t.Fatalf("parsing %s: %v", path, perr)
		}

		info := pkgs[importPath]
		if info == nil {
			info = &pkgInfo{
				dir:         dir,
				importPath:  importPath,
				prodImports: map[string]bool{},
				testImports: map[string]bool{},
			}
			pkgs[importPath] = info
		}

		imports := map[string]bool{}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			imports[p] = true
		}

		isTest := strings.HasSuffix(path, "_test.go")
		var spawns bool
		if isTest {
			spawns = fileHasGitLiteral(file)
		} else {
			spawns = fileCallsGitSpawner(file)
		}
		hasMain := declaresHermeticTestMain(file)

		if isTest {
			info.hasTestFiles = true
			if spawns {
				info.testSpawns = true
			}
			if hasMain {
				info.hasTestMain = true
			}
			for p := range imports {
				info.testImports[p] = true
			}
		} else {
			info.hasProductionGo = true
			if spawns {
				info.prodSpawns = true
			}
			for p := range imports {
				info.prodImports[p] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking module: %v", err)
	}

	// A package's effective "reaches a spawner" set starts from its own
	// production spawn status and its production imports; memoize the
	// transitive reachability of each module package to a git-spawning
	// production package.
	reaches := map[string]bool{}
	visiting := map[string]bool{}
	var reachesSpawner func(importPath string) bool
	reachesSpawner = func(importPath string) bool {
		if v, ok := reaches[importPath]; ok {
			return v
		}
		info, ok := pkgs[importPath]
		if !ok {
			return false // outside the module
		}
		if visiting[importPath] {
			return false // import cycle guard
		}
		visiting[importPath] = true
		result := info.prodSpawns
		if !result {
			for imp := range info.prodImports {
				if reachesSpawner(imp) {
					result = true
					break
				}
			}
		}
		visiting[importPath] = false
		reaches[importPath] = result
		return result
	}

	var offenders []string
	for importPath, info := range pkgs {
		if !info.hasTestFiles {
			continue
		}
		spawns := info.testSpawns || reachesSpawner(importPath)
		if !spawns {
			for imp := range info.testImports {
				if reachesSpawner(imp) {
					spawns = true
					break
				}
			}
		}
		if !spawns {
			continue
		}
		if info.hasTestMain {
			continue
		}
		rel, err := filepath.Rel(moduleRoot, info.dir)
		if err == nil {
			if _, exempt := exempt[filepath.ToSlash(rel)]; exempt {
				continue
			}
		}
		r, err := filepath.Rel(moduleRoot, info.dir)
		if err != nil {
			r = info.dir
		}
		offenders = append(offenders, filepath.ToSlash(r))
	}
	sort.Strings(offenders)
	return offenders
}

// fileHasGitLiteral reports whether file contains the string literal "git"
// anywhere in code (comments are not part of the AST, so they never
// match). Used for _test.go files: a test spawns git whether it passes the
// literal directly to exec.Command or hands it to a local wrapper.
func fileHasGitLiteral(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if strings.Trim(lit.Value, `"`) == "git" {
			found = true
			return false
		}
		return true
	})
	return found
}

// fileCallsGitSpawner reports whether file contains a call to one of
// gitSpawnCalls whose designated argument is the direct string literal
// "git". Used for production .go files, where only a genuine spawn call
// site counts, not any mention of the literal.
func fileCallsGitSpawner(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		argIdx, ok := gitSpawnCalls[pkgIdent.Name][sel.Sel.Name]
		if !ok {
			return true
		}
		if argIdx >= len(call.Args) {
			return true
		}
		lit, ok := call.Args[argIdx].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if strings.Trim(lit.Value, `"`) == "git" {
			found = true
			return false
		}
		return true
	})
	return found
}

// declaresHermeticTestMain reports whether file declares
// func TestMain(m *testing.M) whose body calls testenv.Main.
func declaresHermeticTestMain(file *ast.File) bool {
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

// readModulePath reads the module path from moduleRoot/go.mod's leading
// "module " directive.
func readModulePath(t *testing.T, moduleRoot string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}
	t.Fatal("go.mod has no module directive")
	return ""
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

// TestGitSpawnInventoryFollowsIndirection requires that the git-spawn
// inventory follows indirection: a test package that spawns git only
// through a local wrapper function, or only by calling a helper defined in
// a sibling production package, must still be flagged, while a package
// that merely mentions "git" in a comment must not be. It builds a
// synthetic module under t.TempDir() with five packages:
//
//   - helper: a production (non-test) file whose function SpawnGit calls
//     exec.Command("git", ...) directly.
//   - a: a _test.go file with no TestMain that spawns git only through a
//     local wrapper run(t, name, args...) — not a literal exec.Command("git").
//   - b: a _test.go file with no TestMain that spawns git only by calling
//     helper.SpawnGit.
//   - c: a _test.go file with no TestMain that only mentions "git" in a
//     comment, never in code.
//   - d: a _test.go file with a literal exec.Command("git", ...) spawn and
//     a proper TestMain that calls testenv.Main.
//
// The expected offender set is exactly {a, b}: c must not be flagged
// (comment, not code) and d must not be flagged (it installs the policy).
func TestGitSpawnInventoryFollowsIndirection(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root, "go.mod", "module synthtest\n\ngo 1.21\n")

	writeFile(t, root, "helper/helper.go", `package helper

import "os/exec"

func SpawnGit() error {
	cmd := exec.Command("git", "status")
	return cmd.Run()
}
`)

	writeFile(t, root, "a/a_test.go", `package a

import (
	"os/exec"
	"testing"
)

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	_ = cmd.Run()
}

func TestA(t *testing.T) {
	run(t, "git", "status")
}
`)

	writeFile(t, root, "b/b_test.go", `package b

import (
	"testing"

	"synthtest/helper"
)

func TestB(t *testing.T) {
	if err := helper.SpawnGit(); err != nil {
		t.Fatal(err)
	}
}
`)

	writeFile(t, root, "c/c_test.go", `package c

import "testing"

// TestC does not spawn git; this comment just mentions the word git for
// documentation purposes only.
func TestC(t *testing.T) {
}
`)

	writeFile(t, root, "d/d_test.go", `package d

import (
	"os/exec"
	"testing"
)

func TestMain(m *testing.M) {
	testenv.Main(m)
}

func TestD(t *testing.T) {
	cmd := exec.Command("git", "status")
	_ = cmd.Run()
}
`)

	offenders := gitSpawnOffenders(t, root, "", nil)

	want := []string{"a", "b"}
	if !equalStrings(offenders, want) {
		t.Fatalf("gitSpawnOffenders = %v, want %v", offenders, want)
	}
}

// writeFile writes contents to root/relPath, creating parent directories as
// needed.
func writeFile(t *testing.T, root, relPath, contents string) {
	t.Helper()
	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", full, err)
	}
}

// equalStrings reports whether two string slices are equal.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
