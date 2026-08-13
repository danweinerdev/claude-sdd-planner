// Package compile turns a Markdown proposal into the artifact the tool emits
// (FR-17). The payload is a proposal, never the file: the bytes written are
// always the ones produced here.
package compile

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
)

// Refusal is a reason the payload cannot be committed. All refusals are
// reported together rather than one at a time (FR-17).
type Refusal struct {
	Code       string
	Line       int
	Message    string
	Correction string
}

func (r Refusal) String() string {
	s := fmt.Sprintf("%s", r.Code)
	if r.Line > 0 {
		s += fmt.Sprintf(" line %d", r.Line)
	}
	s += ": " + r.Message
	if r.Correction != "" {
		s += "\n    fix: " + r.Correction
	}
	return s
}

// Result is the outcome of a compile.
type Result struct {
	Output      string
	Corrections []string // itemized auto-corrections (FR-19)
	Allocations []string // identifiers allocated and the text they bound to (FR-20)
	Carried     []string // identifiers carried forward onto rewritten items (supersede)
	Retired     []string
	Added       []string // structure inserted by the upgrade path
	// Todos are things a human or model must supply: a stubbed section whose
	// content is substantive, or a field with no mechanical default. They are
	// NOT refusals — the artifact is written and compliant in shape — but it is
	// not finished, and reporting them is the point of the upgrade path.
	Todos    []string
	Notes    []string // informational, not refusals
	Refusals []Refusal
}

func (r *Result) OK() bool { return len(r.Refusals) == 0 }

func (r *Result) refuse(code string, line int, msg, fix string) {
	r.Refusals = append(r.Refusals, Refusal{Code: code, Line: line, Message: msg, Correction: fix})
}

// Options controls a compile.
type Options struct {
	// Existing is the artifact currently on disk, or nil when creating.
	Existing *artifact.Doc
	// Retire names identifiers the author is deliberately removing (FR-45).
	Retire map[string]bool
	// Supersede replaces an artifact's content wholesale while carrying its
	// identifiers forward positionally: the Nth identified item in the payload
	// inherits the Nth live identifier, and only items beyond that count get
	// newly allocated ones.
	//
	// Ordinary apply is an EDIT: it assumes the payload restates the artifact,
	// so an identifier present on disk and absent from the payload is an
	// accidental deletion (SPK031) and an unidentified item is a genuinely new
	// requirement. Rewriting a spec from scratch violates both assumptions at
	// once — every requirement looks new and every existing id looks dropped —
	// which produced allocation output and mass-retirement refusals that
	// contradicted each other. Supersede states the intent that apply had to
	// guess: this is the same artifact, rewritten.
	Supersede bool
	// Today is the date stamped into `updated` (tool-owned, FR-18).
	Today string
	// AllowFrozen lets the FR-47 normalization migration — and only it — rewrite
	// `complete` and `frozen` artifacts that FR-46 otherwise protects.
	AllowFrozen bool
	// StubSections additionally inserts a placeholder body for a missing
	// required SECTION. Off by default, and deliberately so: measured against a
	// 660-artifact real corpus, stubbing sections traded 733 missing-heading
	// diagnostics for 1576 content-shape ones — it relocates a violation rather
	// than resolving it, because a section whose content is substantive is not
	// made compliant by a placeholder. Only frontmatter whose correct value is
	// genuinely mechanical (tags: [], related: []) is safe to insert.
	StubSections bool
	// Upgrade turns the migration path on: a missing required section or author
	// field is inserted from its schema default and reported, instead of
	// refusing. This is deliberately NOT a leniency mode for ordinary edits —
	// the guards exist so non-compliant structure cannot enter, and a project
	// with pre-existing non-compliant artifacts upgrades them explicitly rather
	// than having the compiler quietly tolerate them forever.
	Upgrade bool
}

var (
	idDecl = regexp.MustCompile(`^\s*[-*+]\s*(?:\[[ xX]\]\s*)?(~~)?\*\*([A-Z]+-\d+)\*\*(~~)?\s*:`)
	idTok  = regexp.MustCompile(`\b([A-Z]{1,4})-(\d{1,4})\b`)
	listRe = regexp.MustCompile(`^(\s*)[*+](\s+)`)
)

// Compile matches the payload against the schema, allocates identifiers,
// resolves references, normalizes, stamps tool-owned frontmatter, and returns
// the bytes that would be written.
func Compile(s *schema.Schema, payload string, opts Options) *Result {
	res := &Result{}
	if opts.Retire == nil {
		opts.Retire = map[string]bool{}
	}
	doc := artifact.Parse(payload)

	if doc.HadBOM {
		res.Corrections = append(res.Corrections, "stripped UTF-8 BOM")
	}
	if doc.LineEnding == "\r\n" {
		res.Corrections = append(res.Corrections, "converted CRLF line endings to LF")
	}

	checkDuplicateKeys(doc, res)
	checkFrozen(opts, res)

	matched, ordered, canonical := matchSections(s, doc, res, opts.Upgrade, opts.StubSections)
	fm := compileFrontmatter(s, doc, opts, res)
	existingIDs, retiredIDs := collectIdentifiers(s, opts.Existing)
	applyIdentifiers(s, matched, existingIDs, retiredIDs, opts, res)
	currentIDs, currentRetired := collectFromMatched(s, matched)
	checkCitations(s, matched, currentIDs, currentRetired, res)

	if !res.OK() {
		return res
	}
	if s.Preserves() {
		fmLines, ok := renderFrontmatter(doc, opts.Today)
		if !ok {
			res.refuse("FM01", 1,
				"frontmatter cannot be modeled as YAML, so it cannot be rewritten safely",
				"Correct the frontmatter syntax; a `{{PLACEHOLDER}}` value must be quoted.")
			return res
		}
		if opts.Upgrade {
			fmLines = upgradeFrontmatter(s, doc, fmLines, res)
		}
		res.Output = emitPreserved(s, fmLines, doc, ordered, canonical)
	} else {
		res.Output = emit(s, fm, doc, ordered, canonical)
	}
	return res
}

// CheckFrozen exposes the FR-46 frozen/complete guard for callers that never
// go through Compile, e.g. `sdd section set` (FR-22), which edits a single
// section body directly rather than recompiling the whole artifact.
func CheckFrozen(existing *artifact.Doc, allowFrozen bool) []Refusal {
	res := &Result{}
	checkFrozen(Options{Existing: existing, AllowFrozen: allowFrozen}, res)
	return res.Refusals
}

// RestampFrontmatter exposes the frontmatter renderer — the
// block copied verbatim except `updated` — for callers that edit an artifact
// without going through the flat field model, e.g. `sdd section set`.
func RestampFrontmatter(doc *artifact.Doc, today string) ([]string, bool) {
	return renderFrontmatter(doc, today)
}

// checkDuplicateKeys refuses an artifact declaring the same top-level
// frontmatter key twice. This is never auto-fixed, in either direction: YAML
// consumers disagree about which wins (PyYAML keeps the last, a line-oriented
// reader the first), so an artifact with two `status:` keys reads as complete to
// one tool and planned to another. Choosing for the author would silently pick a
// lifecycle state; only the author knows which was intended.
func checkDuplicateKeys(doc *artifact.Doc, res *Result) {
	first := map[string]int{}
	for _, e := range doc.Frontmatter {
		if prev, dup := first[e.Key]; dup {
			res.refuse("SPK023", e.Line,
				fmt.Sprintf("frontmatter key %q is declared twice (also line %d)", e.Key, prev),
				fmt.Sprintf("delete whichever %s: line is wrong — consumers disagree about which value wins, so this cannot be resolved mechanically", e.Key))
			continue
		}
		first[e.Key] = e.Line
	}
}

// checkFrozen refuses to rewrite a history-bearing artifact (FR-46).
// Normalization must never happen as a side effect of an ordinary edit: a
// `complete` artifact or a `frozen: true` review is anchored to recorded
// revision identity, and canonical reformatting would move the bytes that
// identity refers to. Converting such an artifact is the FR-47 migration's job,
// which validates before and after and lands as its own scoped revision.
func checkFrozen(opts Options, res *Result) {
	if opts.Existing == nil || opts.AllowFrozen {
		return
	}
	if v, ok := opts.Existing.FM("frozen"); ok && strings.EqualFold(strings.TrimSpace(v), "true") {
		res.refuse("SPK050", opts.Existing.FMLine("frozen"),
			"artifact is frozen (frozen: true) and may not be rewritten",
			"frozen artifacts are converted only by the normalization migration; edit a new review instead")
	}
	if v, ok := opts.Existing.FM("status"); ok && strings.EqualFold(strings.TrimSpace(v), "complete") {
		res.refuse("SPK051", opts.Existing.FMLine("status"),
			"artifact is complete and may not be rewritten",
			"completed work is anchored to recorded revision identity; use a lifecycle verb or the normalization migration")
	}
}

// matchSections maps payload sections onto declared slots, correcting
// unambiguous near misses and refusing the ambiguous ones (FR-19).
func matchSections(s *schema.Schema, doc *artifact.Doc, res *Result, upgrade, stubSections bool) (map[string]*artifact.Section, []artifact.Section, map[int]string) {
	matched := map[string]*artifact.Section{}
	var ordered []artifact.Section
	canonical := map[int]string{}
	maxSlot := s.MaxSlotDepth()

	folded := artifact.FoldDeeper(doc.Sections, maxSlot)
	for i := range folded {
		sec := &folded[i]
		h, correction := resolveHeading(s, sec)
		if h == nil {
			if s.AdditionalSections == "refused" {
				res.refuse("SPK010", sec.Line,
					fmt.Sprintf("section %q is not declared by the %s schema", sec.Heading, s.Type),
					"remove the section or declare it in the schema")
			} else {
				ordered = append(ordered, *sec)
				res.Notes = append(res.Notes,
					fmt.Sprintf("line %d: %q is not a declared slot; retained in place as an additional section", sec.Line, sec.Heading))
			}
			continue
		}
		if prev, dup := matched[h.Text]; dup {
			res.refuse("SPK011", sec.Line,
				fmt.Sprintf("two sections map to slot %q (also line %d)", h.Text, prev.Line),
				"merge them, or rename one to a distinct declared section")
			continue
		}
		if correction != "" {
			res.Corrections = append(res.Corrections, fmt.Sprintf("line %d: %s", sec.Line, correction))
			canonical[sec.Line] = h.Text
		}
		matched[h.Text] = sec
		ordered = append(ordered, *sec)
	}

	for i := range s.Headings {
		h := &s.Headings[i]
		if !h.Required || matched[h.Text] != nil {
			continue
		}
		if !upgrade || !stubSections {
			res.refuse("SPK012", 0,
				fmt.Sprintf("required section %q is absent", h.Text),
				fmt.Sprintf("add %q at depth %d, or run `sdd migrate` to insert it", h.Text, h.Depth))
			continue
		}
		body := []string{h.DefaultBody}
		if h.DefaultBody == "" {
			body = nil
		}
		synth := artifact.Section{Heading: h.Text, Depth: h.Depth, Body: body}
		ordered = append(ordered, synth)
		matched[h.Text] = &ordered[len(ordered)-1]
		res.Added = append(res.Added, fmt.Sprintf("section %s", h.Text))
		if strings.HasPrefix(h.DefaultBody, "TODO") {
			res.Todos = append(res.Todos,
				fmt.Sprintf("%s needs real content (inserted as a TODO placeholder)", h.Text))
		}
	}
	return matched, ordered, canonical
}

// resolveHeading finds the slot a payload heading means, normalizing where the
// intent is unambiguous. Returns the slot and a description of any correction.
func resolveHeading(s *schema.Schema, sec *artifact.Section) (*schema.Heading, string) {
	if h := s.Heading(sec.Heading); h != nil {
		return h, ""
	}
	title := sec.Title()

	// Trailing punctuation: "## Overview:" -> "## Overview"
	stripped := strings.TrimRight(title, ".:;,!?")
	// Emphasis used where a heading is required: "**Non-Goals**"
	stripped = strings.Trim(stripped, "*_")
	stripped = strings.TrimSpace(stripped)

	for i := range s.Headings {
		h := &s.Headings[i]
		if !strings.EqualFold(h.Title(), stripped) {
			continue
		}
		var fixes []string
		if h.Title() != stripped {
			fixes = append(fixes, fmt.Sprintf("corrected heading case to %q", h.Title()))
		}
		if stripped != title {
			fixes = append(fixes, "removed heading decoration/punctuation")
		}
		if h.Depth != sec.Depth {
			if abs(h.Depth-sec.Depth) > 1 {
				// More than one level off is not an unambiguous near miss.
				return nil, ""
			}
			fixes = append(fixes, fmt.Sprintf("corrected heading depth %d -> %d", sec.Depth, h.Depth))
		}
		if len(fixes) == 0 {
			return h, ""
		}
		return h, strings.Join(fixes, "; ")
	}
	return nil, ""
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// compileFrontmatter rejects tool-owned fields in the payload and stamps the
// tool's own values (FR-18).
func compileFrontmatter(s *schema.Schema, doc *artifact.Doc, opts Options, res *Result) []artifact.FMEntry {
	out := make([]artifact.FMEntry, 0, len(s.Frontmatter))
	renamed := map[string]string{}
	var carried []artifact.FMEntry

	// A tool-owned field in a payload is a verified assertion, not a
	// declaration — the same resolution FR-45 applies to identifiers. It is
	// checked against the artifact on disk rather than against what the tool
	// would stamp, because `updated` is restamped on every write and would
	// therefore never match a fresh stamp, refusing every real revision.
	toolValue := func(f schema.Field) string {
		switch f.Key {
		case "type":
			return f.Fixed
		case "updated":
			return opts.Today
		default:
			v := ""
			if opts.Existing != nil {
				v, _ = opts.Existing.FM(f.Key)
			}
			if v == "" {
				switch f.Key {
				case "created":
					v = opts.Today
				case "status":
					v = f.Enum[0]
				}
			}
			return v
		}
	}

	for _, e := range doc.Frontmatter {
		f := s.Field(e.Key)
		if f == nil {
			if alias := s.FieldByAlias(e.Key); alias != nil && opts.Upgrade {
				// Mechanical rename: same meaning, legacy spelling.
				renamed[alias.Key] = e.Value
				res.Corrections = append(res.Corrections,
					fmt.Sprintf("line %d: renamed frontmatter %s: to %s:", e.Line, e.Key, alias.Key))
				continue
			}
			if s.Preserves() {
				continue
			}
			if opts.Upgrade {
				// Carry the value through so migration never loses data, and
				// report it: frontmatter is tool-controlled, so an unmodeled key
				// must be removed or relocated by an author before the artifact
				// will pass an ordinary apply.
				carried = append(carried, e)
				res.Todos = append(res.Todos, fmt.Sprintf(
					"frontmatter %s: is not part of the %s schema — remove it or move its information into the body (kept for now, will refuse on the next apply)",
					e.Key, s.Type))
				continue
			}
			res.refuse("SPK020", e.Line,
				fmt.Sprintf("frontmatter key %q is not declared by the %s schema", e.Key, s.Type),
				fmt.Sprintf("remove the key; %s frontmatter is controlled by the tool and cannot carry unmodeled keys", s.Type))
			continue
		}
		if f.Ownership() == schema.Tool && !toolFieldConsistent(opts, *f, e.Value, toolValue) {
			res.refuse("SPK021", e.Line,
				fmt.Sprintf("frontmatter key %q is tool-owned and may not appear in a payload", e.Key),
				ownerHint(e.Key))
		}
	}

	for _, f := range s.Frontmatter {
		switch {
		case f.Ownership() == schema.Tool:
			out = append(out, artifact.FMEntry{Key: f.Key, Value: toolValue(f)})
		default:
			v, ok := doc.FM(f.Key)
			if !ok {
				if rv, isRenamed := renamed[f.Key]; isRenamed {
					out = append(out, artifact.FMEntry{Key: f.Key, Value: rv})
					continue
				}
				if !f.Required {
					continue
				}
				if opts.Upgrade && f.Default != "" {
					out = append(out, artifact.FMEntry{Key: f.Key, Value: f.Default})
					res.Added = append(res.Added, fmt.Sprintf("frontmatter %s: %s", f.Key, f.Default))
					continue
				}
				if opts.Upgrade {
					// No mechanical default: record it as work to do and keep
					// going, so one unsupplied field does not block every other
					// fix in the same artifact.
					res.Todos = append(res.Todos,
						fmt.Sprintf("frontmatter %s: is required and has no mechanical default — supply it", f.Key))
					continue
				}
				res.refuse("SPK022", 0,
					fmt.Sprintf("required frontmatter key %q is absent", f.Key),
					fmt.Sprintf("add %s: to the payload frontmatter, or run `sdd migrate`", f.Key))
				continue
			}
			out = append(out, artifact.FMEntry{Key: f.Key, Value: v})
		}
	}
	return append(out, carried...)
}

// upgradeFrontmatter adds each required author field the upgrade path can fill
// in from its declared default. Undeclared keys are left alone: nested
// structures (phases[], tasks[], findings[]) are carried by the marshaled node
// tree, and tool-owned flat keys are still verified by compileFrontmatter, so
// `status` cannot drift through this path either.
func upgradeFrontmatter(s *schema.Schema, doc *artifact.Doc, out []string, res *Result) []string {
	for _, f := range s.Frontmatter {
		if !f.Required || f.Ownership() != schema.Author || f.Default == "" {
			continue
		}
		if _, ok := doc.FM(f.Key); ok {
			continue
		}
		out = appendDefaults(out, f.Key, f.Default)
		res.Added = append(res.Added, fmt.Sprintf("frontmatter %s: %s", f.Key, f.Default))
	}
	return out
}

// toolFieldConsistent reports whether a tool-owned field in the payload matches
// what the artifact already says. On creation there is nothing to verify
// against, so any tool-owned field is an attempted override.
func toolFieldConsistent(opts Options, f schema.Field, payloadValue string, toolValue func(schema.Field) string) bool {
	if opts.Existing == nil {
		return false
	}
	onDisk, ok := opts.Existing.FM(f.Key)
	if !ok {
		return payloadValue == toolValue(f)
	}
	return payloadValue == onDisk
}

// ownerHint explains why one tool-owned field may not be supplied, and points
// at the command that generates a payload without any of them. Naming the flag
// matters: the refusal previously taught one field per attempt, so an author
// starting from `sdd template <type>` output hit four rounds of trial and error
// before arriving at a payload apply would accept.
func ownerHint(key string) string {
	const remedy = " (`sdd template <type> --for-apply` emits a payload with the tool-owned fields already omitted)"
	switch key {
	case "status":
		return "status is set by the lifecycle transition verbs, not by apply" + remedy
	case "updated":
		return "updated is stamped by the tool; remove it from the payload" + remedy
	case "created":
		return "created is preserved from the existing artifact; remove it from the payload" + remedy
	case "type":
		return "type is fixed by the schema; remove it from the payload" + remedy
	}
	return "remove the key; it is owned by the tool" + remedy
}

type identSet map[string]map[int]bool // namespace -> numbers

func (is identSet) add(ns string, n int) {
	if is[ns] == nil {
		is[ns] = map[int]bool{}
	}
	is[ns][n] = true
}

func (is identSet) has(ns string, n int) bool { return is[ns] != nil && is[ns][n] }

func (is identSet) max(ns string) int {
	m := 0
	for n := range is[ns] {
		if n > m {
			m = n
		}
	}
	return m
}

func (is identSet) list(ns string, f schema.Namespace) []string {
	var ns2 []int
	for n := range is[ns] {
		ns2 = append(ns2, n)
	}
	sort.Ints(ns2)
	out := make([]string, 0, len(ns2))
	for _, n := range ns2 {
		out = append(out, f.Format(n))
	}
	return out
}

// collectIdentifiers reads live and retired identifiers out of an artifact.
func collectIdentifiers(s *schema.Schema, doc *artifact.Doc) (live, retired identSet) {
	live, retired = identSet{}, identSet{}
	if doc == nil {
		return
	}
	folded := artifact.FoldDeeper(doc.Sections, s.MaxSlotDepth())
	for _, h := range s.Headings {
		if h.IDNamespace == "" {
			continue
		}
		for i := range folded {
			if folded[i].Heading != h.Text {
				continue
			}
			scanBody(folded[i].Body, h.IDNamespace, live, retired)
		}
	}
	return
}

// collectFromMatched reads live and retired identifiers out of the sections
// being compiled right now (after allocation), which is what a citation must
// resolve against — not just what the prior artifact on disk had.
func collectFromMatched(s *schema.Schema, matched map[string]*artifact.Section) (live, retired identSet) {
	live, retired = identSet{}, identSet{}
	for _, h := range s.Headings {
		if h.IDNamespace == "" {
			continue
		}
		sec := matched[h.Text]
		if sec == nil {
			continue
		}
		scanBody(sec.Body, h.IDNamespace, live, retired)
	}
	return
}

func scanBody(body []string, idNamespace string, live, retired identSet) {
	for _, vl := range artifact.VisibleLines(body) {
		m := idDecl.FindStringSubmatch(vl.Text)
		if m == nil {
			continue
		}
		ns, num, ok := splitID(m[2])
		if !ok || ns != idNamespace {
			continue
		}
		if m[1] == "~~" || m[3] == "~~" {
			retired.add(ns, num)
		} else {
			live.add(ns, num)
		}
	}
}

func splitID(id string) (string, int, bool) {
	ns, rest, ok := strings.Cut(id, "-")
	if !ok {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return "", 0, false
	}
	return ns, n, true
}

// applyIdentifiers enforces FR-45: payload identifiers are assertions verified
// against the artifact's current set, and an item with no identifier is new.
func applyIdentifiers(s *schema.Schema, matched map[string]*artifact.Section, live, retired identSet, opts Options, res *Result) {
	carry := newCarryState()
	for _, h := range s.Headings {
		if h.IDNamespace == "" {
			continue
		}
		nsDef := s.Namespace(h.IDNamespace)
		sec := matched[h.Text]
		if sec == nil {
			continue
		}

		payload := identSet{}
		nextFree := maxOf(live.max(h.IDNamespace), retired.max(h.IDNamespace))

		// A payload declaring an identifier the artifact doesn't know about
		// yet (fresh creation, Existing == nil) still claims that number: a
		// later unidentified item in the same section must not collide with
		// it.
		for _, line := range sec.Body {
			m := idDecl.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			ns, num, ok := splitID(m[2])
			if ok && ns == h.IDNamespace && num > nextFree {
				nextFree = num
			}
		}

		for li := range sec.Body {
			line := sec.Body[li]
			m := idDecl.FindStringSubmatch(line)
			if m != nil {
				ns, num, ok := splitID(m[2])
				if !ok || ns != h.IDNamespace {
					continue
				}
				if m[1] == "~~" || m[3] == "~~" {
					payload.add(ns, num) // retirement note carried forward
					continue
				}
				payload.add(ns, num)
				if opts.Existing != nil && !live.has(ns, num) {
					if retired.has(ns, num) {
						res.refuse("SPK030", sec.Line,
							fmt.Sprintf("payload declares %s, which is retired and may not be reissued", m[2]),
							fmt.Sprintf("use a new identifier; live %s identifiers: %s",
								ns, joinOrNone(live.list(ns, *nsDef))))
						continue
					}
					// A payload may introduce the next identifier in a
					// namespace: that is how a section grows, and how a
					// namespace with no live identifiers is started at all.
					// Refusing it made an empty namespace unusable — the
					// correction printed "live NFR identifiers:" with nothing
					// after it, so the only stated remedy named no option.
					// Anything beyond the next number is still refused: a gap
					// is an authoring mistake, not an intent.
					if num == nextFreeIn(live, ns, *nsDef)+1 {
						res.Allocations = append(res.Allocations,
							fmt.Sprintf("%s assigned to %q (declared in the payload)", m[2], snippetAfterID(line, m[2])))
						continue
					}
					res.refuse("SPK030", sec.Line,
						fmt.Sprintf("payload declares %s, which does not exist in the artifact", m[2]),
						fmt.Sprintf("use %s for the next new item; live %s identifiers: %s",
							nsDef.Format(nextFreeIn(live, ns, *nsDef)+1), ns,
							joinOrNone(live.list(ns, *nsDef))))
				}
				continue
			}
			// An unidentified item in an identifier-bearing section is new.
			if isNewItem(line) {
				// Superseding: inherit the next unclaimed live identifier
				// before allocating a fresh one, so a rewrite preserves the
				// identifiers everything else cites.
				if opts.Supersede {
					if id, num, ok := carry.next(h.IDNamespace, live, *nsDef, payload, opts.Retire); ok {
						sec.Body[li] = insertID(line, id)
						res.Carried = append(res.Carried,
							fmt.Sprintf("%s kept on %q", id, snippet(line)))
						payload.add(h.IDNamespace, num)
						continue
					}
				}
				nextFree++
				id := nsDef.Format(nextFree)
				sec.Body[li] = insertID(line, id)
				res.Allocations = append(res.Allocations,
					fmt.Sprintf("%s assigned to %q", id, snippet(line)))
				payload.add(h.IDNamespace, nextFree)
			}
		}

		if opts.Existing == nil {
			continue
		}
		// An existing identifier absent from the payload is an unintended
		// retirement unless --retire named it.
		for _, id := range live.list(h.IDNamespace, *nsDef) {
			_, num, _ := splitID(id)
			if payload.has(h.IDNamespace, num) {
				continue
			}
			if opts.Retire[id] {
				res.Retired = append(res.Retired, id)
				continue
			}
			if opts.Supersede {
				// The rewrite has fewer items than the artifact had, so this
				// identifier has nothing to attach to. Retire it rather than
				// refusing: dropping content is the ordinary meaning of a
				// supersede, and reporting it keeps the removal visible.
				res.Retired = append(res.Retired, id)
				continue
			}
			res.refuse("SPK031", sec.Line,
				fmt.Sprintf("%s exists in the artifact but is absent from the payload", id),
				fmt.Sprintf("restore it, or pass --retire %s to retire it deliberately", id))
		}
	}
}

func maxOf(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// carryState hands out an artifact's existing identifiers, in order, to the
// rewritten items of a supersede payload. Positional inheritance is the only
// mapping available without asking the author to restate every id — and asking
// that is exactly the burden supersede removes. It is reported per item
// (`FR-01 kept on "..."`) so the author can see what inherited what and correct
// it in the payload if the order is wrong.
type carryState struct{ used map[string]bool }

func newCarryState() *carryState { return &carryState{used: map[string]bool{}} }

// next returns the next live identifier in ns that the payload has not already
// claimed, marking it used.
func (c *carryState) next(ns string, live identSet, def schema.Namespace, payload identSet, retire map[string]bool) (string, int, bool) {
	for _, id := range live.list(ns, def) {
		_, num, ok := splitID(id)
		if !ok || payload.has(ns, num) || c.used[id] {
			continue
		}
		// An identifier the author named in --retire is not available to
		// carry. Without this, `--supersede --retire FR-02` handed FR-02 to a
		// rewritten item, and the retirement loop then skipped it as already
		// claimed — so the flag was silently ignored and some other
		// identifier was retired in its place.
		if retire[id] {
			continue
		}
		c.used[id] = true
		return id, num, true
	}
	return "", 0, false
}

// isNewItem reports whether a line is a list item with no identifier — the
// payload form meaning "allocate an identifier here".
func isNewItem(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "- ") && !strings.HasPrefix(t, "* ") && !strings.HasPrefix(t, "+ ") {
		return false
	}
	// Only top-level items are identifier-bearing; indented ones are subpoints.
	if line != strings.TrimLeft(line, " \t") {
		return false
	}
	body := strings.TrimSpace(t[2:])
	body = strings.TrimPrefix(body, "[ ] ")
	body = strings.TrimPrefix(body, "[x] ")
	return strings.TrimSpace(body) != ""
}

func insertID(line, id string) string {
	t := strings.TrimSpace(line)
	marker := t[:2]
	rest := strings.TrimSpace(t[2:])
	box := ""
	for _, p := range []string{"[ ] ", "[x] ", "[X] "} {
		if strings.HasPrefix(rest, p) {
			box = p
			rest = strings.TrimPrefix(rest, p)
			break
		}
	}
	return fmt.Sprintf("%s %s**%s**: %s", strings.TrimSpace(marker), box, id, rest)
}

func snippet(line string) string {
	t := strings.TrimSpace(line)
	t = strings.TrimLeft(t, "-*+ ")
	t = strings.TrimPrefix(t, "[ ] ")
	if len(t) > 60 {
		t = t[:60] + "…"
	}
	return t
}

// checkCitations implements FR-23: identifier-shaped tokens in prose resolve
// against the artifact, with three exemptions — code spans and fenced blocks
// are literals, retired identifiers resolve, and free-prose sections are
// exempt entirely.
func checkCitations(s *schema.Schema, matched map[string]*artifact.Section, live, retired identSet, res *Result) {
	known := map[string]bool{}
	for _, ns := range s.Namespaces {
		known[ns.Name] = true
	}
	for _, h := range s.Headings {
		sec := matched[h.Text]
		if sec == nil || h.FreeProse {
			continue
		}
		nsDef := map[string]schema.Namespace{}
		for _, n := range s.Namespaces {
			nsDef[n.Name] = n
		}
		for _, vl := range artifact.VisibleLines(sec.Body) {
			text := artifact.StripCodeSpans(vl.Text)
			// A declaration on this line defines rather than cites.
			if idDecl.MatchString(vl.Text) {
				if i := strings.Index(text, ":"); i >= 0 {
					text = text[i:]
				}
			}
			for _, m := range idTok.FindAllStringSubmatch(text, -1) {
				ns := m[1]
				if !known[ns] {
					continue
				}
				num, err := strconv.Atoi(m[2])
				if err != nil {
					continue
				}
				if live.has(ns, num) || retired.has(ns, num) {
					continue
				}
				def := nsDef[ns]
				res.refuse("SPK040", sec.Line+1+vl.Offset,
					fmt.Sprintf("citation %s-%s does not resolve", ns, m[2]),
					fmt.Sprintf("available %s identifiers: %s (wrap a literal in backticks to exempt it)",
						ns, strings.Join(live.list(ns, def), ", ")))
			}
		}
	}
}

// emit renders the canonical artifact: LF endings, declared heading order,
// stamped frontmatter, normalized list markers.
func emit(s *schema.Schema, fm []artifact.FMEntry, doc *artifact.Doc, ordered []artifact.Section, canonical map[int]string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, e := range fm {
		b.WriteString(e.Key)
		b.WriteString(": ")
		b.WriteString(e.Value)
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
	writeBody(&b, s, doc, ordered, canonical)
	out := b.String()
	return strings.TrimRight(out, "\n") + "\n"
}

// writeBody renders the H1, any lead prose, every matched slot in declared
// order, then additional sections in the order they appeared.
func writeBody(b *strings.Builder, s *schema.Schema, doc *artifact.Doc, ordered []artifact.Section, canonical map[int]string) {
	title, _ := doc.FM("title")
	title = strings.Trim(title, `"`)
	b.WriteString("# " + title + "\n\n")

	// Prose between the H1 and the first section is real content — a superseded
	// notice, a lead paragraph — and dropping it would silently lose text the
	// author wrote.
	var lead []string
	for _, l := range doc.Preamble {
		if strings.HasPrefix(strings.TrimSpace(l), "# ") {
			continue
		}
		lead = append(lead, l)
	}
	if lead = artifact.TrimBlank(lead); len(lead) > 0 {
		for _, l := range lead {
			b.WriteString(normalizeListMarker(l) + "\n")
		}
		b.WriteString("\n")
	}

	writeSection := func(heading string, body []string) {
		b.WriteString(heading + "\n")
		if body = artifact.TrimBlank(body); len(body) > 0 {
			for _, l := range body {
				b.WriteString(normalizeListMarker(l) + "\n")
			}
		}
		b.WriteString("\n")
	}

	// Sections are emitted in SOURCE order, not declared order. Canonical
	// reordering was tried and reverted: it is the one normalization that can
	// damage a document, and it buys the least. On a real design doc it hoisted
	// `### Data Flow` out of its parent section and relocated every undeclared
	// subsection to the bottom (106 deletions / 96 additions on a 629-line
	// file); on a phase doc it moved every `## <task-id>:` section below
	// `## Phase Completion Evidence`, which is semantically wrong. The schema
	// still governs which sections must EXIST — it no longer dictates where the
	// author put them. Every other normalization (LF, list markers, heading
	// canonicalization, blank-line handling, frontmatter, identifiers) is
	// unaffected and none of them can reorder content.
	for _, sec := range ordered {
		heading := sec.Heading
		if h := s.Heading(sec.Heading); h != nil {
			heading = h.Text // canonical form for a near-miss match
		} else if canon, ok := canonical[sec.Line]; ok {
			heading = canon
		}
		writeSection(heading, sec.Body)
	}
}

// emitPreserved renders an artifact whose frontmatter block is copied verbatim.
func emitPreserved(s *schema.Schema, fmLines []string, doc *artifact.Doc, ordered []artifact.Section, canonical map[int]string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, l := range fmLines {
		b.WriteString(l + "\n")
	}
	b.WriteString("---\n\n")
	writeBody(&b, s, doc, ordered, canonical)
	out := b.String()
	return strings.TrimRight(out, "\n") + "\n"
}

func normalizeListMarker(line string) string {
	return listRe.ReplaceAllString(line, "$1-$2")
}

// nextFreeIn returns the highest live number in ns, so the next new identifier
// is that plus one. Zero when the namespace is empty, which makes the first
// identifier NS-01.
func nextFreeIn(live identSet, ns string, def schema.Namespace) int {
	highest := 0
	for _, id := range live.list(ns, def) {
		if _, num, ok := splitID(id); ok && num > highest {
			highest = num
		}
	}
	return highest
}

// joinOrNone renders an identifier list, saying so explicitly when it is
// empty rather than trailing off after the colon.
func joinOrNone(ids []string) string {
	if len(ids) == 0 {
		return "(none yet)"
	}
	return strings.Join(ids, ", ")
}

// snippetAfterID renders the text of an item that already carries an
// identifier, without echoing the identifier back into the message.
func snippetAfterID(line, id string) string {
	t := snippet(line)
	// The declaration may be spelled `**FR-01**:`, `~~**FR-01**~~:`, or bare.
	// Trim from the identifier through the colon that follows it, whatever
	// emphasis markers sit in between.
	if i := strings.Index(t, id); i >= 0 {
		rest := t[i+len(id):]
		rest = strings.TrimLeft(rest, "*~")
		rest = strings.TrimPrefix(rest, ":")
		return strings.TrimSpace(rest)
	}
	return t
}
