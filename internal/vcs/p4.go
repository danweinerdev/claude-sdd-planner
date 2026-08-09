package vcs

import (
	"bytes"
	"fmt"
	"os/exec"
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
// and `p4 where //...` must resolve at least one line inside dir.
func probeP4(dir string) Repo {
	if _, err := runP4(dir, "info"); err != nil {
		return nil
	}
	out, err := runP4(dir, "where", "//...")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return nil
	}
	return &p4Repo{root: dir}
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
	if !p.RevisionSyntaxValid(rev) {
		return nil, fmt.Errorf("%w: not a changelist number: %s", ErrUnsupported, rev)
	}
	out, err := runP4(p.root, "print", "-q", fmt.Sprintf("%s@%s", relPath, rev))
	if err != nil {
		return nil, fmt.Errorf("%w: %s@%s", ErrNotFound, relPath, rev)
	}
	return out, nil
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
