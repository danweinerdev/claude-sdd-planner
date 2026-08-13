// Package provision implements FR-05/37/38/40: resolving an `sdd` binary,
// admitting it against the plugin's version floor, and placing the copy the
// hooks require.
//
// The plugin never builds or downloads anything (FR-05, FR-41). `go install`
// is the user's job, performed before the plugin runs; this package only finds
// what they installed, checks it is new enough, and puts it where hooks.json
// can reach it.
package provision

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

// Candidate is one resolved binary and why it was or was not admitted.
type Candidate struct {
	Path     string
	Version  string
	Admitted bool
	Reason   string
}

// Result is a provisioning outcome.
type Result struct {
	Source     string      // the binary that was resolved
	Version    string      // its reported version
	PluginCopy string      // where it was placed
	Refreshed  bool        // whether the copy was written this run
	Candidates []Candidate // every path considered, in order
}

// binaryName is `sdd` everywhere except Windows.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "sdd.exe"
	}
	return "sdd"
}

var errNoCandidate = errors.New("no sdd binary found")

// ErrNoCandidate reports that no admissible binary was found.
func ErrNoCandidate() error { return errNoCandidate }

// Resolve implements FR-05's ordered algorithm: the plugin-root copy first,
// then PATH. The first candidate at or above the floor wins.
//
// The order is not arbitrary. The plugin-root copy is what hooks actually
// execute, so preferring it means `sdd doctor` reports on the binary the hooks
// will really run rather than on a different one earlier in PATH.
func Resolve(pluginRoot, floor string) (Result, error) {
	var res Result
	name := binaryName()

	var paths []string
	if pluginRoot != "" {
		paths = append(paths, filepath.Join(pluginRoot, "bin", name))
	}
	if onPath, err := exec.LookPath("sdd"); err == nil {
		paths = append(paths, onPath)
	}

	for _, p := range paths {
		c := Candidate{Path: p}
		v, err := probeVersion(p)
		if err != nil {
			c.Reason = "cannot run: " + err.Error()
			res.Candidates = append(res.Candidates, c)
			continue
		}
		c.Version = v
		switch cmp, cmpErr := compareVersions(v, floor); {
		case cmpErr != nil:
			c.Reason = fmt.Sprintf("unparseable version %q", v)
		case cmp < 0:
			c.Reason = fmt.Sprintf("version %s is below the required floor %s", v, floor)
		default:
			c.Admitted = true
		}
		res.Candidates = append(res.Candidates, c)
		if c.Admitted {
			res.Source, res.Version = p, v
			return res, nil
		}
	}
	return res, errNoCandidate
}

// probeVersion runs `sdd version` and returns the reported version.
func probeVersion(path string) (string, error) {
	out, err := exec.Command(path, "version").Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 || fields[0] != "sdd" {
		return "", fmt.Errorf("unexpected `version` output %q", strings.TrimSpace(string(out)))
	}
	return fields[1], nil
}

// compareVersions orders two versions by their release cores only. A
// prerelease suffix is ignored for ordering, deliberately: the floor is about
// the CLI contract a binary implements, and a prerelease of a version
// implements it — `1.16.0-rc1` satisfies a `1.16.0` floor.
//
// This is why the comparison is not semver.Compare: strict semver orders a
// prerelease *below* its release (verified: semver.Compare("v1.16.0-rc1",
// "v1.16.0") == -1), which would reject exactly the binaries this floor means
// to admit. golang.org/x/mod/semver still does the parsing and validation —
// canonicalization, the "is this even a version" check, and the core
// extraction — so only the intended deviation is ours.
func compareVersions(a, b string) (int, error) {
	ca, err := releaseCore(a)
	if err != nil {
		return 0, err
	}
	cb, err := releaseCore(b)
	if err != nil {
		return 0, err
	}
	return semver.Compare(ca, cb), nil
}

// releaseCore canonicalizes a version and strips any prerelease/build suffix,
// yielding the `vMAJOR.MINOR.PATCH` form semver.Compare orders. It requires
// all three components: a bare `v1` is valid semver to x/mod but is not a
// version this toolchain ever publishes, and silently reading it as `1.0.0`
// would admit a binary on a guess.
func releaseCore(v string) (string, error) {
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", fmt.Errorf("not a MAJOR.MINOR.PATCH version: %q", v)
	}
	// Require all three components in the input: semver.Canonical expands `v1`
	// to `v1.0.0`, and admitting a binary on that expansion would bypass the
	// floor on a guess. Measure the release core only — a prerelease or build
	// suffix carries its own dots (`1.16.0+build.5`) and must not count.
	core := v
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	if strings.Count(core, ".") != 2 {
		return "", fmt.Errorf("not a MAJOR.MINOR.PATCH version: %q", v)
	}
	return semver.Canonical(core), nil
}

// Provision resolves a binary and refreshes the plugin-root copy (FR-37).
//
// The copy is unconditional on every run, and it is a copy rather than a
// symlink: hooks.json can interpolate only ${CLAUDE_PLUGIN_ROOT} and cannot
// resolve PATH, and Windows symlink creation needs elevated privilege. A hook
// trusting PATH would fail open silently while every skill kept working — a
// failure with no symptom, which is what this guards against.
func Provision(pluginRoot, floor string) (Result, error) {
	res, err := Resolve(pluginRoot, floor)
	if err != nil {
		return res, err
	}
	dest := filepath.Join(pluginRoot, "bin", binaryName())
	res.PluginCopy = dest

	if sameFile(res.Source, dest) {
		return res, nil // already the copy we resolved
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return res, fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
	}
	data, err := os.ReadFile(res.Source)
	if err != nil {
		return res, fmt.Errorf("reading %s: %w", res.Source, err)
	}
	// Write to a temporary file and rename, so a hook firing mid-refresh never
	// sees a truncated binary.
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return res, fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return res, fmt.Errorf("installing %s: %w", dest, err)
	}
	res.Refreshed = true
	return res, nil
}

func sameFile(a, b string) bool {
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ai, bi)
}

// Floor reads minSddVersion from a plugin manifest.
func Floor(pluginRoot string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(pluginRoot, ".claude-plugin", "plugin.json"))
	if err != nil {
		return "", err
	}
	var manifest struct {
		MinSddVersion string `json:"minSddVersion"`
		Version       string `json:"version"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return "", err
	}
	if manifest.MinSddVersion != "" {
		return manifest.MinSddVersion, nil
	}
	// A manifest with no declared floor admits any binary reporting a version.
	// Requiring exact equality with the plugin's own version is explicitly not
	// the rule (FR-38).
	return "0.0.0", nil
}

// InstallCommand is the exact command that satisfies the floor (FR-41).
func InstallCommand(floor string) string {
	return "go install github.com/danweinerdev/claude-sdd-planner/cmd/sdd@v" + floor
}
