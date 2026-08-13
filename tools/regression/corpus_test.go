package regression

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
