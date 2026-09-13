package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// The pure selection and the coverage inventory that keeps it honest
// (FR-06, AC-03, DD-8).
//
// `make test-pure` is a documented, targeted selection: the TestPure* cases
// in this package, which exercise the rules package's pure transformations
// and must run with no SCM on PATH and no process starts at all. Two things
// can quietly hollow that out, and each has a test here:
//
//   - The selection goes vacuous. A `-run` pattern that matches nothing
//     exits 0, so "make test-pure passed" would stop meaning anything.
//     TestPureSelectionInventory pins the executed set to a declared list
//     and runs it for real under an empty PATH.
//   - The relocated real-SCM assertions go missing. Moving pure logic out
//     of the SCM-backed tests is only safe while the boundary assertions
//     those tests carried still execute in the full suite.
//     TestSCMBoundaryInventory pins each boundary to the function that
//     carries it, so a rename or deletion fails rather than silently
//     reducing coverage.

// pureSelectionPattern is the selector `make test-pure` uses. The Makefile
// target and this test must agree; TestPureSelectionInventory asserts the
// declared inventory is exactly what the pattern matches.
const pureSelectionPattern = "^TestPure"

// pureInventory is the declared set of pure test cases. It is written out
// rather than derived so that adding or removing a TestPure case is a
// deliberate edit to this list, reviewable in the diff that makes it.
//
// Every entry must be a test that touches no repository, no SCM binary and
// no subprocess: it may build strings, parse artifact bytes, and evaluate
// rules over an in-memory or plain-directory root, and nothing else.
var pureInventory = []string{
	"TestPureApplyWaiversExcusesOnlyMatchingArtifact",
	"TestPureDemoteRetiredFindingsKeepsSupersessionErrors",
	"TestPureEvaluateRunsEachRuleOncePerScope",
	"TestPureEvaluateOverPureRulesNeedsNoRepository",
	"TestPureFrontmatterScalarNormalization",
	"TestPureMarkdownVisibilityStripsFencesAndComments",
	"TestPureParseArtifactBytesStageLadder",
	"TestPureQualifiedCitationRequiresColon",
	"TestPureSectionsAreDelimitedBySameDepthHeadings",
	"TestPureSortDiagnosticsOrdersByPathLineCodeMessage",
	"TestPureSpecDefinedIDsPerFamily",
	"TestPureStringDuplicatesAreSortedAndUnique",
	"TestPureWaiverValidationRejects",
	"TestPureWaiversParseWellFormedEntries",
}

// pureChildEnv gates the child process TestPureSelectionInventory re-execs:
// the child runs the declared selection, the parent inspects its report.
const pureChildEnv = "RULES_PURE_SELECTION_CHILD"

// FR-06 / AC-03 / DD-8: the documented pure selection executes a nonzero,
// exactly-declared inventory of TestPure* cases, and every one of them
// passes with no SCM anywhere on PATH.
//
// PATH pointing at an empty directory is what makes "zero SCM process
// starts" observable rather than asserted: a pure case that reached for
// git, p4, or any other executable would fail to start it and fail the
// child run. The child's verbose report is then checked case by case, so a
// silently skipped or never-run case fails too.
func TestPureSelectionInventory(t *testing.T) {
	if os.Getenv(pureChildEnv) != "" {
		t.Skip("parent case; the child runs the selection itself")
	}

	declared := append([]string(nil), pureInventory...)
	sort.Strings(declared)
	if len(declared) == 0 {
		t.Fatal("the pure inventory is empty: a selection that executes nothing cannot gate anything")
	}
	for i := 1; i < len(declared); i++ {
		if declared[i] == declared[i-1] {
			t.Fatalf("pureInventory declares %s twice", declared[i])
		}
	}

	// The declared list must be exactly the TestPure* functions this
	// package's test files define, so neither side can drift: an
	// undeclared new case is as much a failure as a declared missing one.
	found := testFunctionsIn(t, ".", func(name string) bool {
		return strings.HasPrefix(name, "TestPure")
	})
	// The inventory test itself starts with TestPure and is not a pure
	// case: it re-execs a child process. Exclude it from both sides.
	delete(found, "TestPureSelectionInventory")
	var declaredSet = map[string]bool{}
	for _, name := range declared {
		declaredSet[name] = true
		if !found[name] {
			t.Errorf("pureInventory declares %s, but no such test function is defined in this package", name)
		}
	}
	for name := range found {
		if !declaredSet[name] {
			t.Errorf("%s is defined but not declared in pureInventory; add it deliberately", name)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// Run the declared selection for real, with nothing executable on
	// PATH. Anchored alternation, so the child runs exactly the declared
	// cases and not this inventory test.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	selection := "^(" + strings.Join(declared, "|") + ")$"
	empty := t.TempDir()
	child := exec.Command(exe, "-test.run="+selection, "-test.v", "-test.count=1")
	child.Env = append(scrubbedEnv(), "PATH="+empty, pureChildEnv+"=1", "SDD_VCS_DISABLE_P4=1")
	out, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("the pure selection failed with no SCM on PATH: %v\n%s", err, out)
	}
	report := string(out)
	for _, name := range declared {
		if !strings.Contains(report, "--- PASS: "+name) {
			t.Errorf("%s did not report PASS under the SCM-free selection:\n%s", name, report)
		}
		if strings.Contains(report, "--- SKIP: "+name) {
			t.Errorf("%s was skipped; a skipped case executes no assertion", name)
		}
	}
	if strings.Contains(report, "no tests to run") {
		t.Fatalf("the selection matched nothing:\n%s", report)
	}
}

// scmBoundary is one real-SCM behavior the pure extraction must not have
// cost the suite, mapped to the test functions that still carry it.
type scmBoundary struct {
	// what the boundary is, in the spec's own terms.
	what string
	// pkg is the package directory, relative to this one.
	pkg string
	// tests are the functions that assert it. All must exist.
	tests []string
}

// scmBoundaries is the coverage map FR-06 requires: each real-SCM boundary
// assertion associated with the concrete test that executes it in the full
// suite. Renaming or deleting one of these without updating this map fails
// TestSCMBoundaryInventory, which is the point — a removal with no mapped
// equivalent must not pass silently.
var scmBoundaries = []scmBoundary{
	{
		what:  "repository detection: plain tree, git repository, linked worktree",
		pkg:   "../vcs",
		tests: []string{"TestDetectNoRepo", "TestGitDetection", "TestGitWorktreeDetection"},
	},
	{
		what:  "linked worktrees are detected as their own kind, not as the main repository",
		pkg:   "../vcs",
		tests: []string{"TestGitWorktreeDetection"},
	},
	{
		what:  "history reads: committed content, parents, ancestry, revisions after a point",
		pkg:   "../vcs",
		tests: []string{"TestFileAt", "TestParentsAndIsAncestor", "TestRevisionsAfter", "TestRevisionExists"},
	},
	{
		what:  "staged (index) versus worktree state are distinct observations",
		pkg:   "../vcs",
		tests: []string{"TestClean", "TestChangedPaths"},
	},
	{
		what:  "an unstaged worktree file cannot excuse a staged deletion (index is read on its own)",
		pkg:   ".",
		tests: []string{"TestUnstagedGraphDoesNotExcuseStagedDeletion", "TestGraphConversionExcusesStagedDeletion"},
	},
	{
		what:  "absence (authoritative not-found) is distinguished from execution failure",
		pkg:   "../vcs",
		tests: []string{"TestAuthoritativeSCMAbsence", "TestVCSOperationalErrors"},
	},
	{
		what:  "an unrunnable SCM aborts evaluation rather than becoming findings",
		pkg:   ".",
		tests: []string{"TestIgnoredRepoErrorAbortsEvaluation", "TestRetirementDetectionFailureIsOperational"},
	},
	{
		what:  "path edge cases: canonical spelling agrees with the SCM's own answer",
		pkg:   "../vcs",
		tests: []string{"TestCanonPathAgreesWithGitToplevel", "TestCanonPathNonexistentPassthrough", "TestCanonPathIdempotent", "TestCanonPathResolvesTempDirSpelling"},
	},
	{
		what:  "mutation between validations: a reloaded root observes index and HEAD changes",
		pkg:   ".",
		tests: []string{"TestReloadSeesSCMMutation", "TestAppendOnlyScanOnce"},
	},
	{
		what:  "validation is read-only: fixture bytes, index and HEAD survive it",
		pkg:   ".",
		tests: []string{"TestValidationLeavesFixtureUnchanged"},
	},
	{
		what:  "ambient host configuration cannot reach in-process validation",
		pkg:   ".",
		tests: []string{"TestInProcessValidationHermetic"},
	},
	{
		what:  "every registered Good/Bad example still executes, including the git-backed ones",
		pkg:   ".",
		tests: []string{"TestExamplesBehaveAsDeclared", "TestEveryRuleHasGoodAndBadExamples", "TestRunIsDeterministic"},
	},
}

// FR-06 / AC-03 / DD-8: every mapped real-SCM boundary assertion still
// exists as a runnable test function. The map is the coverage inventory;
// this test is what makes it load-bearing instead of documentation.
func TestSCMBoundaryInventory(t *testing.T) {
	if len(scmBoundaries) == 0 {
		t.Fatal("the SCM boundary map is empty")
	}
	byPkg := map[string]map[string]bool{}
	for _, b := range scmBoundaries {
		if len(b.tests) == 0 {
			t.Errorf("boundary %q names no test; an unmapped boundary is an uncovered one", b.what)
			continue
		}
		if _, seen := byPkg[b.pkg]; !seen {
			byPkg[b.pkg] = testFunctionsIn(t, b.pkg, func(string) bool { return true })
		}
		for _, name := range b.tests {
			if !byPkg[b.pkg][name] {
				t.Errorf("boundary %q maps to %s in %s, which no longer exists; "+
					"map the assertion to its replacement or restore it",
					b.what, name, b.pkg)
			}
		}
	}

	// The spec names the boundaries that must stay covered. Each must
	// appear in the map, so dropping a whole boundary fails too.
	for _, required := range []string{
		"detection", "worktree", "staged", "history", "absence",
		"path edge cases", "mutation between validations",
	} {
		hit := false
		for _, b := range scmBoundaries {
			if strings.Contains(b.what, required) {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("no mapped boundary covers %q", required)
		}
	}
}

// testFunctionsIn parses every _test.go file in a package directory and
// returns the names of the top-level `func TestXxx(*testing.T)` it
// declares, filtered by keep. Parsing the source (rather than asking the
// toolchain to list tests) keeps this test free of process starts, so it
// runs in the pure selection's own environment.
func testFunctionsIn(t *testing.T, dir string, keep func(string) bool) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !isTestFuncName(fn.Name.Name) || !isTestingTParam(fn.Type) {
				continue
			}
			if keep(fn.Name.Name) {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

var testNameRe = regexp.MustCompile(`^Test([A-Z_].*)?$`)

func isTestFuncName(name string) bool { return testNameRe.MatchString(name) }

// isTestingTParam reports whether a function's signature is the ordinary
// `(t *testing.T)` a `go test` run invokes — excluding TestMain, whose
// parameter is *testing.M.
func isTestingTParam(ft *ast.FuncType) bool {
	if ft.Params == nil || len(ft.Params.List) != 1 {
		return false
	}
	star, ok := ft.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && sel.Sel.Name == "T"
}

// scrubbedEnv is the current environment with PATH removed, so the caller's
// replacement is the only one the child sees.
func scrubbedEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, "PATH") {
			continue
		}
		if runtime.GOOS == "windows" && strings.EqualFold(k, "PATHEXT") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
