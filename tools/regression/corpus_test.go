package regression

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
)

const (
	manifestPath     = "fixtures/MANIFEST"
	expectationsPath = "expectations.json"
	fixturesDir      = "fixtures"
)

// TestCorpus is the breadth check: every fixture root must still produce
// exactly the diagnostics it is recorded as producing.
//
// Unit tests cover rules one at a time. This covers their interaction — a
// change to one rule that perturbs another's output fails here and nowhere
// else, which is precisely the class of regression that is otherwise found by
// a user rather than by the build.
func TestCorpus(t *testing.T) {
	failures, err := Check(manifestPath, expectationsPath, fixturesDir)
	if err != nil {
		t.Fatalf("running the corpus: %v", err)
	}
	for _, f := range failures {
		t.Errorf("%s", f)
	}
	if len(failures) > 0 {
		t.Log("If a rule change legitimately alters this output, regenerate the " +
			"fixtures with `make gen-fixtures` and update expectations.json as a " +
			"reviewed diff. Never regenerate it wholesale — that changes what " +
			"\"correct\" means instead of testing against it.")
		return
	}
	roots, diagnostics, err := Corpus(expectationsPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d roots, %d diagnostics, no drift", roots, diagnostics)
}

// TestCorpusIsNotVacuous guards the failure mode that makes every other
// assertion here worthless: a corpus that silently shrinks to nothing still
// passes, because zero roots produce zero mismatches. freeze.py did exactly
// this — it swallowed a missing-oracle error and rewrote all 128 roots to
// expect no findings, exiting 0. The floors are deliberately well below the
// current numbers, so ordinary corpus growth or pruning does not trip them,
// but a collapse does.
func TestCorpusIsNotVacuous(t *testing.T) {
	roots, diagnostics, err := Corpus(expectationsPath)
	if err != nil {
		t.Fatal(err)
	}
	const (
		minRoots       = 100
		minDiagnostics = 500
	)
	if roots < minRoots {
		t.Errorf("corpus records %d roots, expected at least %d — it has collapsed, "+
			"and a collapsed corpus passes every check while testing nothing", roots, minRoots)
	}
	if diagnostics < minDiagnostics {
		t.Errorf("corpus records %d diagnostics, expected at least %d — see above",
			diagnostics, minDiagnostics)
	}

	manifestRoots, err := loadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	// Every manifest entry must have an expectation. Check() reports a missing
	// one as a failure, but only for roots the manifest still lists; a root
	// dropped from both would vanish without trace.
	if len(manifestRoots) != roots {
		t.Errorf("manifest lists %d roots but expectations record %d; "+
			"a root present in only one of the two is untested",
			len(manifestRoots), roots)
	}
}

// TestCorpusDetectsDrift proves the corpus can fail. A regression suite that
// cannot go red is indistinguishable from one that is passing, and this is the
// only test here that would notice if compare() were gutted.
func TestCorpusDetectsDrift(t *testing.T) {
	real, err := loadExpectations(expectationsPath)
	if err != nil {
		t.Fatal(err)
	}
	// Pick a root with diagnostics to perturb; an empty one cannot show a miss.
	var key string
	for k, v := range real {
		if len(v.Diagnostics) > 0 {
			if key == "" || k < key { // deterministic pick
				key = k
			}
		}
	}
	if key == "" {
		t.Fatal("no root with diagnostics to perturb")
	}

	for _, tc := range []struct {
		name   string
		mutate func(Expectation) Expectation
		want   string
	}{
		{
			name: "dropped diagnostic is reported as unexpected",
			mutate: func(e Expectation) Expectation {
				e.Diagnostics = e.Diagnostics[1:]
				return e
			},
			want: "unexpected:",
		},
		{
			name: "invented diagnostic is reported as missing",
			mutate: func(e Expectation) Expectation {
				e.Diagnostics = append(append([]Diagnostic{}, e.Diagnostics...),
					Diagnostic{Code: "SDD999", Path: "nope.md", Line: 42, Severity: "error"})
				return e
			},
			want: "missing:",
		},
		{
			name: "flipped severity is reported",
			mutate: func(e Expectation) Expectation {
				d := append([]Diagnostic{}, e.Diagnostics...)
				d[0].Severity = "candidate"
				e.Diagnostics = d
				return e
			},
			want: "severity:",
		},
		{
			name: "wrong exit status is reported",
			mutate: func(e Expectation) Expectation {
				e.Exit = 99
				return e
			},
			want: "exit status:",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writePerturbed(t, dir, real, key, tc.mutate)

			failures, err := Check(
				filepath.Join(dir, "MANIFEST"),
				filepath.Join(dir, "expectations.json"),
				absFixtures(t),
			)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if len(failures) == 0 {
				t.Fatalf("perturbing %s produced no failure; the corpus cannot detect drift", key)
			}
			joined := strings.Join(failures, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("failure does not mention %q:\n%s", tc.want, joined)
			}
		})
	}
}

// writePerturbed writes a one-root manifest and a matching expectations file
// with that root's recorded verdict mutated.
func writePerturbed(t *testing.T, dir string, real map[string]Expectation,
	key string, mutate func(Expectation) Expectation) {
	t.Helper()

	// The manifest is resolved relative to its own directory, so point the
	// entry back at the real fixture with an absolute path.
	entry := filepath.Join(absFixtures(t), key)
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST"), []byte(entry+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(dir, "expectations.json"),
		map[string]Expectation{key: mutate(real[key])}); err != nil {
		t.Fatal(err)
	}
}

func absFixtures(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(fixturesDir)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// FR-13 / FR-14 / DD-2: the corpus prepare step runs fixture SETUP commands
// through the bounded runner, so a hanging or over-producing setup command
// fails inside the runner's own bounds instead of stalling the package.
func TestCorpusPrepareUsesOwnedRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hanging and flooding probes are POSIX shell commands")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh unavailable: %v", err)
	}

	const cleanup = 500 * time.Millisecond

	for _, tc := range []struct {
		name     string
		commands [][]string
		policy   procexec.Policy
		want     procexec.Cause // CauseUnknown means the setup must succeed
	}{
		{
			name:     "ordinary setup succeeds",
			commands: [][]string{{"git", "init", "-q"}},
			policy:   procexec.Policy{Timeout: 30 * time.Second, Cleanup: cleanup},
		},
		{
			name:     "a hanging setup command is bounded by the deadline",
			commands: [][]string{{"sh", "-c", "sleep 30"}},
			policy:   procexec.Policy{Timeout: 300 * time.Millisecond, Cleanup: cleanup},
			want:     procexec.CauseDeadline,
		},
		{
			name:     "a flooding setup command is bounded by the machine limit",
			commands: [][]string{{"sh", "-c", "yes | head -c 5000000"}},
			policy:   procexec.Policy{Timeout: 30 * time.Second, Cleanup: cleanup, MachineLimit: 1 << 20},
			want:     procexec.CauseOverflow,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			done := make(chan error, 1)
			go func() { done <- runSetup(dir, tc.commands, tc.policy) }()

			// The bound under test is the runner's; this select only keeps a
			// missing bound from hanging the package, and its expiry is the
			// failure.
			select {
			case err := <-done:
				if tc.want == procexec.CauseUnknown {
					if err != nil {
						t.Fatalf("ordinary setup failed: %v", err)
					}
					return
				}
				if err == nil {
					t.Fatalf("setup %v succeeded; want a %s failure", tc.commands, tc.want)
				}
				var pe *procexec.Error
				if !errors.As(err, &pe) {
					t.Fatalf("setup error is not a procexec *Error: %T: %v", err, err)
				}
				if pe.Cause != tc.want {
					t.Fatalf("setup failed with cause %s, want %s: %v", pe.Cause, tc.want, err)
				}
				if !strings.Contains(err.Error(), tc.commands[0][0]) {
					t.Errorf("error does not name the command %q: %v", tc.commands[0][0], err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("setup %v did not return within 5s; the runner's bound (timeout %v + cleanup %v) was not enforced",
					tc.commands, tc.policy.Timeout, tc.policy.Cleanup)
			}
		})
	}
}
