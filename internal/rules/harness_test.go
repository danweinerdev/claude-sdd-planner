package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// prepared is one materialized example fixture: files written and Setup
// history committed exactly once (FR-04, DD-8). Evaluations load fresh
// roots from it and never share a Root.
type prepared struct {
	dir string
	ex  Example
}

// fixtureSnapshot is the read-only view a validation pass must not change:
// every non-.git file's bytes (by digest), the index (path, mode, blob;
// `git ls-files -s`, since `git status` legitimately refreshes stat data in
// the index file), and HEAD.
type fixtureSnapshot map[string]string

// prepareCalls counts fixture preparations so a test can prove a
// determinism case prepares exactly once.
var prepareCalls atomic.Int64

// prepareExample materializes ex once: files (with {{REPO}} substituted by
// the fixture root) and every Setup command under the fixed fixture
// identity, so commit ids are reproducible.
func prepareExample(t *testing.T, ex Example) *prepared {
	t.Helper()
	prepareCalls.Add(1)
	dir := t.TempDir()
	for rel, content := range ex.Files {
		content = strings.ReplaceAll(content, "{{REPO}}", dir)
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range ex.Setup {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), setupEnv...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup command %v: %v\n%s", args, err, out)
		}
	}
	return &prepared{dir: dir, ex: ex}
}

// evaluatePrepared loads a fresh Root from the prepared fixture and runs
// the strict evaluation. Each call returns a distinct Root.
func evaluatePrepared(t *testing.T, p *prepared) (*Root, []Diagnostic) {
	t.Helper()
	root, err := LoadRoot(p.dir)
	if err != nil {
		t.Fatalf("LoadRoot: %v", err)
	}
	return root, Run(root)
}

// runDeterminismCase is the body TestRunIsDeterministic runs per Bad
// example: prepare once, evaluate four fresh roots. It returns the roots so
// a test can prove they were independent, and the four complete results.
func runDeterminismCase(t *testing.T, ex Example) (roots []*Root, results [][]Diagnostic) {
	t.Helper()
	p := prepareExample(t, ex)
	for i := 0; i < 4; i++ {
		r, d := evaluatePrepared(t, p)
		roots = append(roots, r)
		results = append(results, d)
	}
	return roots, results
}

// diagnosticsEqual compares two complete ordered diagnostic lists: every
// field (code, severity, path, line, message, correction, implicated,
// waived reason), in order. It reports the first difference.
func diagnosticsEqual(a, b []Diagnostic) (bool, string) {
	if len(a) != len(b) {
		return false, "diagnostic count differs"
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			aj, _ := json.Marshal(a[i])
			bj, _ := json.Marshal(b[i])
			return false, "diagnostic " + string(rune('0'+i%10)) + " differs:\n  " + string(aj) + "\n  " + string(bj)
		}
	}
	return true, ""
}

// snapshotFixture captures the read-only view of a prepared fixture.
func snapshotFixture(t *testing.T, p *prepared) fixtureSnapshot {
	t.Helper()
	snap := fixtureSnapshot{}
	err := filepath.WalkDir(p.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(p.dir, path)
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		snap["file:"+filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(p.dir, ".git")); statErr == nil {
		gitOut := func(args ...string) string {
			cmd := exec.Command("git", append([]string{"-C", p.dir}, args...)...)
			cmd.Env = append(os.Environ(), setupEnv...)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("git %v: %v", args, err)
			}
			return strings.TrimSpace(string(out))
		}
		snap["git:HEAD"] = gitOut("rev-parse", "HEAD")
		snap["git:index"] = gitOut("ls-files", "-s")
	}
	return snap
}

func representativeBad(t *testing.T) map[string]Example {
	t.Helper()
	out := map[string]Example{}
	for _, code := range []string{"SDD020", "SDD154"} {
		r := ruleByCode(t, code)
		if len(r.Bad) == 0 {
			t.Fatalf("%s has no Bad example", code)
		}
		out[code] = r.Bad[0]
	}
	return out
}

// FR-04 / AC-02: a determinism case prepares its fixture exactly once and
// evaluates four independently loaded roots.
func TestDeterminismPreparationCount(t *testing.T) {
	for code, ex := range representativeBad(t) {
		t.Run(code, func(t *testing.T) {
			before := prepareCalls.Load()
			roots, results := runDeterminismCase(t, ex)
			if got := prepareCalls.Load() - before; got != 1 {
				t.Errorf("fixture prepared %d times, want exactly 1", got)
			}
			if len(results) != 4 {
				t.Fatalf("%d evaluations, want 4", len(results))
			}
			seen := map[*Root]bool{}
			for _, r := range roots {
				if r == nil {
					t.Fatal("evaluation returned no Root; roots must be independently loaded")
				}
				seen[r] = true
			}
			if len(seen) != 4 {
				t.Errorf("%d distinct roots across 4 evaluations, want 4", len(seen))
			}
		})
	}
}

// FR-05 / AC-02 (deterministic-replay): the same prepared fixture yields a
// byte-identical diagnostic trace across two evaluations, and the
// comparison is complete: a drifting message, line, correction or waived
// reason fails it even when every code matches.
func TestCompleteDiagnosticComparison(t *testing.T) {
	for code, ex := range representativeBad(t) {
		t.Run(code, func(t *testing.T) {
			p := prepareExample(t, ex)
			_, first := evaluatePrepared(t, p)
			_, again := evaluatePrepared(t, p)
			fb, _ := json.Marshal(first)
			ab, _ := json.Marshal(again)
			if !reflect.DeepEqual(fb, ab) {
				t.Fatalf("trace differs between two evaluations of one fixture:\n%s\n%s", fb, ab)
			}
			if ok, why := diagnosticsEqual(first, again); !ok {
				t.Fatalf("comparison rejected identical traces: %s", why)
			}
			if len(first) == 0 {
				t.Fatal("representative Bad example produced no diagnostics")
			}
			drifts := map[string]func(d *Diagnostic){
				"message":    func(d *Diagnostic) { d.Message += " (drift)" },
				"line":       func(d *Diagnostic) { d.Line++ },
				"correction": func(d *Diagnostic) { d.Correction += " drift" },
				"severity":   func(d *Diagnostic) { d.Severity = Candidate },
				"waived":     func(d *Diagnostic) { d.WaivedReason = "drift" },
				"path":       func(d *Diagnostic) { d.Path += ".drift" },
			}
			for name, drift := range drifts {
				mutated := append([]Diagnostic(nil), first...)
				drift(&mutated[0])
				if ok, _ := diagnosticsEqual(first, mutated); ok {
					t.Errorf("a %s drift with matching codes was not detected", name)
				}
			}
		})
	}
}

// FR-04: read-only validation leaves fixture bytes, index and HEAD unchanged
// across four evaluations.
func TestValidationLeavesFixtureUnchanged(t *testing.T) {
	for code, ex := range representativeBad(t) {
		t.Run(code, func(t *testing.T) {
			p := prepareExample(t, ex)
			before := snapshotFixture(t, p)
			if len(before) == 0 {
				t.Fatal("snapshot captured nothing")
			}
			if len(ex.Setup) > 0 {
				if _, ok := before["git:HEAD"]; !ok {
					t.Fatal("git fixture snapshot lacks HEAD")
				}
				if _, ok := before["git:index"]; !ok {
					t.Fatal("git fixture snapshot lacks the index")
				}
			}
			for i := 0; i < 4; i++ {
				evaluatePrepared(t, p)
			}
			after := snapshotFixture(t, p)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("validation changed the fixture:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}
