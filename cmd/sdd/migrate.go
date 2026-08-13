package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/compile"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
)

// cmdMigrate is the FR-47 upgrade path: bring a non-compliant artifact into
// schema compliance, inserting missing required structure from schema defaults
// and reporting every insertion.
//
// This is the answer to a project whose artifacts predate the schema, and it is
// deliberately a separate verb rather than a flag on `apply`. `apply` must keep
// refusing non-compliant structure — that refusal is the whole point of routing
// writes through a compiler. Upgrading is an explicit, auditable act performed
// once per artifact, not a mode ordinary edits run in.
type migrateOpts struct {
	DryRun      bool
	Diff        bool
	JSON        bool
	AllowFrozen bool
	NoStubs     bool
	Type        string
	All         bool
}

func cmdMigrate(target string, o migrateOpts) error {
	if o.All {
		return migrateAll(target, o.DryRun, o.JSON, o.AllowFrozen, !o.NoStubs)
	}
	if target == "" {
		return fmt.Errorf("migrate: expected an artifact path, or --all to sweep the planning root")
	}

	art, err := store.Read(target)
	if err != nil {
		return err
	}
	if !art.Exists {
		return fmt.Errorf("migrate: %s does not exist; migration upgrades existing artifacts", target)
	}
	doc := artifact.Parse(art.Source)

	resolvedType := o.Type
	if resolvedType == "" {
		t, ok := doc.FM("type")
		if !ok {
			return fmt.Errorf("migrate: %s has no `type:` frontmatter; it is not an SDD artifact", relPath(target))
		}
		resolvedType = strings.Trim(strings.TrimSpace(t), `"`)
	}
	s, err := schema.Load(resolvedType)
	if err != nil {
		return err
	}

	res := compile.Compile(s, art.Source, compile.Options{
		Today:        time.Now().Format("2006-01-02"),
		Existing:     doc,
		Upgrade:      true,
		AllowFrozen:  o.AllowFrozen,
		StubSections: !o.NoStubs,
	})

	rel := relPath(target)
	if o.JSON {
		// Perform the write before reporting it. The JSON branch used to
		// return here without ever reaching the WriteAtomic below, so
		// `migrate --json` reported `"wrote": true` while leaving the file
		// untouched — a caller scripting against it would record a migration
		// that never happened.
		wrote := false
		if res.OK() && !o.DryRun && res.Output != art.Source {
			if err := store.WriteAtomic(art.Path, res.Output); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}
			wrote = true
		}
		return writeJSON(struct {
			Path        string   `json:"path"`
			Type        string   `json:"type"`
			OK          bool     `json:"ok"`
			Added       []string `json:"added,omitempty"`
			Corrections []string `json:"corrections,omitempty"`
			Todos       []string `json:"todos,omitempty"`
			Refusals    []string `json:"refusals,omitempty"`
			Wrote       bool     `json:"wrote"`
		}{rel, resolvedType, res.OK(), res.Added, res.Corrections, res.Todos,
			refusalStrings(res), wrote})
	}

	fmt.Printf("%s (%s)\n", rel, resolvedType)
	for _, a := range res.Added {
		fmt.Printf("  + %s\n", a)
	}
	for _, c := range res.Corrections {
		fmt.Printf("  ~ %s\n", c)
	}
	for _, td := range res.Todos {
		fmt.Printf("  TODO %s\n", td)
	}

	if !res.OK() {
		fmt.Fprintf(os.Stderr, "  cannot migrate: %d unresolved violation(s)\n", len(res.Refusals))
		for _, r := range res.Refusals {
			fmt.Fprintf(os.Stderr, "    %s\n", r)
		}
		return &refusedError{n: len(res.Refusals)}
	}

	if res.Output == art.Source {
		fmt.Println("  already compliant; nothing to do")
		return nil
	}
	if o.Diff {
		fmt.Print(lineDiff(art.Source, res.Output))
	}
	if o.DryRun {
		fmt.Printf("  would write (%d insertions)\n", len(res.Added))
		return nil
	}
	// The resolved path, not the argument — see the note in apply.go.
	if err := store.WriteAtomic(art.Path, res.Output); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	fmt.Printf("  migrated; digest %s\n", store.Digest(res.Output)[:12])
	return nil
}

func refusalStrings(res *compile.Result) []string {
	var out []string
	for _, r := range res.Refusals {
		out = append(out, r.Code+": "+r.Message)
	}
	return out
}

// migrateAll sweeps every artifact under the planning root. Its value is the
// worklist it prints: what it fixed mechanically, and what a human or model must
// still supply. A sweep is not expected to reach zero outstanding items —
// substantive content cannot be synthesized, and pretending otherwise is how a
// migration silently launders a real gap into a placeholder.
func migrateAll(root string, dryRun, jsonOut, allowFrozen, stubSections bool) error {
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		root, err = store.FindPlanningRoot(wd)
		if err != nil {
			return fmt.Errorf("migrate --all: %w", err)
		}
	}
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("migrate --all: %w", err)
	}
	sort.Strings(files)

	type item struct {
		Path     string   `json:"path"`
		Type     string   `json:"type,omitempty"`
		Outcome  string   `json:"outcome"`
		Added    []string `json:"added,omitempty"`
		Todos    []string `json:"todos,omitempty"`
		Refusals []string `json:"refusals,omitempty"`
	}
	var items []item
	counts := map[string]int{}
	todoTally := map[string]int{}
	today := time.Now().Format("2006-01-02")

	for _, f := range files {
		art, err := store.Read(f)
		if err != nil || !art.Exists {
			continue
		}
		doc := artifact.Parse(art.Source)
		t, ok := doc.FM("type")
		it := item{Path: relPath(f)}
		if !ok {
			it.Outcome = "not-an-artifact"
			counts[it.Outcome]++
			items = append(items, it)
			continue
		}
		it.Type = strings.Trim(strings.TrimSpace(t), `"`)
		sc, err := schema.Load(it.Type)
		if err != nil {
			it.Outcome = "no-schema"
			counts[it.Outcome]++
			items = append(items, it)
			continue
		}
		res := compile.Compile(sc, art.Source, compile.Options{
			Today: today, Existing: doc, Upgrade: true,
			AllowFrozen: allowFrozen, StubSections: stubSections,
		})
		it.Added, it.Todos = res.Added, res.Todos
		it.Refusals = refusalStrings(res)
		switch {
		case !res.OK():
			it.Outcome = "blocked"
		case res.Output == art.Source:
			it.Outcome = "compliant"
		default:
			it.Outcome = "migrated"
			if !dryRun {
				if err := store.WriteAtomic(f, res.Output); err != nil {
					return fmt.Errorf("migrate --all: %w", err)
				}
			}
		}
		counts[it.Outcome]++
		for _, td := range it.Todos {
			todoTally[summarizeTodo(td)]++
		}
		items = append(items, it)
	}

	if jsonOut {
		return writeJSON(struct {
			Root   string         `json:"root"`
			DryRun bool           `json:"dry_run"`
			Counts map[string]int `json:"counts"`
			Todos  map[string]int `json:"todo_tally"`
			Items  []item         `json:"items"`
		}{relPath(root), dryRun, counts, todoTally, items})
	}

	fmt.Printf("%s — %d files\n\n", relPath(root), len(files))
	for _, k := range []string{"compliant", "migrated", "blocked", "no-schema", "not-an-artifact"} {
		if counts[k] > 0 {
			fmt.Printf("  %-16s %d\n", k, counts[k])
		}
	}
	if len(todoTally) > 0 {
		fmt.Printf("\noutstanding work a model or human must supply:\n")
		keys := make([]string, 0, len(todoTally))
		for k := range todoTally {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return todoTally[keys[i]] > todoTally[keys[j]] })
		for _, k := range keys {
			fmt.Printf("  %5d  %s\n", todoTally[k], k)
		}
	}
	blocked := 0
	for _, it := range items {
		if it.Outcome == "blocked" {
			blocked++
		}
	}
	if blocked > 0 {
		fmt.Printf("\n%d artifact(s) blocked — run `sdd migrate <path>` for detail\n", blocked)
	}
	return nil
}

// summarizeTodo collapses a per-artifact todo into a class, so the sweep reports
// "203 x ## Phase Completion Evidence" rather than 203 separate lines.
func summarizeTodo(td string) string {
	if i := strings.Index(td, " needs real content"); i > 0 {
		return td[:i] + " (needs content)"
	}
	if strings.HasPrefix(td, "frontmatter ") {
		rest := strings.TrimPrefix(td, "frontmatter ")
		if i := strings.Index(rest, " is required"); i > 0 {
			return "frontmatter " + rest[:i] + " (must be supplied)"
		}
	}
	return td
}
