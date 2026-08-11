package dlg

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Validate ports validate(): discover the ledger set, validate each, then the
// collection and (optionally) the git history.
//
// Diagnostics come back sorted by (path, line, code, message), matching the
// Python so the two can be compared directly.
func Validate(primary string, history bool) []Diagnostic {
	var out []Diagnostic
	var ledgers []*Ledger
	entriesByLedger := map[string][]map[string]any{}

	for _, path := range discover(primary) {
		l, diagnostics := ParseLedger(path)
		out = append(out, diagnostics...)
		if l == nil {
			continue
		}
		ledgers = append(ledgers, l)
		more, entries := ValidateLedger(l)
		out = append(out, more...)
		entriesByLedger[l.Path] = entries
	}

	var active []*Ledger
	for _, l := range ledgers {
		name := filepath.Base(l.Path)
		if str(l.Meta["status"]) == "active" && (name == "decisions.md" || name == "DECISIONS.md") {
			active = append(active, l)
		}
	}
	if len(active) > 1 {
		out = append(out, diag(active[0], "DLG043",
			"Multiple active canonical decision ledgers exist for one repository surface.",
			"Keep exactly the ledger selected by the planning-root location rule and merge retained history before removing the other.",
			1, "", Error))
	}
	if len(ledgers) > 0 && len(active) == 0 {
		out = append(out, diag(ledgers[0], "DLG045",
			"Ledger archives exist without one active canonical ledger.",
			"Restore `Decisions/decisions.md` or repository-root `DECISIONS.md` and keep archives as siblings.",
			1, "", Error))
	}

	out = append(out, ValidateCollection(ledgers, entriesByLedger)...)
	if history {
		out = append(out, ValidateHistory(primary, ledgers, entriesByLedger)...)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// discover ports discover(): the primary ledger plus the canonical and
// archive files that belong to the same surface.
func discover(primary string) []string {
	primary = absDir(primary)
	candidates := map[string]bool{primary: true}

	repository := gitRoot(primary)
	parent := filepath.Dir(primary)
	canonical := []string{
		filepath.Join(parent, "DECISIONS.md"),
		filepath.Join(parent, "decisions.md"),
	}
	if repository != "" {
		canonical = append(canonical,
			filepath.Join(repository, "DECISIONS.md"),
			filepath.Join(repository, "Decisions", "decisions.md"))
	}
	for _, p := range canonical {
		if isRegularFile(p) {
			candidates[absDir(p)] = true
		}
	}

	directories := map[string]bool{}
	for p := range candidates {
		directories[filepath.Dir(p)] = true
	}
	if repository != "" {
		directories[repository] = true
		directories[filepath.Join(repository, "Decisions")] = true
	}
	for dir := range directories {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), "archive-") || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			candidates[absDir(filepath.Join(dir, e.Name()))] = true
		}
	}
	return distinctFiles(candidates)
}

func isRegularFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// distinctFiles collapses paths naming the same file on disk and respells each
// to the name the directory actually holds.
//
// Discovery probes both `DECISIONS.md` and `decisions.md`; on a
// case-insensitive filesystem both resolve to whichever exists. Carrying the
// probed spelling forward would misjudge a canonical `decisions.md` as an
// external `DECISIONS.md` in the wrong directory, producing a false DLG042.
func distinctFiles(candidates map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	var keys []string
	for p := range candidates {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		if !isRegularFile(p) {
			continue
		}
		actual := onDiskName(p)
		key := actual
		if resolved, err := filepath.EvalSymlinks(actual); err == nil {
			key = resolved
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, actual)
	}
	sort.Strings(out)
	return out
}

// onDiskName respells a path to the filename its directory actually holds.
func onDiskName(path string) string {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return path
	}
	name := filepath.Base(path)
	var names []string
	for _, e := range entries {
		if e.Name() == name {
			return path
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	folded := strings.ToLower(name)
	for _, n := range names {
		if strings.ToLower(n) == folded {
			return filepath.Join(filepath.Dir(path), n)
		}
	}
	return path
}
