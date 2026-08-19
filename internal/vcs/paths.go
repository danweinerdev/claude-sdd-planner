package vcs

import "path/filepath"

// CanonPath returns the canonical spelling of an existing path: symlinks
// resolved and — on Windows — 8.3 short names expanded and the volume's
// stored case restored (filepath.EvalSymlinks does all three).
//
// Every rule that compares a path from one producer against a path from
// another needs both sides canonical. The producers genuinely disagree:
// os.TempDir/user input can hand out `C:\Users\DANIEL~1.WEI\...` (a short
// name, whenever TMP is spelled that way) while `git rev-parse
// --show-toplevel` reports `C:/Users/daniel.weiner/...`, and on macOS
// os.TempDir says `/tmp/...` while git resolves it to `/private/tmp/...`.
// A component-wise filepath.Rel or prefix comparison of two spellings of the
// same directory concludes the paths are unrelated, which silently disabled
// the git-history rules on such machines and reported artifacts as "outside"
// worktrees they are inside.
//
// A path that does not exist (or cannot be resolved) returns
// filepath.Clean(p) unchanged: canonicalization is a comparison aid, never an
// error source.
func CanonPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(p)
}
