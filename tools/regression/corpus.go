// Package regression asserts that validation still produces exactly the
// diagnostics the committed corpus records for each fixture root.
//
// It runs as an ordinary Go test (corpus_test.go), so `go test ./...` covers it
// with everything else and there is no separate command to remember. The rules
// are invoked in-process rather than through the built binary, which removes
// the stale-artifact hazard a shell-out has: a corpus check that silently ran
// against yesterday's build would report a pass it did not earn.
//
// It began as a differential oracle comparing the Go validator against
// scripts/sdd_validate.py. That Python is gone, and with it the "parity"
// framing: there is no second implementation to agree with. What remains is
// more useful and more honest — a regression corpus. 128 fixture roots, each
// with the exact set of (code, path, line, severity) diagnostics the validator
// is expected to emit, generated from the rules' own Bad examples by
// `make gen-fixtures`.
//
// The corpus's value is breadth. `sdd validate` runs 126+ rules whose
// interactions no unit test spans; this is what catches a change to one rule
// that silently perturbs another's output. Every cross-cutting edit to Run()
// is checked against all 705 recorded diagnostics at once.
//
// expectations.json is the recorded answer and is never
// regenerated: rewriting it would change what "correct" means rather than test
// against it. When a rule change legitimately alters output, the fixture corpus
// is regenerated (`make gen-fixtures`) and the expectation edited deliberately,
// as a reviewed diff.
package regression

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/rules"
)

// Diagnostic is the identity of one finding. Message text is deliberately not
// compared: the recorded expectations never captured it (two codes interpolated
// CPython and PyYAML exception strings, which are not stable across versions),
// so asserting on text would be asserting on data that does not exist.
type Diagnostic struct {
	Code     string `json:"code"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
}

func (d Diagnostic) key() string {
	return fmt.Sprintf("%s\x00%s\x00%d", d.Code, d.Path, d.Line)
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s %s:%d (%s)", d.Code, d.Path, d.Line, d.Severity)
}

// Expectation is one root's recorded verdict.
type Expectation struct {
	Exit        int          `json:"exit"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// validateOutput is the subset of `sdd validate --json` this tool reads.
type validateOutput struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// setupEnv fixes identity and timestamp so a fixture commit's SHA is
// reproducible, matching internal/rules/rules_test.go's runExample. A fixture
// may hardcode a SHA only because of this.
var setupEnv = []string{
	"GIT_AUTHOR_NAME=sdd-fixture", "GIT_AUTHOR_EMAIL=sdd-fixture@example.com",
	"GIT_COMMITTER_NAME=sdd-fixture", "GIT_COMMITTER_EMAIL=sdd-fixture@example.com",
	"GIT_AUTHOR_DATE=2024-01-01T00:00:00+0000",
	"GIT_COMMITTER_DATE=2024-01-01T00:00:00+0000",
	"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
}

// Check validates every root in the manifest against its recorded expectation,
// returning one human-readable failure per regressed root.
func Check(manifestPath, expectationsPath, fixturesDir string) ([]string, error) {
	expectations, err := loadExpectations(expectationsPath)
	if err != nil {
		return nil, err
	}
	roots, err := loadManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	absFixtures, err := filepath.Abs(fixturesDir)
	if err != nil {
		return nil, err
	}

	scratch, err := os.MkdirTemp("", "sdd-regression-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)

	var failures []string
	for _, root := range roots {
		key, err := expectationKey(root, absFixtures)
		if err != nil {
			return nil, err
		}
		want, ok := expectations[key]
		if !ok {
			// A root with no recorded expectation proves nothing, and skipping
			// it silently would let a fixture drift out of coverage unnoticed.
			failures = append(failures, key+": no recorded expectation")
			continue
		}

		prepared, err := prepare(root, scratch)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		gotExit, got, err := validate(prepared)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		if diff := compare(want, gotExit, got); diff != "" {
			failures = append(failures, key+":\n"+diff)
		}
	}
	return failures, nil
}

// Corpus reports how many roots and diagnostics the expectations record, so a
// passing run can state its own breadth rather than just being silent.
func Corpus(expectationsPath string) (roots, diagnostics int, err error) {
	expectations, err := loadExpectations(expectationsPath)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range expectations {
		diagnostics += len(e.Diagnostics)
	}
	return len(expectations), diagnostics, nil
}

// compare reports the difference between the recorded and observed verdicts, or
// "" when they agree. Both directions matter: a diagnostic that disappeared is
// a rule that stopped firing, and one that appeared is a rule firing where it
// should not.
func compare(want Expectation, gotExit int, got []Diagnostic) string {
	var b strings.Builder

	if want.Exit != gotExit {
		fmt.Fprintf(&b, "    exit status: expected %d, got %d\n", want.Exit, gotExit)
	}

	wantByKey := map[string]Diagnostic{}
	for _, d := range want.Diagnostics {
		wantByKey[d.key()] = d
	}
	gotByKey := map[string]Diagnostic{}
	for _, d := range got {
		gotByKey[d.key()] = d
	}

	var missing, extra, severity []string
	for k, w := range wantByKey {
		g, ok := gotByKey[k]
		if !ok {
			missing = append(missing, "    - missing: "+w.String())
			continue
		}
		if g.Severity != w.Severity {
			severity = append(severity, fmt.Sprintf(
				"    ~ severity: %s %s:%d expected %q, got %q",
				w.Code, w.Path, w.Line, w.Severity, g.Severity))
		}
	}
	for k, g := range gotByKey {
		if _, ok := wantByKey[k]; !ok {
			extra = append(extra, "    + unexpected: "+g.String())
		}
	}

	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(severity)
	for _, group := range [][]string{missing, extra, severity} {
		for _, line := range group {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// prepare returns a root ready to validate, materializing it when it needs
// run-time work.
//
// A fixture carrying a SETUP script or a {{REPO}} placeholder cannot be
// validated where it sits: the first needs a real git repository, the second
// needs the absolute path of the directory it ends up in. Both are resolved by
// copying to scratch and finishing there, which leaves the committed corpus
// untouched and keeps every git-verifying rule inside the corpus rather than
// outside it.
func prepare(root, scratch string) (string, error) {
	setupPath := filepath.Join(root, "SETUP")
	_, setupErr := os.Stat(setupPath)
	hasSetup := setupErr == nil

	needsRepo, err := containsRepoPlaceholder(root)
	if err != nil {
		return "", err
	}
	if !hasSetup && !needsRepo {
		return root, nil
	}

	target := filepath.Join(scratch, filepath.Base(root)+"-"+hash(root))
	if err := copyTree(root, target); err != nil {
		return "", err
	}

	var commands [][]string
	preparedSetup := filepath.Join(target, "SETUP")
	if hasSetup {
		raw, err := os.ReadFile(preparedSetup)
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			commands = append(commands, strings.Split(line, "\t"))
		}
		// SETUP is harness metadata, not part of the planning root.
		if err := os.Remove(preparedSetup); err != nil {
			return "", err
		}
	}

	if needsRepo {
		if err := substituteRepo(target); err != nil {
			return "", err
		}
	}

	for _, argv := range commands {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = target
		cmd.Env = append(os.Environ(), setupEnv...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("setup %v failed: %v\n%s", argv, err, out)
		}
	}
	return target, nil
}

func containsRepoPlaceholder(root string) (bool, error) {
	found := false
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || found || !strings.HasSuffix(path, ".md") {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(raw), "{{REPO}}") {
			found = true
		}
		return nil
	})
	return found, err
}

func substituteRepo(target string) error {
	return filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !strings.Contains(string(raw), "{{REPO}}") {
			return nil
		}
		return os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), "{{REPO}}", target)), info.Mode())
	})
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, raw, info.Mode())
	})
}

// hash keeps two fixtures with the same basename from colliding in scratch.
// Several rules use the same example name (`missing-title`, `bad-status`), and
// an earlier Python version of this tool matched roots by basename, which
// silently compared a root against another rule's expectation.
func hash(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return fmt.Sprintf("%08x", h)
}

// validate runs the validator over one prepared root, in-process, and returns
// what `sdd validate --no-waivers --format json` would have reported.
//
// It mirrors cmdValidate's composition deliberately: rules.Run for the artifact
// rules plus FocusedDecisionLogs for the DLG family, which the CLI folds in the
// same way. Diverging here would mean the corpus tested something the tool does
// not actually do.
//
// Run, not RunWithWaivers: the corpus records the unexcused state. A fixture
// that declared an accepted exception would otherwise record fewer diagnostics
// than the rules produce, and the corpus would quietly stop testing them.
func validate(root string) (int, []Diagnostic, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return 0, nil, err
	}
	repoRoot := gitRoot(abs)
	if repoRoot == "" {
		repoRoot = abs
	}
	r, err := rules.LoadRootRepo(abs, repoRoot)
	if err != nil {
		return 0, nil, err
	}
	if len(r.Artifacts) == 0 {
		// The CLI treats this as a usage error and exits 2 without a document.
		return 2, nil, nil
	}

	diags := rules.Run(r)
	diags = append(diags, rules.FocusedDecisionLogs(r, false)...)
	rules.SortDiagnostics(diags)

	out := make([]Diagnostic, 0, len(diags))
	exit := 0
	for _, d := range diags {
		if d.Severity == rules.Error {
			exit = 1
		}
		out = append(out, Diagnostic{
			Code:     d.Code,
			Path:     strings.ReplaceAll(d.Path, "\\", "/"),
			Line:     d.Line,
			Severity: string(d.Severity),
		})
	}
	return exit, out, nil
}

// gitRoot walks up for a .git entry, matching how the CLI resolves the
// repository a planning root belongs to. The DLG history rules read that
// repository's log, so a root resolved to the wrong one reports different
// diagnostics.
func gitRoot(start string) string {
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func loadExpectations(path string) (map[string]Expectation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading expectations: %w", err)
	}
	var out map[string]Expectation
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parsing expectations: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("expectations file records no roots; refusing to pass vacuously")
	}
	return out, nil
}

func loadManifest(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	base := filepath.Dir(path)
	var roots []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Manifest entries are relative to the manifest's own directory, so the
		// corpus can live anywhere and still resolve identically.
		if !filepath.IsAbs(line) {
			line = filepath.Join(base, line)
		}
		roots = append(roots, line)
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("manifest lists no roots")
	}
	return roots, nil
}

// expectationKey is a root's manifest-relative path, which is how the recorded
// expectations are keyed — stable across working directories and across the
// scratch copies whose absolute path differs every run.
func expectationKey(root, fixturesDir string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(fixturesDir, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// writeJSONFile writes v as indented JSON. Used by the tests to build a
// perturbed expectations file.
func writeJSONFile(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
