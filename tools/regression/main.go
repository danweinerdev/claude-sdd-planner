// Command regression asserts that `sdd validate` still produces exactly the
// diagnostics the committed corpus records for each fixture root.
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
// tools/parity/frozen-expectations.json is the recorded answer and is never
// regenerated: rewriting it would change what "correct" means rather than test
// against it. When a rule change legitimately alters output, the fixture corpus
// is regenerated (`make gen-fixtures`) and the expectation edited deliberately,
// as a reviewed diff.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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

func main() {
	var (
		manifest = flag.String("manifest", "", "file listing one fixture root per line")
		frozen   = flag.String("frozen", "", "recorded expectations JSON")
		binary   = flag.String("binary", "", "path to the sdd binary")
		fixtures = flag.String("fixtures", "", "fixture corpus root, for manifest-relative keys")
		verbose  = flag.Bool("v", false, "print a line per root even when it passes")
	)
	flag.Parse()

	if *manifest == "" || *frozen == "" || *binary == "" {
		fmt.Fprintln(os.Stderr, "regression: --manifest, --frozen, and --binary are required")
		os.Exit(2)
	}
	if *fixtures == "" {
		*fixtures = filepath.Dir(*manifest)
	}

	if err := run(*manifest, *frozen, *binary, *fixtures, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "regression: %v\n", err)
		os.Exit(1)
	}
}

func run(manifestPath, frozenPath, binary, fixturesDir string, verbose bool) error {
	expectations, err := loadExpectations(frozenPath)
	if err != nil {
		return err
	}
	roots, err := loadManifest(manifestPath)
	if err != nil {
		return err
	}
	absFixtures, err := filepath.Abs(fixturesDir)
	if err != nil {
		return err
	}
	absBinary, err := filepath.Abs(binary)
	if err != nil {
		return err
	}

	scratch, err := os.MkdirTemp("", "sdd-regression-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)

	var failures []string
	checked, totalDiags := 0, 0

	for _, root := range roots {
		key, err := expectationKey(root, absFixtures)
		if err != nil {
			return err
		}
		want, ok := expectations[key]
		if !ok {
			// A root with no recorded expectation proves nothing, and silently
			// skipping it would let a fixture drift out of coverage unnoticed.
			failures = append(failures, fmt.Sprintf("%s: no recorded expectation", key))
			continue
		}

		prepared, err := prepare(root, scratch)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		gotExit, got, err := runValidate(absBinary, prepared)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}

		checked++
		totalDiags += len(want.Diagnostics)
		if diff := compare(want, gotExit, got); diff != "" {
			failures = append(failures, key+":\n"+diff)
		} else if verbose {
			fmt.Printf("  ok  %-60s %d diagnostics\n", key, len(want.Diagnostics))
		}
	}

	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d of %d roots regressed:\n\n", len(failures), len(roots))
		for _, f := range failures {
			fmt.Fprintln(os.Stderr, f)
		}
		fmt.Fprintln(os.Stderr,
			"\nIf a rule change legitimately alters this output, regenerate the corpus\n"+
				"with `make gen-fixtures` and update the expectation as a reviewed diff.\n"+
				"Never regenerate frozen-expectations.json wholesale — that changes what\n"+
				"\"correct\" means instead of testing against it.")
		return fmt.Errorf("%d root(s) regressed", len(failures))
	}

	fmt.Printf("regression: %d roots, %d diagnostics, no drift\n", checked, totalDiags)
	return nil
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

func runValidate(binary, root string) (int, []Diagnostic, error) {
	cmd := exec.Command(binary, "validate", "--root", root, "--format", "json",
		// The corpus records the unexcused state. A fixture that declared a
		// waiver would otherwise record fewer diagnostics than the rules
		// actually produce, and the corpus would stop testing the rules.
		"--no-waivers")
	out, err := cmd.Output()
	exit := 0
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if err != nil {
		return 0, nil, err
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return exit, nil, nil
	}
	var doc validateOutput
	if err := json.Unmarshal(out, &doc); err != nil {
		return exit, nil, fmt.Errorf("parsing validate output: %w", err)
	}
	for i := range doc.Diagnostics {
		doc.Diagnostics[i].Path = strings.ReplaceAll(doc.Diagnostics[i].Path, "\\", "/")
	}
	return exit, doc.Diagnostics, nil
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
