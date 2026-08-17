package main

import (
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

// applyOpts is what `sdd apply` needs from the command line. Cobra binds
// these fields directly, so the flag set is declared once rather than twice.
type applyOpts struct {
	DryRun    bool
	Diff      bool
	Create    bool
	Supersede bool
	Quiet     bool
	JSON      bool
	Retire    string
	Expect    string
	Type      string
}

func cmdApply(target string, o applyOpts) error {
	// Argument validation runs before anything is read. Both of these checks
	// used to sit after the stdin read, so `--supersede --create` and
	// superseding a missing artifact both surfaced as "empty payload on
	// stdin" when invoked without a payload — the wrong reason, and one that
	// sends the caller looking at their pipe instead of their flags.
	if o.Supersede && o.Create {
		return fmt.Errorf("apply: --supersede and --create are mutually exclusive; " +
			"--create starts a new artifact, --supersede rewrites an existing one")
	}
	if o.Supersede {
		if existing, err := store.Read(target); err == nil && !existing.Exists {
			return fmt.Errorf("apply --supersede: %s does not exist; "+
				"there is nothing to supersede (use --create for a new artifact)", relPath(target))
		}
	}

	s, err := schema.Load(o.Type)
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
	if o.Expect != "" && !digestMatches(o.Expect, art.Digest) {
		got := art.Digest
		if !art.Exists {
			got = "<absent>"
		}
		return &staleError{path: target, want: o.Expect, got: got}
	}

	opts := compile.Options{Today: time.Now().Format("2006-01-02"), Retire: map[string]bool{}, Supersede: o.Supersede}
	for _, id := range strings.Split(o.Retire, ",") {
		if id = strings.TrimSpace(id); id != "" {
			opts.Retire[id] = true
		}
	}
	if art.Exists && !o.Create {
		opts.Existing = artifact.Parse(art.Source)
	}

	res := compile.Compile(s, string(payload), opts)
	rel := relPath(art.Path)

	if o.JSON {
		return emitJSON(rel, art, res, o.DryRun)
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

	if o.Diff || o.DryRun {
		if unchanged {
			fmt.Printf("%s — no change (byte-idempotent)\n", rel)
		} else if o.Diff {
			fmt.Printf("would write %s:\n%s", rel, lineDiff(art.Source, res.Output))
		}
	}
	if o.DryRun {
		if !unchanged && !o.Diff {
			// Dumping the whole would-be document scrolled the verdict and
			// report off screen for long artifacts; --quiet keeps the dry run
			// to its answer (the report above plus this one line).
			if o.Quiet {
				fmt.Printf("dry-run: would write %s (%d lines); use --diff or drop --quiet to see the content\n",
					rel, strings.Count(res.Output, "\n"))
			} else {
				fmt.Print(res.Output)
			}
		}
		return nil
	}

	if unchanged {
		fmt.Printf("%s unchanged; digest %s\n", rel, art.Digest[:12])
		return nil
	}
	// Write to the path that was actually read. store.Read resolves a
	// planning-root-relative spelling (Specs/X/README.md) to its real
	// location (.plans/Specs/X/README.md); writing back to the unresolved
	// argument sends the output somewhere else entirely — creating a shadow
	// file at the literal path while the artifact just read stays unchanged.
	if err := store.WriteAtomicExpecting(art.Path, res.Output, art.Digest); err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	fmt.Printf("wrote %s; digest %s\n", rel, store.Digest(res.Output)[:12])
	return nil
}

// digestMatches accepts the full digest or any prefix of at least 12 hex
// characters — the length `sdd show`'s text output prints. Requiring the
// full 64-character digest while the tool itself only ever displayed 12
// meant every round-trip through the text interface was refused, and the
// refusal truncated both values to the same 12 characters: "expected
// 77dcab6d082a, found 77dcab6d082a".
func digestMatches(expect, full string) bool {
	if len(expect) >= 12 && strings.HasPrefix(full, expect) {
		return true
	}
	return expect == full
}

type staleError struct {
	path, want, got string
}

func (e *staleError) Error() string {
	want, got := distinguishing(e.want, e.got)
	return fmt.Sprintf(
		"apply: %s changed since it was read (expected digest %s, found %s)\n"+
			"    fix: re-read the artifact, reapply your edit to the current content, and retry",
		e.path, want, got)
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
		{"carried forward", "=", res.Carried},
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
			if err := store.WriteAtomicExpecting(art.Path, res.Output, art.Digest); err != nil {
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

// distinguishing shortens two digests to a common length that still shows
// they differ. A fixed 12-character truncation produced the worst possible
// message when two digests shared a long prefix:
//
//	expected 2900a4afce7b, found 2900a4afce7b
//
// — a mismatch error whose own evidence says the values are equal, which
// reads as a tool bug rather than the stale-read it actually reports. The
// length grows until the prefixes diverge, so the difference is always
// visible; identical inputs (which should never reach here) fall back to the
// full strings rather than silently claiming a difference.
func distinguishing(a, b string) (string, string) {
	const min = 12
	if a == b {
		return a, b
	}
	n := min
	for n < len(a) && n < len(b) && a[:n] == b[:n] {
		n += 4
	}
	return truncate(a, n), truncate(b, n)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
