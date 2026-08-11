package dlg

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// History validation: the append-only guarantee.
//
// A decision that reached accepted, rejected, or superseded is history. It may
// not vanish, and an accepted entry may not have its substance rewritten — only
// its status advanced to superseded. Enforcing that needs three states: the
// committed baseline at HEAD, the staged index, and the worktree.
//
// The index matters on its own. A ledger edit staged but not committed is
// still an edit another writer will see when they allocate the next id, so
// DLG074 requires the worktree and index to agree about a new entry.

var archiveAnyRe = regexp.MustCompile(`^archive-.*\.md$`)

// entryRef is one decision entry and the file it was read from.
type entryRef struct {
	Path  string
	Entry map[string]any
}

// isLedgerFilename ports ledger_filename().
func isLedgerFilename(rel string) bool {
	name := filepath.Base(rel)
	if name == "DECISIONS.md" {
		return true
	}
	if name == "decisions.md" && filepath.Base(filepath.Dir(rel)) == "Decisions" {
		return true
	}
	return archiveAnyRe.MatchString(name)
}

// sourceEntries ports source_entries(): the decisions[] entries of a ledger
// source that are mappings with a string id.
func sourceEntries(source string) []map[string]any {
	lines := splitKeepends(source)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return nil
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "")), &node); err != nil {
		return nil
	}
	if len(node.Content) == 0 {
		return nil
	}
	meta, ok := nodeToMap(node.Content[0])
	if !ok {
		return nil
	}
	list, ok := meta["decisions"].([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, raw := range list {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := m["id"].(string); !ok {
			continue
		}
		out = append(out, m)
	}
	return out
}

// gitLedgerPaths ports git_ledger_paths(): ledger files directly inside one
// directory, at HEAD or in the index.
func gitLedgerPaths(root, directory, treeish string) ([]string, bool) {
	rel, err := filepath.Rel(root, directory)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, true // outside the repository is empty, not an error
	}
	rel = filepath.ToSlash(rel)

	var output string
	var ok bool
	if treeish == "HEAD" {
		output, ok = gitText(root, "ls-tree", "-r", "--name-only", "HEAD", "--", rel)
	} else {
		output, ok = gitText(root, "ls-files", "--", rel)
	}
	if !ok {
		return nil, false
	}
	seen := map[string]bool{}
	for _, value := range strings.Split(output, "\n") {
		if value == "" {
			continue
		}
		if filepath.ToSlash(filepath.Dir(value)) != rel || !isLedgerFilename(value) {
			continue
		}
		seen[value] = true
	}
	var result []string
	for p := range seen {
		result = append(result, p)
	}
	sort.Strings(result)
	return result, true
}

// compareRetained ports compare_retained(): what a retained entry is allowed
// to become between the committed baseline and a later surface.
func compareRetained(old, next map[string]entryRef, surface string) []Diagnostic {
	var out []Diagnostic
	var ids []string
	for id := range old {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, entryID := range ids {
		previous := old[entryID]
		status := str(previous.Entry["status"])
		if status != "accepted" && status != "rejected" && status != "superseded" {
			continue
		}
		match, found := next[entryID]
		if !found {
			out = append(out, diag(nil, "DLG070",
				"Retained decision `"+entryID+"` is missing from the "+surface+" state.",
				"Restore it; accepted, rejected, and superseded entries are append-only history.",
				1, previous.Path, Error))
			continue
		}
		if status == "rejected" || status == "superseded" {
			if !entriesEqual(match.Entry, previous.Entry) {
				out = append(out, diag(nil, "DLG073",
					"Historical decision `"+entryID+"` changed in the "+surface+" state after reaching `"+status+"`.",
					"Restore the retained entry; record a new decision for later changes.",
					1, match.Path, Error))
			}
			continue
		}
		// An accepted entry may advance its status and gain superseded_by;
		// everything else about it is immutable.
		fields := map[string]bool{}
		for f := range previous.Entry {
			if f != "status" && f != "superseded_by" {
				fields[f] = true
			}
		}
		for f := range match.Entry {
			if f != "status" && f != "superseded_by" {
				fields[f] = true
			}
		}
		var names []string
		for f := range fields {
			names = append(names, f)
		}
		sort.Strings(names)
		for _, field := range names {
			if !valuesEqual(previous.Entry[field], match.Entry[field]) {
				out = append(out, diag(nil, "DLG071",
					"Accepted decision `"+entryID+"` changed immutable field `"+field+"` in the "+surface+" state.",
					"Restore the accepted value and record a superseding decision instead.",
					1, match.Path, Error))
			}
		}
		nowStatus := str(match.Entry["status"])
		if nowStatus != "accepted" && nowStatus != "superseded" {
			out = append(out, diag(nil, "DLG072",
				"Accepted decision `"+entryID+"` changed status to `"+pyValue(match.Entry["status"])+"` in the "+surface+" state.",
				"Accepted entries may only remain accepted or become superseded.",
				1, match.Path, Error))
		}
	}
	return out
}

// ValidateHistory ports validate_history().
func ValidateHistory(primary string, ledgers []*Ledger, entriesByLedger map[string][]map[string]any) []Diagnostic {
	root := gitRoot(primary)
	if root == "" {
		return []Diagnostic{diag(nil, "DLG075", "Git-backed history validation is unavailable.",
			"Run inside the owning Git repository or use `--no-history` only for an explicitly unversioned audit.",
			1, primary, Operational)}
	}
	if _, ok := gitText(root, "rev-parse", "--verify", "HEAD"); !ok {
		// A repository with no commits has no baseline to compare against;
		// that is not a finding. Returning an empty (non-nil) slice keeps the
		// JSON encoding `[]` rather than `null`.
		return []Diagnostic{}
	}

	dirSet := map[string]bool{
		absDir(filepath.Dir(primary)):    true,
		root:                             true,
		filepath.Join(root, "Decisions"): true,
	}
	for _, l := range ledgers {
		dirSet[absDir(filepath.Dir(l.Path))] = true
	}
	var directories []string
	for d := range dirSet {
		directories = append(directories, d)
	}
	sort.Strings(directories)

	collect := func(treeish string) ([]string, bool) {
		seen := map[string]bool{}
		for _, d := range directories {
			paths, ok := gitLedgerPaths(root, d, treeish)
			if !ok {
				return nil, false
			}
			for _, p := range paths {
				seen[p] = true
			}
		}
		var out []string
		for p := range seen {
			out = append(out, p)
		}
		sort.Strings(out)
		return out, true
	}

	headPaths, ok := collect("HEAD")
	if !ok {
		return []Diagnostic{diag(nil, "DLG076", "Git failed while enumerating historical ledger files.",
			"Repair the repository/index and rerun validation.", 1, primary, Operational)}
	}

	var historyOut []Diagnostic
	old := map[string]entryRef{}
	for _, relative := range headPaths {
		source, ok := gitText(root, "show", "HEAD:"+relative)
		if !ok {
			return []Diagnostic{diag(nil, "DLG076",
				"Git could not read historical ledger `"+relative+"`.",
				"Repair the repository and rerun validation.", 1, filepath.Join(root, relative), Operational)}
		}
		for _, entry := range sourceEntries(source) {
			id := str(entry["id"])
			if _, dup := old[id]; dup {
				historyOut = append(historyOut, diag(nil, "DLG077",
					"Historical baseline contains duplicate decision id `"+id+"`.",
					"Preserve both entries, renumber the later one to the next free id, and update all links/citations before continuing.",
					1, filepath.Join(root, relative), Error))
				continue
			}
			old[id] = entryRef{Path: filepath.Join(root, relative), Entry: entry}
		}
	}

	worktree := map[string]entryRef{}
	for _, l := range ledgers {
		for _, entry := range entriesByLedger[l.Path] {
			if id, ok := entry["id"].(string); ok {
				worktree[id] = entryRef{Path: l.Path, Entry: entry}
			}
		}
	}

	indexPaths, ok := collect("index")
	if !ok {
		return []Diagnostic{diag(nil, "DLG076", "Git failed while enumerating staged ledger files.",
			"Repair the repository/index and rerun validation.", 1, primary, Operational)}
	}
	index := map[string]entryRef{}
	for _, relative := range indexPaths {
		source, ok := gitText(root, "show", ":"+relative)
		if !ok {
			return []Diagnostic{diag(nil, "DLG076",
				"Git could not read staged ledger `"+relative+"`.",
				"Repair the repository/index and rerun validation.", 1, filepath.Join(root, relative), Operational)}
		}
		for _, entry := range sourceEntries(source) {
			index[str(entry["id"])] = entryRef{Path: filepath.Join(root, relative), Entry: entry}
		}
	}

	out := historyOut
	out = append(out, compareRetained(old, index, "staged index")...)
	out = append(out, compareRetained(old, worktree, "worktree")...)

	var stagedIDs []string
	for id := range index {
		stagedIDs = append(stagedIDs, id)
	}
	sort.Strings(stagedIDs)
	for _, entryID := range stagedIDs {
		staged := index[entryID]
		if _, known := old[entryID]; known {
			continue
		}
		current, found := worktree[entryID]
		if !found || !entriesEqual(current.Entry, staged.Entry) || absDir(current.Path) != absDir(staged.Path) {
			out = append(out, diag(nil, "DLG074",
				"New staged decision `"+entryID+"` is absent or different in the worktree.",
				"Restore or restage the ledger so the worktree and index agree before another writer chooses an id.",
				1, staged.Path, Error))
		}
	}
	return out
}

func absDir(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// entriesEqual compares two decision entries the way Python's dict equality
// does: same keys, same values, recursively.
func entriesEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, present := b[k]
		if !present || !valuesEqual(av, bv) {
			return false
		}
	}
	return true
}

func valuesEqual(a, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !valuesEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		return ok && entriesEqual(av, bv)
	default:
		return a == b
	}
}
