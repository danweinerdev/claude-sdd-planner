package rules

import (
	"os"
	"path/filepath"
	"strings"
)

// Family: Validator._discover and _legacy_layouts — the two checks about the
// planning root itself rather than about any artifact in it.
//
// Both report with no artifact, because their subject is a directory. SDD001's
// subject may not exist at all.

// legacyStatusNames is every lifecycle status any artifact type uses, plus the
// task statuses — the directory names that betray a pre-frontmatter layout
// where status was encoded in the path.
func legacyStatusNames() map[string]bool {
	out := map[string]bool{}
	for _, values := range statusValues {
		for _, v := range values {
			out[strings.ToLower(v)] = true
		}
	}
	for _, v := range taskStatusValues {
		out[strings.ToLower(v)] = true
	}
	return out
}

func init() {
	Register(&Rule{
		Code: "SDD001", Severity: Error, PyFunc: "_discover",
		What: "the planning root is not a directory",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			if info, err := os.Stat(r.Dir); err == nil && info.IsDir() {
				return
			}
			emit(Diagnostic{
				// Python reports the root path itself as `path`; there is no
				// artifact to attribute this to.
				Code: "SDD001", Severity: Error, Path: r.Dir, Line: 1,
				Message:    "Planning root is not a directory.",
				Correction: "Pass an existing planning root with --root.",
			})
		},
		UnexampledReason: "the example harness always materializes a real " +
			"directory to hold Files, so a missing root cannot be expressed " +
			"as a fixture. Covered by TestPlanningRootMustBeADirectory.",
	})

	Register(&Rule{
		Code: "SDD008", Severity: Error, PyFunc: "_legacy_layouts",
		What: "an artifact directory contains a legacy status subfolder",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			legacy := legacyStatusNames()
			for _, dirname := range artifactDirs {
				directory := filepath.Join(r.Dir, dirname)
				entries, err := os.ReadDir(directory)
				if err != nil {
					continue
				}
				for _, child := range entries {
					if !child.IsDir() || !legacy[strings.ToLower(child.Name())] {
						continue
					}
					rel := dirname + "/" + child.Name()
					emit(Diagnostic{
						Code: "SDD008", Severity: Error, Path: rel, Line: 1,
						Message:    "Legacy status subfolder `" + rel + "` is invalid.",
						Correction: "Move artifacts to the type directory and keep lifecycle in frontmatter.",
					})
				}
			}
		},
		Bad: []Example{{Name: "legacy-status-subfolder", Files: map[string]string{
			"Research/ok.md":         validResearch,
			"Plans/active/README.md": validResearch,
		}}},
		Good: []Example{{Name: "no-status-subfolder", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})
}
