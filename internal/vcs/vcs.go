// Package vcs is the seam between the validator's evidence rules and whatever
// version control a project actually uses.
//
// Completion evidence is anchored to a native SCM revision (D-0008), and the
// checks that verify it — is this revision real, is it an ancestor of HEAD, was
// the artifact committed, is the worktree clean — are the same questions in every
// VCS. Only the commands and the revision grammar differ. So the rules depend on
// this interface and never on git: git is one implementation of the seam, not the
// definition of it. `shared/vcs-detection.md` is the plugin's existing statement
// of the same idea, and this package is its executable form.
//
// Two properties are deliberate:
//
// Every operation returns (result, error) where an error means "could not
// determine", never "false". A validator that treats an unavailable VCS as a
// failed check would report a repository as invalid because git was missing,
// which is the opposite of useful — the Python validator emits an OPERATIONAL
// diagnostic in that case, and the rules need to be able to tell the difference.
//
// Nothing here shells through a shell. Arguments are passed as argv so a path
// containing a space or a quote cannot become a second command (NFR-06).
package vcs

import (
	"errors"
	"fmt"
)

// Kind names a detected VCS, using the exact labels shared/vcs-detection.md
// defines so the two cannot drift.
type Kind string

const (
	Git         Kind = "git"
	GitWorktree Kind = "git-worktree"
	GitBare     Kind = "git-bare"
	Perforce    Kind = "perforce"
	None        Kind = "none"
)

// ErrUnsupported means this adapter cannot answer the question at all — not that
// the answer is negative. A rule seeing this reports an operational condition
// rather than a validation failure.
var ErrUnsupported = errors.New("operation not supported by this VCS adapter")

// ErrNotFound means the revision or path does not exist in the repository. This
// IS a determinate answer and rules may treat it as a finding.
var ErrNotFound = errors.New("revision or path not found")

// Repo is one repository as the evidence rules need to see it.
//
// Implementations must be read-only: validation may not mutate artifact bytes,
// index state, worktrees, or configuration (NFR-05). An adapter that needs to
// write to answer a question should return ErrUnsupported instead.
type Repo interface {
	// Kind reports which VCS this is, for diagnostics and for rules that are
	// legitimately git-specific.
	Kind() Kind

	// Root is the absolute path of the repository root.
	Root() string

	// RevisionSyntaxValid reports whether s is well-formed as a revision
	// identifier in THIS VCS, without contacting the repository. Git wants a
	// full 40-hex object name; Perforce wants a changelist number. This is why
	// the grammar cannot live in the rules: the same evidence string is valid in
	// one VCS and malformed in another.
	RevisionSyntaxValid(s string) bool

	// RevisionExists reports whether the revision resolves to a commit-like
	// object. ErrNotFound when it does not resolve.
	RevisionExists(rev string) (bool, error)

	// Head is the current revision of the checked-out state.
	Head() (string, error)

	// IsAncestor reports whether ancestor is reachable from descendant.
	IsAncestor(ancestor, descendant string) (bool, error)

	// Parents lists a revision's parent revisions, in order. A merge has more
	// than one; the first is the mainline parent. Used to enforce that a task
	// revision is non-merge and to locate the base of a task diff.
	Parents(rev string) ([]string, error)

	// FileAt returns a path's contents as of a revision. ErrNotFound when the
	// path did not exist there — which is itself the finding for "was this
	// artifact actually committed".
	FileAt(rev, relPath string) ([]byte, error)

	// ChangedPaths lists the repository-relative paths a revision touched.
	ChangedPaths(rev string) ([]string, error)

	// RevisionsAfter lists revisions reachable from Head but not from rev, so a
	// rule can tell whether work landed after a frozen review.
	RevisionsAfter(rev string) ([]string, error)

	// Clean reports whether the working state has no uncommitted modifications,
	// including untracked files. The second return names what was dirty, for the
	// diagnostic.
	Clean() (bool, []string, error)
}

// Detect identifies the VCS owning dir and returns an adapter for it. It never
// returns nil: an undetectable or unsupported directory yields the None adapter,
// whose operations all report ErrUnsupported, so callers need no nil check and
// cannot accidentally treat "no VCS" as "checks passed".
func Detect(dir string) Repo {
	for _, probe := range probes {
		if r := probe(dir); r != nil {
			return r
		}
	}
	return NoRepo{Dir: dir}
}

// probes are consulted in order; the first non-nil result wins. Adapters
// register here from their own files so adding a VCS touches no shared code.
var probes []func(dir string) Repo

// RegisterProbe adds a detection probe. Called from adapter init functions.
func RegisterProbe(p func(dir string) Repo) { probes = append(probes, p) }

// NoRepo is the adapter for a directory under no version control. Every history
// operation reports ErrUnsupported rather than a negative answer, because
// "there is no history here" and "this revision is wrong" are different facts
// and the rules must not conflate them.
type NoRepo struct{ Dir string }

func (n NoRepo) Kind() Kind                      { return None }
func (n NoRepo) Root() string                    { return n.Dir }
func (n NoRepo) RevisionSyntaxValid(string) bool { return false }
func (n NoRepo) RevisionExists(string) (bool, error) {
	return false, fmt.Errorf("%w: no VCS detected at %s", ErrUnsupported, n.Dir)
}
func (n NoRepo) Head() (string, error) {
	return "", fmt.Errorf("%w: no VCS detected at %s", ErrUnsupported, n.Dir)
}
func (n NoRepo) IsAncestor(string, string) (bool, error) {
	return false, fmt.Errorf("%w: no VCS detected at %s", ErrUnsupported, n.Dir)
}
func (n NoRepo) Parents(string) ([]string, error) {
	return nil, fmt.Errorf("%w: no VCS detected at %s", ErrUnsupported, n.Dir)
}
func (n NoRepo) FileAt(string, string) ([]byte, error) {
	return nil, fmt.Errorf("%w: no VCS detected at %s", ErrUnsupported, n.Dir)
}
func (n NoRepo) ChangedPaths(string) ([]string, error) {
	return nil, fmt.Errorf("%w: no VCS detected at %s", ErrUnsupported, n.Dir)
}
func (n NoRepo) RevisionsAfter(string) ([]string, error) {
	return nil, fmt.Errorf("%w: no VCS detected at %s", ErrUnsupported, n.Dir)
}
func (n NoRepo) Clean() (bool, []string, error) {
	return false, nil, fmt.Errorf("%w: no VCS detected at %s", ErrUnsupported, n.Dir)
}
