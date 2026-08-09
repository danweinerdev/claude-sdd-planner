package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/compile"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
)

func cmdApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dryRun := fs.Bool("dry-run", false, "print what would be written and write nothing")
	diff := fs.Bool("diff", false, "show a line diff against the artifact on disk")
	create := fs.Bool("create", false, "treat the target as new even if it exists")
	jsonOut := fs.Bool("json", false, "emit the result as JSON")
	retire := fs.String("retire", "", "comma-separated identifiers being deliberately retired")
	expect := fs.String("expect", "", "refuse unless the artifact's current digest equals this value (FR-48)")
	typ := fs.String("type", "spec", "artifact type schema to compile against")

	flags, positional := splitArgs(args, map[string]bool{
		"-retire": true, "--retire": true,
		"-type": true, "--type": true,
		"-expect": true, "--expect": true,
	})
	if err := fs.Parse(flags); err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	var target string
	for _, a := range positional {
		if target != "" {
			return fmt.Errorf("apply: unexpected extra argument %q", a)
		}
		target = a
	}
	if target == "" {
		return fmt.Errorf("apply: expected an artifact path")
	}

	s, err := schema.Load(*typ)
	if err != nil {
		return err
	}

	payload, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("apply: reading payload from stdin: %w", err)
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		return fmt.Errorf("apply: empty payload on stdin")
	}

	art, err := store.Read(target)
	if err != nil {
		return err
	}

	// FR-48: isolation, not just atomicity. A caller that read the artifact in an
	// earlier turn passes the digest it saw; if another writer has landed since,
	// the write is refused rather than silently discarding their work.
	if *expect != "" && *expect != art.Digest {
		got := art.Digest
		if !art.Exists {
			got = "<absent>"
		}
		return &staleError{path: target, want: *expect, got: got}
	}

	opts := compile.Options{Today: time.Now().Format("2006-01-02"), Retire: map[string]bool{}}
	for _, id := range strings.Split(*retire, ",") {
		if id = strings.TrimSpace(id); id != "" {
			opts.Retire[id] = true
		}
	}
	if art.Exists && !*create {
		opts.Existing = artifact.Parse(art.Source)
	}

	res := compile.Compile(s, string(payload), opts)
	rel := relPath(target)

	if *jsonOut {
		return emitJSON(rel, art, res, *dryRun)
	}

	report(res)

	if !res.OK() {
		fmt.Fprintf(os.Stderr, "refused: %s (%d violation(s), nothing written)\n\n", rel, len(res.Refusals))
		for _, r := range res.Refusals {
			fmt.Fprintf(os.Stderr, "  %s\n", r)
		}
		return &refusedError{n: len(res.Refusals)}
	}

	unchanged := art.Exists && res.Output == art.Source

	if *diff || *dryRun {
		if unchanged {
			fmt.Printf("%s — no change (byte-idempotent)\n", rel)
		} else if *diff {
			fmt.Printf("would write %s:\n%s", rel, lineDiff(art.Source, res.Output))
		}
	}
	if *dryRun {
		if !unchanged && !*diff {
			fmt.Print(res.Output)
		}
		return nil
	}

	if unchanged {
		fmt.Printf("%s unchanged; digest %s\n", rel, art.Digest[:12])
		return nil
	}
	if err := store.WriteAtomic(target, res.Output); err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	fmt.Printf("wrote %s; digest %s\n", rel, store.Digest(res.Output)[:12])
	return nil
}

type staleError struct {
	path, want, got string
}

func (e *staleError) Error() string {
	return fmt.Sprintf(
		"apply: %s changed since it was read (expected digest %s, found %s)\n"+
			"    fix: re-read the artifact, reapply your edit to the current content, and retry",
		e.path, short(e.want), short(e.got))
}

func short(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}

func report(res *compile.Result) {
	sections := []struct {
		label  string
		marker string
		items  []string
	}{
		{"corrections", "~", res.Corrections},
		{"allocated", "+", res.Allocations},
		{"retired", "-", res.Retired},
		{"notes", "i", res.Notes},
	}
	for _, sec := range sections {
		if len(sec.items) == 0 {
			continue
		}
		fmt.Printf("%s (%d):\n", sec.label, len(sec.items))
		for _, it := range sec.items {
			fmt.Printf("  %s %s\n", sec.marker, it)
		}
		fmt.Println()
	}
}

func emitJSON(rel string, art *store.Artifact, res *compile.Result, dryRun bool) error {
	type refusal struct {
		Code       string `json:"code"`
		Line       int    `json:"line,omitempty"`
		Message    string `json:"message"`
		Correction string `json:"correction,omitempty"`
	}
	out := struct {
		Path        string    `json:"path"`
		OK          bool      `json:"ok"`
		DryRun      bool      `json:"dry_run"`
		Wrote       bool      `json:"wrote"`
		Unchanged   bool      `json:"unchanged"`
		Digest      string    `json:"digest,omitempty"`
		Corrections []string  `json:"corrections,omitempty"`
		Allocations []string  `json:"allocations,omitempty"`
		Retired     []string  `json:"retired,omitempty"`
		Notes       []string  `json:"notes,omitempty"`
		Refusals    []refusal `json:"refusals,omitempty"`
	}{
		Path: rel, OK: res.OK(), DryRun: dryRun,
		Corrections: res.Corrections, Allocations: res.Allocations,
		Retired: res.Retired, Notes: res.Notes,
	}
	for _, r := range res.Refusals {
		out.Refusals = append(out.Refusals, refusal{r.Code, r.Line, r.Message, r.Correction})
	}
	if res.OK() {
		out.Unchanged = art.Exists && res.Output == art.Source
		out.Digest = store.Digest(res.Output)
		if !dryRun && !out.Unchanged {
			if err := store.WriteAtomic(art.Path, res.Output); err != nil {
				return fmt.Errorf("apply: %w", err)
			}
			out.Wrote = true
		}
	}
	if err := writeJSON(out); err != nil {
		return err
	}
	if !res.OK() {
		return &refusedError{n: len(res.Refusals)}
	}
	return nil
}
