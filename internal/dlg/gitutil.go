package dlg

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// This validator talks to git directly rather than through internal/vcs.
//
// That mirrors the Python it ports, and it is the right call here for a
// second reason: a decision ledger may live in a repository that is not the
// planning root's — the whole point of an external DECISIONS.md — so there is
// no single vcs.Repo the caller could have resolved in advance. Every call is
// read-only.

// gitRoot returns the repository root containing path's directory, or "".
func gitRoot(path string) string {
	out, err := exec.Command("git", "-C", filepath.Dir(path), "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return root
	}
	return resolved
}

// gitText runs a read-only git command in root, or returns ok=false.
func gitText(root string, args ...string) (string, bool) {
	full := append([]string{"-C", root}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// sameDir compares two directories after resolving symlinks, matching
// Python's Path.resolve() comparison.
func sameDir(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}

var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
var fenceOpenRe = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")

// visibleBody ports visible_body(): comments stripped, fenced blocks removed
// entirely including their delimiters.
func visibleBody(value string) string {
	withoutComments := htmlCommentRe.ReplaceAllString(value, "")
	var lines []string
	fenceChar := byte(0)
	fenceLen := 0
	inFence := false
	for _, line := range strings.Split(withoutComments, "\n") {
		if !inFence {
			if m := fenceOpenRe.FindStringSubmatch(line); m != nil {
				fenceChar, fenceLen = m[1][0], len(m[1])
				inFence = true
				continue
			}
			lines = append(lines, line)
			continue
		}
		if isFenceClose(line, fenceChar, fenceLen) {
			inFence = false
		}
	}
	return strings.Join(lines, "\n")
}

// isFenceClose reports whether line closes a fence of at least fenceLen
// repetitions of fenceChar, with only trailing whitespace after it.
func isFenceClose(line string, fenceChar byte, fenceLen int) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == fenceChar {
		n++
	}
	if n < fenceLen {
		return false
	}
	return strings.TrimSpace(trimmed[n:]) == ""
}
