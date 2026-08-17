// Command sdd, subcommand `decide`, is a focused reader/writer for the
// decision ledger (shared/decision-log.md): list, search, and add — with the
// append-time collision check from D-0003 (never auto-resolve, never settle
// by recency).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/compile"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
)

// decisionEntry mirrors the ledger's decisions[] entry schema
// (shared/decision-log.md § Entry Schema).
type decisionEntry struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind,omitempty"`
	Status        string   `json:"status"`
	Date          string   `json:"date,omitempty"`
	DecidedBy     string   `json:"decided_by,omitempty"`
	Statement     string   `json:"statement"`
	Question      string   `json:"question,omitempty"`
	Rejected      []string `json:"rejected,omitempty"`
	Rationale     string   `json:"rationale,omitempty"`
	Confirmation  string   `json:"confirmation,omitempty"`
	Scope         []string `json:"scope,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Supersedes    string   `json:"supersedes,omitempty"`
	SupersededBy  string   `json:"superseded_by,omitempty"`
	Reversibility string   `json:"reversibility,omitempty"`
}

func entryFromItem(it fmItem) decisionEntry {
	return decisionEntry{
		ID: it.Str("id"), Kind: it.Str("kind"), Status: it.Str("status"),
		Date: it.Str("date"), DecidedBy: it.Str("decided_by"),
		Statement: it.Str("statement"), Question: it.Str("question"),
		Rejected: it.List("rejected"), Rationale: it.Str("rationale"),
		Confirmation: it.Str("confirmation"), Scope: it.List("scope"),
		Tags: it.List("tags"), Supersedes: it.Str("supersedes"),
		SupersededBy: it.Str("superseded_by"), Reversibility: it.Str("reversibility"),
	}
}

// ledgerPath resolves <planning-root>/Decisions/decisions.md.
func ledgerPath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := store.FindPlanningRoot(wd)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "Decisions", "decisions.md"), nil
}

func loadLedger() (*artifact.Doc, string, error) {
	path, err := ledgerPath()
	if err != nil {
		return nil, "", err
	}
	art, err := store.Read(path)
	if err != nil {
		return nil, "", err
	}
	if !art.Exists {
		return nil, path, fmt.Errorf("decision ledger %s does not exist", path)
	}
	return artifact.Parse(art.Source), path, nil
}

func loadEntries(doc *artifact.Doc) []decisionEntry {
	var out []decisionEntry
	for _, it := range fmSequence(doc.FrontmatterRaw, "decisions") {
		out = append(out, entryFromItem(it))
	}
	return out
}

func cmdDecideList(status string, jsonOut bool) error {
	doc, _, err := loadLedger()
	if err != nil {
		return fmt.Errorf("decide list: %w", err)
	}
	entries := loadEntries(doc)
	var out []decisionEntry
	for _, e := range entries {
		if status != "" && e.Status != status {
			continue
		}
		out = append(out, e)
	}

	if jsonOut {
		return writeJSON(struct {
			Decisions []decisionEntry `json:"decisions"`
		}{out})
	}
	for _, e := range out {
		stmt := e.Statement
		if len(stmt) > 80 {
			stmt = stmt[:80] + "…"
		}
		fmt.Printf("%-8s %-10s %-10s %s\n", e.ID, e.Status, e.Date, stmt)
	}
	return nil
}

func cmdDecideSearch(term string, jsonOut bool) error {
	term = strings.ToLower(term)

	doc, _, err := loadLedger()
	if err != nil {
		return fmt.Errorf("decide search: %w", err)
	}
	var out []decisionEntry
	for _, e := range loadEntries(doc) {
		if entryMatches(e, term) {
			out = append(out, e)
		}
	}

	if jsonOut {
		return writeJSON(struct {
			Decisions []decisionEntry `json:"decisions"`
		}{out})
	}
	for _, e := range out {
		fmt.Printf("%-8s %-10s %s\n", e.ID, e.Status, e.Statement)
	}
	return nil
}

func entryMatches(e decisionEntry, term string) bool {
	haystacks := append([]string{e.Statement, e.Rationale}, e.Tags...)
	haystacks = append(haystacks, e.Scope...)
	for _, h := range haystacks {
		if strings.Contains(strings.ToLower(h), term) {
			return true
		}
	}
	return false
}

type decideAddOpts struct {
	Statement     string
	Rationale     string
	Rejected      string
	Scope         string
	Tags          string
	Supersedes    string
	Kind          string
	Reversibility string
	Accept        bool
	DryRun        bool
	JSON          bool
}

func cmdDecideAdd(o decideAddOpts) error {
	if strings.TrimSpace(o.Statement) == "" {
		return fmt.Errorf("decide add: --statement is required")
	}

	doc, path, err := loadLedger()
	if err != nil {
		return fmt.Errorf("decide add: %w", err)
	}
	entries := loadEntries(doc)

	// D-0003: a new entry that collides with an accepted one always stops for
	// the user unless --supersedes names the entry it resolves the collision
	// with. Never auto-resolve, never settle by recency.
	if o.Supersedes == "" {
		candidates := findCollisionCandidates(o.Statement, splitCSV(o.Scope), entries)
		if len(candidates) > 0 {
			fmt.Fprintln(os.Stderr, "decide add: refused — candidate collision(s) with accepted entries:")
			for _, c := range candidates {
				fmt.Fprintf(os.Stderr, "  %s: %s\n", c.ID, c.Statement)
			}
			fmt.Fprintln(os.Stderr, "pass --supersedes D-NNNN to resolve one of them, or rephrase --statement to avoid the overlap")
			return &refusedError{n: len(candidates)}
		}
	} else if target, ok := findEntry(entries, o.Supersedes); !ok {
		return fmt.Errorf("decide add: --supersedes %s does not name an existing entry", o.Supersedes)
	} else if target.Status == "superseded" {
		// Superseding an already-superseded entry forks the chain: the target
		// gains a second `superseded_by`, which is duplicate-key YAML and made
		// the ledger unparseable for every later `decide add`. Supersession is
		// one-step by design (shared/decision-log.md) — point at the live
		// successor instead.
		successor := target.SupersededBy
		if successor == "" {
			successor = "its recorded successor"
		}
		return fmt.Errorf("decide add: --supersedes %s is already superseded by %s; "+
			"supersession is one-step — supersede the live entry (%s) instead",
			o.Supersedes, successor, successor)
	}

	s, err := schema.Load("decision-log")
	if err != nil {
		return fmt.Errorf("decide add: %w", err)
	}
	ns := s.Namespace("D")
	if ns == nil {
		return fmt.Errorf("decide add: decision-log schema has no D namespace")
	}
	nextNum := 0
	for _, e := range entries {
		if _, n, ok := splitIdent(e.ID); ok && n > nextNum {
			nextNum = n
		}
	}
	nextNum++
	newID := ns.Format(nextNum)

	today := time.Now().Format("2006-01-02")
	status := "proposed"
	decidedBy := "agent"
	if o.Accept {
		status = "accepted"
		decidedBy = "user-approved"
	}

	newLines := renderEntry(decisionEntry{
		ID: newID, Kind: o.Kind, Status: status, Date: today, DecidedBy: decidedBy,
		Statement: o.Statement, Rejected: splitCSV(o.Rejected), Rationale: o.Rationale,
		Scope: splitCSV(o.Scope), Tags: splitCSV(o.Tags), Supersedes: o.Supersedes,
		Reversibility: o.Reversibility,
	})

	out, err := applyLedgerEdits(doc, today, newLines, o.Supersedes, newID)
	if err != nil {
		return err
	}

	if o.JSON {
		res := struct {
			Path    string `json:"path"`
			ID      string `json:"id"`
			DryRun  bool   `json:"dry_run"`
			Wrote   bool   `json:"wrote"`
			Refused bool   `json:"refused"`
			Output  string `json:"output,omitempty"`
		}{Path: relPath(path), ID: newID, DryRun: o.DryRun}
		if o.DryRun {
			res.Output = out
		} else {
			if err := store.WriteAtomic(path, out); err != nil {
				return fmt.Errorf("decide add: %w", err)
			}
			res.Wrote = true
		}
		return writeJSON(res)
	}

	if o.DryRun {
		fmt.Print(out)
		return nil
	}
	if err := store.WriteAtomic(path, out); err != nil {
		return fmt.Errorf("decide add: %w", err)
	}
	fmt.Printf("added %s to %s (status: %s)\n", newID, relPath(path), status)
	return nil
}

func findEntry(entries []decisionEntry, id string) (decisionEntry, bool) {
	for _, e := range entries {
		if e.ID == id {
			return e, true
		}
	}
	return decisionEntry{}, false
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// stopwords are filtered out of the term-overlap heuristic; they carry no
// disambiguating signal and would inflate every pair's score.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true, "of": true,
	"to": true, "and": true, "or": true, "in": true, "on": true, "for": true,
	"with": true, "by": true, "as": true, "be": true, "this": true, "that": true,
	"it": true, "at": true, "than": true, "over": true, "into": true,
}

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

func significantTokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		if len(w) < 3 || stopwords[w] {
			continue
		}
		out[w] = true
	}
	return out
}

// termOverlapScore is a stopword-filtered token-overlap heuristic — NOT
// comprehension. It reports |intersection| / |smaller set|, so two
// statements sharing most of the shorter one's vocabulary score high even
// when the longer statement adds unrelated detail.
// minOverlapTokens is the floor below which the ratio stops meaning anything.
// Dividing by the SMALLER token set makes a short statement match on almost
// nothing: a 2-token statement sharing one common word scores 0.50, over the
// scoped threshold, so a terse probe collided with nine unrelated entries and
// a legitimately brief decision could be unaddable without a spurious
// --supersedes. Below the floor, only a near-total overlap counts.
const minOverlapTokens = 4

func termOverlapScore(a, b string) float64 {
	ta, tb := significantTokens(a), significantTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	// A statement too short to be characterized by its vocabulary is compared
	// against the LARGER set instead, which is the honest question for it:
	// "is this substantially the same statement?" rather than "are its few
	// words present somewhere in that paragraph?".
	if len(ta) < minOverlapTokens || len(tb) < minOverlapTokens {
		shared := 0
		for w := range ta {
			if tb[w] {
				shared++
			}
		}
		larger := len(ta)
		if len(tb) > larger {
			larger = len(tb)
		}
		return float64(shared) / float64(larger)
	}
	shared := 0
	small := ta
	if len(tb) < len(ta) {
		small = tb
	}
	for w := range small {
		if ta[w] && tb[w] {
			shared++
		}
	}
	return float64(shared) / float64(len(small))
}

// scopeOverlaps: an empty/absent scope is global and overlaps everything;
// otherwise two scopes overlap when they share an entry or one path is a
// prefix of the other (shared/decision-log.md § Collision Detection — the
// `related`-frontmatter one-hop connectivity rule is out of scope for this
// mechanical proxy).
func scopeOverlaps(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, pa := range a {
		for _, pb := range b {
			if pa == pb || strings.HasPrefix(pa, pb) || strings.HasPrefix(pb, pa) {
				return true
			}
		}
	}
	return false
}

// termOverlapThreshold and scopedTermOverlapThreshold are heuristic
// thresholds, not proof of contradiction — the append-time check is a
// mechanical proxy for the judgment pass a human still owns (D-0003).
const (
	termOverlapThreshold       = 0.6 // strong statement similarity alone is a candidate
	scopedTermOverlapThreshold = 0.3 // weaker similarity is a candidate only alongside scope overlap
)

func findCollisionCandidates(statement string, scope []string, entries []decisionEntry) []decisionEntry {
	var out []decisionEntry
	for _, e := range entries {
		if e.Status != "accepted" {
			continue
		}
		score := termOverlapScore(statement, e.Statement)
		overlap := scopeOverlaps(scope, e.Scope)
		if score >= termOverlapThreshold || (overlap && score >= scopedTermOverlapThreshold) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// renderEntry renders one decisions[] item in the ledger's exact convention:
// 2-space dash indent, 4-space field indent, quoted statement/rationale,
// unquoted flow lists.
func renderEntry(e decisionEntry) []string {
	var l []string
	l = append(l, fmt.Sprintf("  - id: %s", e.ID))
	l = append(l, fmt.Sprintf("    kind: %s", e.Kind))
	l = append(l, fmt.Sprintf("    status: %s", e.Status))
	if e.Supersedes != "" {
		l = append(l, fmt.Sprintf("    supersedes: %s", e.Supersedes))
	}
	l = append(l, fmt.Sprintf("    date: %s", e.Date))
	l = append(l, fmt.Sprintf("    decided_by: %s", e.DecidedBy))
	l = append(l, fmt.Sprintf("    statement: %s", quoteYAML(e.Statement)))
	if e.Question != "" {
		l = append(l, fmt.Sprintf("    question: %s", quoteYAML(e.Question)))
	}
	l = append(l, fmt.Sprintf("    rejected: %s", flowList(e.Rejected)))
	l = append(l, fmt.Sprintf("    rationale: %s", quoteYAML(e.Rationale)))
	l = append(l, fmt.Sprintf("    scope: %s", flowList(e.Scope)))
	l = append(l, fmt.Sprintf("    tags: %s", flowList(e.Tags)))
	l = append(l, fmt.Sprintf("    reversibility: %s", e.Reversibility))
	return l
}

func quoteYAML(s string) string {
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func flowList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(items))
	for _, it := range items {
		quoted = append(quoted, flowScalar(it))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// flowScalar renders one flow-sequence item, quoting it when plain style
// would not round-trip. An unquoted item containing `: ` parses as a nested
// mapping, so `--rejected "re-render (vacuous: no changes)"` was written as
// YAML the ledger's own validator then rejected (DLG028). Brackets, braces,
// commas, and quotes are equally structural inside a flow sequence.
func flowScalar(s string) string {
	if s == "" {
		return `""`
	}
	needsQuote := strings.ContainsAny(s, `[]{},"'#&*!|>%@`+"`") ||
		strings.Contains(s, ": ") || strings.HasSuffix(s, ":") ||
		strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") ||
		strings.ContainsAny(s, "\n\r\t")
	if !needsQuote {
		return s
	}
	return quoteYAML(s)
}

// emptyListLine returns the index of a top-level `key: []` line (the empty
// form of a block sequence), or -1. A ledger that has never had an entry
// declares its list this way, and it must be converted into a block header
// rather than duplicated.
func emptyListLine(fm []string, key string) int {
	for i, l := range fm {
		if strings.TrimRight(l, " \t\r") == key+": []" {
			return i
		}
	}
	return -1
}

// applyLedgerEdits restamps `updated` to today, inserts the new entry's lines
// at the end of the decisions[] block, and — when supersedes names an
// existing entry — flips that entry's status and appends its superseded_by
// line.
//
// The frontmatter is re-rendered from its parsed form rather than copied, so
// the splice below operates on canonical YAML: a hand-written ledger and a
// tool-written one present the same shape to fmBlockBounds.
func applyLedgerEdits(doc *artifact.Doc, today string, newLines []string, supersedes, newID string) (string, error) {
	fm, ok := compile.RestampFrontmatter(doc, today)
	if !ok {
		return "", fmt.Errorf("decision ledger frontmatter cannot be modeled as YAML, so it cannot be rewritten safely")
	}

	start, end, found := fmBlockBounds(fm, "decisions")
	if !found {
		// An empty ledger declares `decisions: []` (the list's empty form,
		// what the template and schema emit). fmBlockBounds only recognizes a
		// bare `decisions:` header, so appending here added a SECOND
		// `decisions:` key and the duplicate made the ledger unparseable YAML
		// on the very first `decide add`. Convert the placeholder in place.
		if i := emptyListLine(fm, "decisions"); i >= 0 {
			fm[i] = "decisions:"
			fm = insertAt(fm, i+1, newLines)
		} else {
			// No decisions[] at all: append the key and the block at the end.
			fm = append(fm, "decisions:")
			fm = append(fm, newLines...)
		}
	} else if supersedes == "" {
		fm = insertAt(fm, end, newLines)
	} else {
		var ok bool
		fm, end, ok = markSuperseded(fm, start, end, supersedes, newID)
		if !ok {
			return "", fmt.Errorf("decision %s not found in the ledger: refusing to add %s, which claims to supersede it", supersedes, newID)
		}
		fm = insertAt(fm, end, newLines)
	}

	var b strings.Builder
	b.WriteString("---\n")
	for _, l := range fm {
		b.WriteString(l + "\n")
	}
	b.WriteString("---\n\n")
	for _, l := range doc.Preamble {
		b.WriteString(l + "\n")
	}
	for _, sec := range doc.Sections {
		b.WriteString(sec.Heading + "\n")
		for _, l := range sec.Body {
			b.WriteString(l + "\n")
		}
	}
	out := b.String()
	return strings.TrimRight(out, "\n") + "\n", nil
}

func insertAt(lines []string, at int, ins []string) []string {
	out := make([]string, 0, len(lines)+len(ins))
	out = append(out, lines[:at]...)
	out = append(out, ins...)
	out = append(out, lines[at:]...)
	return out
}

// markSuperseded sets the named entry's status to superseded and appends its
// superseded_by line — the only mutation an accepted entry permits. It returns
// the edited lines, the (possibly shifted) block end, and whether the entry was
// found.
//
// The entry is located by decoding the block and matching the `id` field,
// rather than by matching the text "- id: <id>" on a dash line. Those are not
// the same test: YAML puts no significance on which key an author writes first,
// so a ledger whose entries begin with `kind:` would never match textually. The
// earlier version returned silently in that case, leaving the superseded entry
// still `accepted` while the new entry claimed to supersede it — two
// contradictory accepted entries, which is precisely the state the ledger's
// collision rule exists to prevent.
//
// Line edits are still applied textually, so every byte the author wrote
// outside the two touched lines survives.
func markSuperseded(fm []string, start, end int, id, supersededBy string) ([]string, int, bool) {
	idx := entryLineIndex(fm, start, end, id)
	if idx < 0 {
		return fm, end, false
	}

	// This item's extent runs to the next dash line at the same indent, or the
	// block end.
	indent := leadingSpaceCount(fm[idx])
	itemEnd := end
	for j := idx + 1; j < end; j++ {
		if strings.TrimSpace(fm[j]) == "" {
			continue
		}
		if leadingSpaceCount(fm[j]) == indent && strings.HasPrefix(strings.TrimSpace(fm[j]), "- ") {
			itemEnd = j
			break
		}
	}

	fieldIndent := indent + 2
	statusSet := false
	for j := idx; j < itemEnd; j++ {
		trimmed := strings.TrimSpace(fm[j])
		if !strings.HasPrefix(trimmed, "status:") && !strings.HasPrefix(trimmed, "- status:") {
			continue
		}
		lead := leadingSpaceCount(fm[j])
		if strings.HasPrefix(trimmed, "- ") {
			// `- status: accepted` — the field shares the dash line, so the
			// dash has to be preserved.
			fm[j] = strings.Repeat(" ", lead) + "- status: superseded"
		} else {
			fm[j] = strings.Repeat(" ", lead) + "status: superseded"
			fieldIndent = lead
		}
		statusSet = true
		break
	}
	if !statusSet {
		// No status line to rewrite: add one rather than silently skipping it.
		fm = insertAt(fm, itemEnd, []string{strings.Repeat(" ", fieldIndent) + "status: superseded"})
		itemEnd++
		end++
	}

	supersededLine := strings.Repeat(" ", fieldIndent) + "superseded_by: " + supersededBy
	fm = insertAt(fm, itemEnd, []string{supersededLine})
	return fm, end + 1, true
}

// entryLineIndex returns the index of the line beginning the entry whose `id`
// is id, searching [start, end). It decodes the block so the match does not
// depend on field order or formatting, then maps the Nth entry back to its
// line by counting dash lines at the sequence's indent.
func entryLineIndex(fm []string, start, end int, id string) int {
	ordinal := -1
	for i, it := range fmSequenceBlock(fm[start:end]) {
		if it.Str("id") == id {
			ordinal = i
			break
		}
	}
	if ordinal < 0 {
		return -1
	}
	dashIndent := -1
	seen := 0
	for i := start; i < end; i++ {
		trimmed := strings.TrimSpace(fm[i])
		if !strings.HasPrefix(trimmed, "- ") && trimmed != "-" {
			continue
		}
		lead := leadingSpaceCount(fm[i])
		if dashIndent == -1 {
			dashIndent = lead
		}
		if lead != dashIndent {
			continue // a nested sequence inside an entry, not a new entry
		}
		if seen == ordinal {
			return i
		}
		seen++
	}
	return -1
}

func leadingSpaceCount(s string) int {
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	return i
}
