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
// Two kinds of example are skipped rather than emitted broken:
//
//   - Examples with Setup steps need a live git repository built at test time.
//     A static directory cannot carry one, and committing a nested .git would
//     be worse.
//   - Examples containing {{REPO}} need the absolute path of the root they are
//     materialized into, which is only known at run time.
//
// Both are reported by count so the gap stays visible rather than silently
// shrinking the corpus. They remain covered by the Go-side registry test.
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
		skipGit  []string
		skipRepo []string
	)

	for _, r := range rules.All() {
		for _, ex := range r.Bad {
			if len(ex.Setup) > 0 {
				skipGit = append(skipGit, r.Code+"/"+ex.Name)
				continue
			}
			if usesRepoPlaceholder(ex) {
				skipRepo = append(skipRepo, r.Code+"/"+ex.Name)
				continue
			}
			name := ex.Name
			if name == "" {
				name = "bad"
			}
			root := filepath.Join(*out, r.Code, name)
			if err := writeRoot(root, ex.Files); err != nil {
				fatal("%s/%s: %v", r.Code, name, err)
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
	report("needs a git repository (Setup steps)", skipGit)
	report("needs {{REPO}} substitution", skipRepo)
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
	fmt.Printf("skipped %d example(s) — %s:\n", len(items), what)
	for _, it := range items {
		fmt.Printf("  %s\n", it)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genfixtures: "+format+"\n", args...)
	os.Exit(1)
}
