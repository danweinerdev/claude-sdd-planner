// Command genfixtures materializes every rule's Bad examples into planning
// roots on disk, so the differential oracle in tools/parity has something that
// actually fires diagnostics to compare.
//
// Why this exists: the parity harness's green result is only worth what its
// inputs exercise. Run against a healthy planning root, both validators agree
// on zero diagnostics — a passing comparison that touches none of the ported
// rules. Every rule already carries a Bad example proving it fires (the
// registry meta-test in internal/rules enforces that), so those examples are
// the natural corpus: generating from them means the fixtures cannot drift
// from the rules, because they ARE the rules' own evidence.
//
// Output is one planning root per rule, at
// tools/parity/fixtures/<code>/<example>/. Each is a complete root — its own
// planning-config.json plus the example's files — because a diagnostic's path
// is reported relative to its root, and sharing one root across rules would
// make every fixture's expected paths depend on its neighbors.
//
// Two kinds of example cannot be committed as plain directories, and both are
// emitted with a sidecar telling the harness how to finish them at run time:
//
//   - Examples with Setup steps need a live git repository. Committing a
//     nested .git would be wrong, so the argv lines go in a SETUP file that
//     tools/parity replays after copying the root to a scratch directory.
//   - Examples containing {{REPO}} need the absolute path of the root they are
//     materialized into, which is only known once that copy exists. The
//     placeholder is left in the committed bytes and substituted then.
//
// Before this, both kinds were skipped outright, which meant every
// git-verifying rule (SDD072, SDD169-175) was invisible to the differential
// oracle — its green said nothing about exactly the rules whose logic is
// hardest to get right.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/rules"
)

func main() {
	out := flag.String("out", "tools/parity/fixtures", "directory to write fixture roots into")
	clean := flag.Bool("clean", true, "remove the output directory first")
	flag.Parse()

	if *clean {
		if err := os.RemoveAll(*out); err != nil {
			fatal("removing %s: %v", *out, err)
		}
	}

	var (
		written  int
		roots    []string
		withGit  []string
		withRepo []string
	)

	for _, r := range rules.All() {
		for _, ex := range r.Bad {
			name := ex.Name
			if name == "" {
				name = "bad"
			}
			root := filepath.Join(*out, r.Code, name)
			if err := writeRoot(root, ex.Files); err != nil {
				fatal("%s/%s: %v", r.Code, name, err)
			}
			if len(ex.Setup) > 0 {
				if err := writeSetup(root, ex.Setup); err != nil {
					fatal("%s/%s setup: %v", r.Code, name, err)
				}
				withGit = append(withGit, r.Code+"/"+name)
			}
			if usesRepoPlaceholder(ex) {
				withRepo = append(withRepo, r.Code+"/"+name)
			}
			roots = append(roots, root)
			written++
		}
	}

	sort.Strings(roots)
	if err := writeManifest(*out, roots); err != nil {
		fatal("writing manifest: %v", err)
	}

	fmt.Printf("wrote %d fixture roots to %s\n", written, *out)
	report("carry a SETUP script (prepared as a git repository at run time)", withGit)
	report("carry {{REPO}} (substituted at run time)", withRepo)
}

// usesRepoPlaceholder reports whether any file needs the run-time root path.
func usesRepoPlaceholder(ex rules.Example) bool {
	for _, content := range ex.Files {
		if strings.Contains(content, "{{REPO}}") {
			return true
		}
	}
	return false
}

// anchorArtifact is a valid artifact added to every fixture root that does not
// already carry one.
//
// It exists because the two validators disagree about a root whose only file
// has no frontmatter: Python refuses the whole root ("planning root contains
// no discoverable SDD artifacts") and never reaches its per-artifact rules,
// while Go inspects the file and reports the defect. That difference is about
// root discovery, not about the rule under test, so without an anchor a
// fixture for SDD002/004/005/006/007 would report a divergence that says
// nothing about the port's correctness.
//
// It is rules.AnchorArtifact — the same document the rules' own Good examples
// use — rather than a copy maintained here, so a schema change cannot leave
// the corpus anchored to a document that no longer validates clean. Anything
// the oracle reports therefore comes from the example under test.
var anchorArtifact = rules.AnchorArtifact

const anchorPath = "Research/_parity-anchor.md"

// writeRoot materializes one example as a self-contained planning root.
func writeRoot(root string, files map[string]string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	// Every fixture is its own planning root, so both validators resolve the
	// same boundary and report the same relative paths.
	cfg := filepath.Join(root, "planning-config.json")
	if err := os.WriteFile(cfg, []byte("{\n  \"planningRoot\": \".\"\n}\n"), 0o644); err != nil {
		return err
	}
	if _, taken := files[anchorPath]; !taken {
		p := filepath.Join(root, filepath.FromSlash(anchorPath))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(anchorArtifact), 0o644); err != nil {
			return err
		}
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// writeSetup records an example's Setup argv lines, one command per line with
// arguments tab-separated, so the harness can replay them without a shell.
// Tabs are safe as a separator because every argument here is a git argv token.
func writeSetup(root string, setup [][]string) error {
	var b strings.Builder
	b.WriteString("# Generated by tools/genfixtures — do not edit.\n")
	b.WriteString("# One argv per line, arguments separated by tabs. Replayed in\n")
	b.WriteString("# the prepared copy of this root before validation.\n")
	for _, args := range setup {
		b.WriteString(strings.Join(args, "\t") + "\n")
	}
	return os.WriteFile(filepath.Join(root, "SETUP"), []byte(b.String()), 0o644)
}

// writeManifest records the generated roots so the oracle can be handed the
// whole corpus without re-deriving the list, and so a reviewer can see its
// extent in one file.
//
// Entries are relative to the manifest's own directory rather than to the
// repository. That keeps the corpus location-independent: regenerating into a
// scratch directory to check for drift must produce byte-identical output, and
// an absolute or repo-relative path would encode the output directory into
// every line and make that comparison always fail.
func writeManifest(out string, roots []string) error {
	var b strings.Builder
	b.WriteString("# Generated by tools/genfixtures — do not edit.\n")
	b.WriteString("# One planning root per line, relative to this file's directory.\n")
	for _, r := range roots {
		rel, err := filepath.Rel(out, r)
		if err != nil {
			return err
		}
		b.WriteString(filepath.ToSlash(rel) + "\n")
	}
	return os.WriteFile(filepath.Join(out, "MANIFEST"), []byte(b.String()), 0o644)
}

func report(what string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("%d example(s) %s:\n", len(items), what)
	for _, it := range items {
		fmt.Printf("  %s\n", it)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genfixtures: "+format+"\n", args...)
	os.Exit(1)
}
