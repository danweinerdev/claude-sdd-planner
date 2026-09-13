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
//
// internal/testenv itself was previously exempted here (testenv_test.go
// tests Install/cleanup's own before/after env state, and a package-level
// TestMain would pre-install and break those assertions). Re-checked under
// the import-based rule: testenv_test.go still imports os/exec directly (it
// spawns a real git binary to assert the policy takes effect), so the
// package still spawns and the same conflict with a package-level TestMain
// still applies. The exemption stays for the same reason.
var exemptDirs = map[string]string{
	"internal/testenv": "tests Install/cleanup's own before/after env state; a package-level TestMain would pre-install and break those assertions",
}

// TestGitSpawningTestPackagesInstallPolicy requires that every package in
// this module whose tests spawn a real subprocess also installs the
// hermetic policy via a TestMain that calls testenv.Main, so test-owned Git
// activity never depends on the workstation's ambient configuration
// (Designs/TestSuiteReliability DD-1). It walks the module from its own
// package outward and flags any package that spawns — directly in a test
// file, in its own production sources, or transitively through a module
// package it (or its tests) import — without declaring the required
// TestMain.
func TestGitSpawningTestPackagesInstallPolicy(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	offenders := gitSpawnOffenders(t, moduleRoot, selfFile, exemptDirs)
	if len(offenders) == 0 {
		return
	}
	t.Fatalf(
		"packages spawn a subprocess in tests without installing the hermetic "+
			"policy (add a TestMain that calls testenv.Main): %s",
		strings.Join(offenders, ", "),
	)
}

// pkgInfo holds one package directory's parsed shape: its module import
// path, whether its own production sources import a spawning package
// directly, whether any of its test files import one directly, whether it
// declares the required TestMain, and the module-internal import paths
// reachable from its production sources and from its test files
// respectively.
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

// spawnImportPaths names the import paths that make a file a direct
// spawner: the standard library's process-launch package and this module's
// own process-launch wrapper. Membership is resolved purely from each
// file's import path list (go/parser with ImportsOnly) — never from
// identifiers, aliases, or string literals in the file's code, so import
// aliasing, concatenated literals, or variable-named call sites cannot
// hide a spawn from this scan.
func spawnImportPaths(modulePath string) map[string]bool {
	return map[string]bool{
		"os/exec":                         true,
		modulePath + "/internal/procexec": true,
	}
}

// gitSpawnOffenders walks moduleRoot, parses every .go file's imports with
// go/parser (ImportsOnly), and returns the module-relative,
// slash-separated, sorted list of package directories whose tests spawn —
// directly, through a local wrapper, or transitively through an imported
// module package's production sources — without declaring the required
// TestMain. A package is spawning purely by import path: its test sources
// import os/exec or the module's internal/procexec package directly, or
// the package (through its production or test imports) transitively
// reaches, within the module, a package whose production sources import
// either. skipTestFile names a test file (by base name) to exclude from
// scanning entirely (this test's own file). exempt maps module-relative
// directories to their excuse for skipping the TestMain requirement
// despite spawning.
func gitSpawnOffenders(t *testing.T, moduleRoot, skipTestFile string, exempt map[string]string) []string {
	t.Helper()

	modulePath := readModulePath(t, moduleRoot)
	spawnImports := spawnImportPaths(modulePath)
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
		file, perr := parser.ParseFile(fset, path, src, parser.ImportsOnly)
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
		spawns := false
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			imports[p] = true
			if spawnImports[p] {
				spawns = true
			}
		}

		isTest := strings.HasSuffix(path, "_test.go")
		hasMain := false
		if isTest {
			// TestMain detection still needs the full AST (it inspects a
			// function body), so parse again without ImportsOnly only for
			// _test.go files, which are comparatively few.
			fullFile, ferr := parser.ParseFile(fset, path, src, 0)
			if ferr != nil {
				t.Fatalf("parsing %s: %v", path, ferr)
			}
			hasMain = declaresHermeticTestMain(fullFile)
		}

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
	// transitive reachability of each module package to a spawning
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

// TestSpawnInventoryIsImportBased requires that the git-spawn inventory
// resolves packages by import path only: a test package is spawning when
// any of its test sources import os/exec or the module's procexec package,
// or when the package or its test sources transitively import (within the
// module) a package whose production sources import either, so identifier
// names, string literals, and comments play no part and wrappers, import
// aliases, concatenated binary names, variable-named spawners, and helper
// packages cannot escape. It builds a synthetic module under t.TempDir()
// with six packages:
//
//   - w: a _test.go file that imports os/exec under the alias ex and
//     spawns via ex.Command("g"+"it") — a concatenated literal, not the
//     bare string "git", and an import alias rather than the identifier
//     "exec".
//   - h: a production (non-test) file with var run = exec.Command (a
//     variable-named spawner, not a call the literal-matcher recognizes)
//     and func Spawn(name string) that invokes run(name).
//   - u: a _test.go file with no direct exec import that only calls
//     h.Spawn("git"), reaching a spawner transitively through h.
//   - r: a production file defining type runner func(dir, name string) and
//     an execRunner variable of that type built from os/exec under a
//     variable name (not a recognized selector call shape), plus a
//     Run(dir, name string) wrapper; its _test.go file only calls
//     r.Run(dir, "git") and never mentions exec directly.
//   - c: a _test.go file whose only mention of exec.Command("git") is in a
//     comment, never in code.
//   - d: a _test.go file that imports os/exec and declares a proper
//     TestMain calling testenv.Main.
//
// The expected offender set is exactly {w, u, r}: c must not be flagged
// (comment, not code) and d must not be flagged (it installs the policy).
// Detection here must be purely import-based — none of w, u, or r contains
// a string literal "git" or a recognized pkg.Func(...) call shape that the
// prior literal/identifier matcher required.
func TestSpawnInventoryIsImportBased(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root, "go.mod", "module synthtest\n\ngo 1.21\n")

	writeFile(t, root, "w/w_test.go", `package w

import (
	ex "os/exec"
	"testing"
)

func TestW(t *testing.T) {
	cmd := ex.Command("g" + "it")
	_ = cmd.Run()
}
`)

	writeFile(t, root, "h/h.go", `package h

import "os/exec"

var run = exec.Command

func Spawn(name string) error {
	cmd := run(name)
	return cmd.Run()
}
`)

	writeFile(t, root, "u/u_test.go", `package u

import (
	"testing"

	"synthtest/h"
)

func TestU(t *testing.T) {
	if err := h.Spawn("git"); err != nil {
		t.Fatal(err)
	}
}
`)

	writeFile(t, root, "r/r.go", `package r

import "os/exec"

type runner func(dir, name string) *exec.Cmd

var execRunner runner = func(dir, name string) *exec.Cmd {
	cmd := exec.Command(name)
	cmd.Dir = dir
	return cmd
}

func Run(dir, name string) error {
	cmd := execRunner(dir, name)
	return cmd.Run()
}
`)

	writeFile(t, root, "r/r_test.go", `package r

import "testing"

func TestR(t *testing.T) {
	if err := Run(".", "git"); err != nil {
		t.Fatal(err)
	}
}
`)

	writeFile(t, root, "c/c_test.go", `package c

import "testing"

// TestC does not spawn git; exec.Command("git") only appears in this
// comment for documentation purposes.
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

	want := []string{"r", "u", "w"}
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
