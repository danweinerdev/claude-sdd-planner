package rules

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every registered rule must carry at least one passing and one failing example.
// A rule with no failing example has never been shown to fire; a rule with no
// passing example has never been shown to stay quiet. This test is the reason
// examples cannot lag the port — adding a rule without them fails the build's
// test step immediately, rather than showing up as a silent gap later.
func TestEveryRuleHasGoodAndBadExamples(t *testing.T) {
	for _, r := range All() {
		t.Run(r.Code, func(t *testing.T) {
			if r.UnexampledReason != "" {
				// Exempt, but the reason must say what covers it instead.
				if !strings.Contains(r.UnexampledReason, "Test") {
					t.Errorf("%s is unexampled but its reason names no covering test: %q",
						r.Code, r.UnexampledReason)
				}
				return
			}
			if len(r.Bad) == 0 {
				t.Errorf("%s has no Bad example: nothing proves it ever fires", r.Code)
			}
			if len(r.Good) == 0 {
				t.Errorf("%s has no Good example: nothing proves it stays quiet", r.Code)
			}
			if r.What == "" {
				t.Errorf("%s has no What description", r.Code)
			}
			if r.PyFunc == "" {
				t.Errorf("%s does not name the sdd_validate.py function it was ported from", r.Code)
			}
			if r.Severity != Error && r.Severity != Candidate {
				t.Errorf("%s has invalid severity %q", r.Code, r.Severity)
			}
		})
	}
}

// Each Bad example must produce its rule's code, and each Good example must not.
// This is the substantive per-rule test: the registry gives it for free for every
// code, so a newly ported rule is exercised the moment it is registered.
func TestExamplesBehaveAsDeclared(t *testing.T) {
	for _, r := range All() {
		for _, ex := range r.Bad {
			t.Run(r.Code+"/bad/"+ex.Name, func(t *testing.T) {
				diags := runExample(t, ex)
				found := false
				for _, d := range diags {
					if d.Code == r.Code {
						found = true
						if ex.Line != 0 && d.Line != ex.Line {
							t.Errorf("%s reported line %d, example expects %d", r.Code, d.Line, ex.Line)
						}
					}
				}
				if !found {
					t.Errorf("bad example did not produce %s; got %v", r.Code, codesOf(diags))
				}
			})
		}
		for _, ex := range r.Good {
			t.Run(r.Code+"/good/"+ex.Name, func(t *testing.T) {
				for _, d := range runExample(t, ex) {
					if d.Code == r.Code {
						t.Errorf("good example produced %s at %s:%d — %s",
							r.Code, d.Path, d.Line, d.Message)
					}
				}
			})
		}
	}
}

// A rule must not depend on evaluation order: running the whole registry over a
// rule's own examples must produce the same finding as the rule alone would.
// Order dependence would make the parity comparison unstable.
func TestRunIsDeterministic(t *testing.T) {
	for _, r := range All() {
		for _, ex := range r.Bad {
			first := codesOf(runExample(t, ex))
			for i := 0; i < 3; i++ {
				again := codesOf(runExample(t, ex))
				if len(first) != len(again) {
					t.Fatalf("%s/%s: diagnostic count varies between runs (%d vs %d)",
						r.Code, ex.Name, len(first), len(again))
				}
				for j := range first {
					if first[j] != again[j] {
						t.Fatalf("%s/%s: diagnostic order varies between runs", r.Code, ex.Name)
					}
				}
			}
		}
	}
}

// setupEnv is the fixed author/committer identity and timestamp every
// Example.Setup git command runs under, so a commit's resulting SHA is
// reproducible across machines and runs and can be hardcoded into an
// example's Files content instead of computed at test time.
var setupEnv = []string{
	"GIT_AUTHOR_NAME=sdd-fixture", "GIT_AUTHOR_EMAIL=sdd-fixture@example.com",
	"GIT_COMMITTER_NAME=sdd-fixture", "GIT_COMMITTER_EMAIL=sdd-fixture@example.com",
	"GIT_AUTHOR_DATE=2024-01-01T00:00:00+0000", "GIT_COMMITTER_DATE=2024-01-01T00:00:00+0000",
	"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
}

func runExample(t *testing.T, ex Example) []Diagnostic {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range ex.Files {
		// {{REPO}} lets an example reference its own fixture root — needed for
		// a `Repository:` evidence label, which must equal the exact resolved
		// target repository root (t.TempDir() is different every run, so this
		// can't be a hardcoded path the way a commit SHA can be, per Setup's
		// fixed-identity determinism).
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
	root, err := LoadRoot(dir)
	if err != nil {
		t.Fatalf("LoadRoot: %v", err)
	}
	return Run(root)
}

func codesOf(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Code)
	}
	return out
}
