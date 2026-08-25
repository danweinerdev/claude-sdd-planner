package vcs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func init() {
	RegisterProbe(probeP4)
}

// p4Changelist is Perforce's revision grammar: a bare changelist number.
// "default" (the unsubmitted default changelist) is not a revision — only a
// submitted numbered changelist identifies a point in history.
var p4Changelist = regexp.MustCompile(`^[0-9]+$`)

// p4Repo implements Repo for a Perforce client workspace, per the operations
// table in shared/vcs-detection.md. Perforce has no faithful equivalent for
// several git-shaped operations (ancestry-by-graph, working-tree "clean"
// including opened-for-edit binaries, first-parent of a changelist); those
// return ErrUnsupported rather than approximate an answer, per the package
// contract that a wrong answer is worse than an honest "cannot determine".
type p4Repo struct{ root string }

// probeP4 implements shared/vcs-detection.md step 6: `p4 info` must succeed
// and dir must lie inside the client's root. The containment check is what
// makes the probe honest — `p4 info` succeeds anywhere the server is
// reachable and `p4 where //...` resolves the client view regardless of dir,
// so without it every directory on a machine with a configured P4 client
// "detected" as Perforce.
//
// SDD_VCS_DISABLE_P4 (any non-empty value) skips the probe entirely. `p4
// info` is a network RPC to whatever server the machine is configured for,
// paid on EVERY detection of a non-git directory before the containment
// check can reject it. The test suites run hundreds of validation passes
// over temp-dir fixture roots that can never be inside a client mapping, so
// on a workstation with a reachable Perforce server that probe alone
// dominated suite wall-clock (measured ~130-200ms per detect, thousands of
// detects). The hot test packages set it in TestMain; production never sets
// it, and no test asserts positive Perforce detection (that would need a
// live server, which CI does not have — the knob makes a P4-configured
// workstation behave like CI, not differently from it).
func probeP4(dir string) Repo {
	if os.Getenv("SDD_VCS_DISABLE_P4") != "" {
		return nil
	}
	out, err := runP4(dir, "info")
	if err != nil {
		return nil
	}
	clientRoot := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Client root:") {
			clientRoot = strings.TrimSpace(strings.TrimPrefix(line, "Client root:"))
			break
		}
	}
	if clientRoot == "" {
		return nil
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	// Canonicalize both sides: the client spec's root and the probed dir can
	// spell the same directory differently (8.3 short names, symlinks).
	if !pathWithin(CanonPath(clientRoot), CanonPath(absDir)) {
		return nil
	}
	return &p4Repo{root: dir}
}

// pathWithin reports whether dir equals root or lives beneath it, comparing
// case-insensitively because Perforce reports client roots with the casing
// the client spec was written in, not the filesystem's.
func pathWithin(root, dir string) bool {
	if len(dir) < len(root) || !strings.EqualFold(dir[:len(root)], root) {
		return false
	}
	return len(dir) == len(root) || dir[len(root)] == filepath.Separator || root[len(root)-1] == filepath.Separator
}

func runP4(dir string, args ...string) ([]byte, error) {
	full := append([]string{"-d", dir}, args...)
	cmd := exec.Command("p4", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("p4 %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (p *p4Repo) Kind() Kind   { return Perforce }
func (p *p4Repo) Root() string { return p.root }

func (p *p4Repo) RevisionSyntaxValid(s string) bool { return p4Changelist.MatchString(s) }

func (p *p4Repo) RevisionExists(rev string) (bool, error) {
	if !p.RevisionSyntaxValid(rev) {
		return false, fmt.Errorf("%w: not a changelist number: %s", ErrUnsupported, rev)
	}
	out, err := runP4(p.root, "describe", "-s", rev)
	if err != nil {
		return false, fmt.Errorf("%w: %s", ErrNotFound, rev)
	}
	if strings.Contains(string(out), "no such changelist") {
		return false, fmt.Errorf("%w: %s", ErrNotFound, rev)
	}
	return true, nil
}

func (p *p4Repo) Head() (string, error) {
	// Perforce has no single-branch HEAD; "current state" is the workspace's
	// have-revision, which is not a single repository-wide changelist number
	// the way HEAD is in git. Approximating one would misrepresent identity.
	return "", ErrUnsupported
}

func (p *p4Repo) IsAncestor(ancestor, descendant string) (bool, error) {
	// Perforce changelists are a linear, monotonically increasing sequence
	// (no merge graph to walk), so ancestry is numeric ordering — but this
	// adapter deliberately does not assume that equivalence for the rules,
	// which are written against a DAG ancestry question. Report honestly.
	return false, ErrUnsupported
}

func (p *p4Repo) Parents(rev string) ([]string, error) {
	// A changelist has no "parents" concept comparable to a git commit.
	return nil, ErrUnsupported
}

func (p *p4Repo) FileAt(rev, relPath string) ([]byte, error) {
	// `have` and `head` are Perforce's own symbolic file revisions: the
	// revision this client is synced to, and the depot tip. The durable
	// lifecycle adapter reads the planning artifact at #have — the state this
	// workspace is actually based on — the way the git adapter reads HEAD.
	if rev == "have" || rev == "head" {
		out, err := runP4(p.root, "print", "-q", fmt.Sprintf("%s#%s", relPath, rev))
		if err != nil {
			return nil, fmt.Errorf("%w: %s#%s", ErrNotFound, relPath, rev)
		}
		return out, nil
	}
	if !p.RevisionSyntaxValid(rev) {
		return nil, fmt.Errorf("%w: not a changelist number: %s", ErrUnsupported, rev)
	}
	out, err := runP4(p.root, "print", "-q", fmt.Sprintf("%s@%s", relPath, rev))
	if err != nil {
		return nil, fmt.Errorf("%w: %s@%s", ErrNotFound, relPath, rev)
	}
	return out, nil
}

// TrackedPaths and FileInIndex have no Perforce equivalent the append-only
// rules could rely on: p4 has no staged-index state, and enumerating a
// changelist's tree is not the same question. Returning ErrUnsupported makes
// the rules report an operational condition rather than a false finding.
func (p *p4Repo) TrackedPaths(string, []string) ([]string, error) {
	return nil, ErrUnsupported
}

func (p *p4Repo) FileInIndex(string) ([]byte, error) {
	return nil, ErrUnsupported
}

func (p *p4Repo) ChangedPaths(rev string) ([]string, error) {
	if !p.RevisionSyntaxValid(rev) {
		return nil, fmt.Errorf("%w: not a changelist number: %s", ErrUnsupported, rev)
	}
	out, err := runP4(p.root, "describe", "-s", rev)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, rev)
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, "#"); idx > 0 && strings.Contains(line, "//") {
			paths = append(paths, strings.TrimSpace(line[:idx]))
		}
	}
	return paths, nil
}

func (p *p4Repo) RevisionsAfter(rev string) ([]string, error) {
	// No faithful equivalent to "reachable from current state but not from
	// rev" without a defined branch/stream mapping this adapter cannot infer.
	return nil, ErrUnsupported
}

func (p *p4Repo) Clean() (bool, []string, error) {
	out, err := runP4(p.root, "opened")
	if err != nil {
		// `p4 opened` exits nonzero (via stderr "not opened on this client")
		// when nothing is checked out, which is the clean case, not a failure.
		if strings.Contains(err.Error(), "not opened") {
			return true, nil, nil
		}
		return false, nil, fmt.Errorf("p4 opened: %w", err)
	}
	var dirty []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			dirty = append(dirty, line)
		}
	}
	return len(dirty) == 0, dirty, nil
}
