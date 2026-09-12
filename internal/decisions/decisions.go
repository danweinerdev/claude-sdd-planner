// Package decisions owns the per-plan decision record
// (Designs/PlanDecisions): one flat JSON array per plan at
// Plans/<Name>/<Name>-Decisions.json, five fields per entry, append-only,
// content-addressed ids.
//
// The file is written by exactly two callers — `sdd decide add` after the
// user approves a statement, and `sdd compile` copying a related design's
// Design Decisions in verbatim — and both go through Append, which is a
// digest compare-and-swap on the whole file (the same discipline
// internal/graph/store.Update applies to the graph). Reads are derived on
// every call; nothing is cached or stored beyond the file itself.
package decisions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// IDPrefix is the id family for plan decisions. `pd-` plus IDHexLen hex
// characters of SHA-256 over the normalized statement.
const IDPrefix = "pd-"

// IDHexLen is the digest length kept in an id (PlanDecisions DD-3).
const IDHexLen = 8

// Entry is one decision. Exactly these fields (PlanDecisions DD-2); the
// decoder refuses anything else.
type Entry struct {
	ID        string `json:"id"`
	Date      string `json:"date"`
	Statement string `json:"statement"`
	// Supersedes names the decision(s) this one replaces: one id, or a
	// comma-separated list when one entry reconciles competing successors
	// (see Index.Conflicts). Each is `pd-…` or `<Plan>:pd-…`.
	Supersedes string `json:"supersedes,omitempty"`
	Source     string `json:"source,omitempty"`
}

// SupersededRefs splits an entry's Supersedes field into its references.
func (e Entry) SupersededRefs() []string {
	if strings.TrimSpace(e.Supersedes) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(e.Supersedes, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// idRe is the accepted shape of a plan-decision id.
var idRe = regexp.MustCompile(`^pd-[0-9a-f]{` + fmt.Sprint(IDHexLen) + `}$`)

// refRe is a citation: bare `pd-…` or `<Plan>:pd-…`.
var refRe = regexp.MustCompile(`^(?:([A-Za-z0-9][A-Za-z0-9._-]*):)?(pd-[0-9a-f]{` + fmt.Sprint(IDHexLen) + `})$`)

// sourceRe is the two accepted provenance shapes: a design decision
// (`Designs/<X>:DD-N`) or a review finding (`Reviews/<file>:F-NN` — any
// planning-root-relative markdown path followed by a finding id).
var sourceRe = regexp.MustCompile(`^(?:Designs/[^:\s]+:DD-\d{1,4}[a-z]?|[^:\s]+\.md:F-\d{1,4}|Decisions/decisions\.md:D-\d{4,9})$`)

// IsID reports whether s is a well-formed plan-decision id.
func IsID(s string) bool { return idRe.MatchString(s) }

// ParseRef splits a citation into (plan, id). plan is "" for a bare id.
func ParseRef(ref string) (plan, id string, ok bool) {
	m := refRe.FindStringSubmatch(ref)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// PathFor returns the decisions file for a plan directory:
// <planDir>/<PlanName>-Decisions.json.
func PathFor(planDir string) string {
	return filepath.Join(planDir, filepath.Base(planDir)+"-Decisions.json")
}

// Normalize is the statement canonicalization the id is computed over:
// NFC, trimmed, internal whitespace runs collapsed to one space.
func Normalize(statement string) string {
	s := norm.NFC.String(statement)
	var b strings.Builder
	space := false
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// IDFor computes the content-addressed id for a statement.
func IDFor(statement string) string {
	sum := sha256.Sum256([]byte(Normalize(statement)))
	return IDPrefix + hex.EncodeToString(sum[:])[:IDHexLen]
}

// Decode strictly decodes a decisions file. Unknown fields, wrong types,
// malformed ids, and ids that do not match their statement all refuse: the
// file is a record, and a record the tool cannot vouch for is not read.
func Decode(raw []byte) ([]Entry, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var entries []Entry
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("decisions file is not valid: %s", istore.DescribeJSONError(raw, err))
	}
	if entries == nil {
		return nil, errors.New("decisions file is not valid: top-level value must be a non-null array")
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("decisions file is not valid: trailing content after the array")
	}
	seen := map[string]int{}
	for i, e := range entries {
		if !IsID(e.ID) {
			return nil, fmt.Errorf("decisions file is not valid: entry %d id %q is not pd-<%d hex>", i, e.ID, IDHexLen)
		}
		if Normalize(e.Statement) == "" {
			return nil, fmt.Errorf("decisions file is not valid: entry %s has an empty statement", e.ID)
		}
		if want := IDFor(e.Statement); want != e.ID {
			return nil, fmt.Errorf("decisions file is not valid: entry %s does not match its statement (expected %s); entries are immutable — record a superseding decision instead of editing", e.ID, want)
		}
		if e.Date == "" {
			return nil, fmt.Errorf("decisions file is not valid: entry %s has no date", e.ID)
		}
		if _, err := time.Parse("2006-01-02", e.Date); err != nil {
			return nil, fmt.Errorf("decisions file is not valid: entry %s date %q is not YYYY-MM-DD", e.ID, e.Date)
		}
		for _, ref := range e.SupersededRefs() {
			if _, _, ok := ParseRef(ref); !ok {
				return nil, fmt.Errorf("decisions file is not valid: entry %s supersedes %q, which is not pd-<hex> or <Plan>:pd-<hex>", e.ID, ref)
			}
		}
		if e.Supersedes != "" && len(e.SupersededRefs()) == 0 {
			return nil, fmt.Errorf("decisions file is not valid: entry %s has an empty supersedes", e.ID)
		}
		if e.Source != "" && !sourceRe.MatchString(e.Source) {
			return nil, fmt.Errorf("decisions file is not valid: entry %s source %q is not Designs/<X>:DD-N or <review>.md:F-NN", e.ID, e.Source)
		}
		if prior, dup := seen[e.ID]; dup {
			return nil, fmt.Errorf("decisions file is not valid: id %s appears at entries %d and %d", e.ID, prior, i)
		}
		seen[e.ID] = i
	}
	return entries, nil
}

// Encode renders entries in the canonical on-disk form: two-space indent,
// one trailing newline, append order preserved. Byte-stable for the same
// input so a regenerated file diffs clean.
func Encode(entries []Entry) ([]byte, error) {
	if entries == nil {
		entries = []Entry{}
	}
	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// Load reads a plan's decisions file. A missing file is an empty record,
// not an error: a plan that has never recorded a decision has none.
func Load(path string) ([]Entry, error) {
	art, err := istore.Read(path)
	if err != nil {
		return nil, err
	}
	if !art.Exists {
		return nil, nil
	}
	entries, err := Decode([]byte(art.Source))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return entries, nil
}

// Lookup is what Append needs from the root-wide index: collision detection,
// resolution of a superseded reference from the writing plan's perspective,
// and the successor an id already has anywhere under the root. A nil Lookup
// limits all checks to the file being written.
type Lookup interface {
	Resolve(ref, from string) (Located, bool)
	EntriesWithID(id string) []Located
	SuccessorsOf(id string) []Located
}

// AppendResult reports what Append did.
type AppendResult struct {
	Entry   Entry
	Created bool // false when the identical statement was already recorded
	Path    string
}

// Append records one decision under a whole-file compare-and-swap. The id
// and date are always tool-set; a caller-supplied id is ignored.
//
// Idempotent on the statement: the same statement twice is one entry, and
// the second call reports the existing one. A different statement whose id
// collides is refused rather than merged.
func Append(path, statement, supersedes, source string, today string, from string, index Lookup) (AppendResult, error) {
	normalized := Normalize(statement)
	if normalized == "" {
		return AppendResult{}, errors.New("decide: statement is empty")
	}
	if today == "" {
		today = time.Now().UTC().Format("2006-01-02")
	}
	entry := Entry{ID: IDFor(statement), Date: today, Statement: normalized, Supersedes: strings.Join(splitRefs(supersedes), ", "), Source: source}
	if supersedes != "" && len(entry.SupersededRefs()) == 0 {
		return AppendResult{}, errors.New("decide: --supersedes is empty")
	}
	for _, ref := range entry.SupersededRefs() {
		if _, _, ok := ParseRef(ref); !ok {
			return AppendResult{}, fmt.Errorf("decide: --supersedes %q is not pd-<hex> or <Plan>:pd-<hex>", ref)
		}
	}
	if source != "" && !sourceRe.MatchString(source) {
		return AppendResult{}, fmt.Errorf("decide: --source %q is not Designs/<X>:DD-N or <review>.md:F-NN", source)
	}
	const attempts = 16
	for attempt := 0; attempt < attempts; attempt++ {
		// The cross-plan checks (successor, id collision) read other plans'
		// files, which this file's lock does not cover. Re-derive the index
		// on every attempt so the window is the check-to-write gap, not the
		// caller's whole session; a successor that lands in that gap is
		// still caught at merge time by Index.Conflicts / SDD192.
		if r, ok := index.(Reloader); ok && index != nil {
			fresh, err := r.Reload()
			if err != nil {
				return AppendResult{}, fmt.Errorf("decide: refreshing the cross-plan decision index: %w", err)
			}
			index = fresh
		}
		art, err := istore.Read(path)
		if err != nil {
			return AppendResult{}, err
		}
		var entries []Entry
		digest := ""
		if art.Exists {
			digest = art.Digest
			entries, err = Decode([]byte(art.Source))
			if err != nil {
				return AppendResult{}, fmt.Errorf("%s: %w", path, err)
			}
		}
		if index != nil {
			for _, existing := range index.EntriesWithID(entry.ID) {
				if Normalize(existing.Entry.Statement) != entry.Statement {
					return AppendResult{}, fmt.Errorf("decide: id %s is already recorded in plan %s for a different statement:\n  existing: %s\n  proposed: %s\nreword the statement", entry.ID, existing.Plan, existing.Entry.Statement, entry.Statement)
				}
			}
		}
		for _, e := range entries {
			if e.ID != entry.ID {
				continue
			}
			if Normalize(e.Statement) == entry.Statement {
				return AppendResult{Entry: e, Created: false, Path: path}, nil
			}
			return AppendResult{}, fmt.Errorf("decide: id %s is already recorded for a different statement:\n  existing: %s\n  proposed: %s\nreword the statement", entry.ID, e.Statement, entry.Statement)
		}
		for _, ref := range entry.SupersededRefs() {
			target, ok := resolveIn(entries, ref, from)
			if !ok && index != nil {
				if l, found := index.Resolve(ref, from); found {
					target, ok = l.Entry, true
				}
			}
			if !ok {
				return AppendResult{}, fmt.Errorf("decide: --supersedes %s names no recorded decision in this plan or any other plan under the planning root", ref)
			}
			if target.ID == entry.ID {
				return AppendResult{}, fmt.Errorf("decide: a decision cannot supersede itself (%s)", entry.ID)
			}
			// A decision has at most one distinct successor identity root-wide. A second
			// writer that raced in on another plan is refused here rather
			// than silently forking the history; the reconciliation is one
			// new entry that supersedes both (Index.Conflicts).
			var successors []string
			if succ, already := supersededBy(entries, target.ID); already {
				if succ != entry.ID {
					successors = append(successors, succ)
				}
			}
			if index != nil {
				for _, l := range index.SuccessorsOf(target.ID) {
					if l.Entry.ID == entry.ID {
						continue // the same content-addressed successor copied into another plan
					}
					successors = appendUniqueString(successors, l.Plan+":"+l.Entry.ID)
				}
			}
			if len(successors) > 0 {
				return AppendResult{}, fmt.Errorf("decide: %s is already superseded by %s; supersede the current decision instead", target.ID, strings.Join(successors, ", "))
			}
		}
		entries = append(entries, entry)
		out, err := Encode(entries)
		if err != nil {
			return AppendResult{}, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return AppendResult{}, err
		}
		// WriteAtomicExpecting treats an empty digest as expected absence, so
		// first creation has the same CAS fence as replacement. A writer that
		// lost the creation race re-reads and reapplies its append.
		writeErr := istore.WriteAtomicExpecting(path, string(out), digest)
		if writeErr == nil {
			return AppendResult{Entry: entry, Created: true, Path: path}, nil
		}
		var concurrent *istore.ErrConcurrentWrite
		if !errors.As(writeErr, &concurrent) {
			return AppendResult{}, writeErr
		}
	}
	return AppendResult{}, fmt.Errorf("decide: gave up after %d concurrent-write collisions on %s", attempts, path)
}

// resolveIn finds a bare or same-plan-qualified id inside one file's entries.
// An explicit qualifier names a specific plan, so another plan's qualifier
// must resolve through the derived index even when this file carries the id.
func resolveIn(entries []Entry, ref, from string) (Entry, bool) {
	plan, id, ok := ParseRef(ref)
	if !ok || (plan != "" && plan != from) {
		return Entry{}, false
	}
	for _, e := range entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// supersededBy reports the successor of id within one file, if any.
func supersededBy(entries []Entry, id string) (string, bool) {
	for _, e := range entries {
		for _, ref := range e.SupersededRefs() {
			if _, sid, ok := ParseRef(ref); ok && sid == id {
				return e.ID, true
			}
		}
	}
	return "", false
}

func splitRefs(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func appendUniqueString(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// PlanFile is one plan's decisions as loaded from the planning root.
type PlanFile struct {
	Plan    string
	Path    string
	Rel     string // planning-root-relative, forward slashes
	Entries []Entry
	Err     error // a malformed file is reported, not silently skipped
}

// LoadRoot loads every Plans/<Name>/<Name>-Decisions.json under a planning
// root, sorted by plan name. Plans without a file are omitted.
func LoadRoot(planningRoot string) ([]PlanFile, error) {
	plansDir := filepath.Join(planningRoot, "Plans")
	dirs, err := os.ReadDir(plansDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []PlanFile
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		planDir := filepath.Join(plansDir, d.Name())
		path := PathFor(planDir)
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			out = append(out, PlanFile{Plan: d.Name(), Path: path, Rel: "Plans/" + d.Name() + "/" + filepath.Base(path), Err: err})
			continue
		}
		pf := PlanFile{Plan: d.Name(), Path: path, Rel: "Plans/" + d.Name() + "/" + filepath.Base(path)}
		pf.Entries, pf.Err = Load(path)
		out = append(out, pf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Plan < out[j].Plan })
	return out, nil
}

// ValidatedIndex builds a decision index only when every discovered per-plan
// file decoded successfully and same-id copies have the same normalized
// statement. Authority-sensitive callers must not derive a partial or
// collision-ambiguous view.
func ValidatedIndex(files []PlanFile) (*Index, error) {
	seen := map[string]Located{}
	for _, file := range files {
		if file.Err != nil {
			return nil, fmt.Errorf("per-plan decisions snapshot is incomplete: %s: %w", file.Rel, file.Err)
		}
		for _, entry := range file.Entries {
			located := Located{Plan: file.Plan, Entry: entry}
			if prior, ok := seen[entry.ID]; ok && Normalize(prior.Entry.Statement) != Normalize(entry.Statement) {
				return nil, fmt.Errorf("decision authority has id collision %s between plans %s and %s:\n  %s: %s\n  %s: %s", entry.ID, prior.Plan, file.Plan, prior.Plan, prior.Entry.Statement, file.Plan, entry.Statement)
			}
			seen[entry.ID] = located
		}
	}
	return NewIndex(files), nil
}

// Reloader is an optional Lookup capability: re-derive the index from disk.
// Append uses it on every write attempt (see the loop comment there).
type Reloader interface {
	Reload() (Lookup, error)
}

// Reload re-reads every plan file under the root this index was loaded
// from. An index built from an explicit file list (no root) returns itself.
func (x *Index) Reload() (Lookup, error) {
	if x.root == "" {
		return x, nil
	}
	_, fresh, err := LoadValidatedIndex(x.root)
	if err != nil {
		return nil, err
	}
	return fresh, nil
}

// LoadValidatedIndex reads per-plan files and derives a complete cross-plan
// reference index. It stores no global ledger and grants no root-wide authority:
// callers must select the plan whose decisions they intend to consume.
func LoadValidatedIndex(planningRoot string) ([]PlanFile, *Index, error) {
	files, err := LoadRoot(planningRoot)
	if err != nil {
		return nil, nil, err
	}
	index, err := ValidatedIndex(files)
	if err != nil {
		return files, nil, err
	}
	index.root = planningRoot
	return files, index, nil
}

// Located is an entry together with the plan that carries it.
type Located struct {
	Plan  string `json:"plan"`
	Entry Entry  `json:"entry"`
}

// Index is the root-wide lookup over every plan's decisions.
type Index struct {
	files      []PlanFile
	root       string               // planning root when loaded via LoadValidatedIndex; "" otherwise
	byID       map[string][]Located // an id compiled into two plans appears twice
	bySource   map[string][]Located // e.g. "Designs/X:DD-3" -> the compiled entries
	successors map[string][]Located // superseded id -> entries that supersede it
}

// NewIndex builds the lookup. Malformed files contribute nothing; the
// caller reports PlanFile.Err separately.
func NewIndex(files []PlanFile) *Index {
	x := &Index{
		files:      files,
		byID:       map[string][]Located{},
		bySource:   map[string][]Located{},
		successors: map[string][]Located{},
	}
	for _, f := range files {
		if f.Err != nil {
			continue
		}
		for _, e := range f.Entries {
			l := Located{Plan: f.Plan, Entry: e}
			x.byID[e.ID] = append(x.byID[e.ID], l)
			if e.Source != "" {
				x.bySource[e.Source] = append(x.bySource[e.Source], l)
			}
			seenTargets := map[string]bool{}
			for _, ref := range e.SupersededRefs() {
				if _, id, ok := ParseRef(ref); ok && !seenTargets[id] {
					x.successors[id] = append(x.successors[id], l)
					seenTargets[id] = true
				}
			}
		}
	}
	return x
}

// EntriesWithID returns every plan-local occurrence of id. Identical
// content-addressed copies are valid; callers use the statements to refuse a
// truncated-digest collision instead of silently treating it as a copy.
func (x *Index) EntriesWithID(id string) []Located {
	return append([]Located(nil), x.byID[id]...)
}

// BySource returns the entries compiled from one provenance reference
// (e.g. "Designs/X:DD-3"), preferring the given plan's copy first.
func (x *Index) BySource(source, plan string) []Located {
	hits := append([]Located(nil), x.bySource[source]...)
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Plan == plan && hits[j].Plan != plan })
	return hits
}

// Files returns the loaded plan files.
func (x *Index) Files() []PlanFile { return x.files }

// Resolve answers a citation: a bare id resolves when exactly one plan
// carries it OR when `from` (the citing plan) carries it; a qualified id
// resolves in the named plan.
func (x *Index) Resolve(ref, from string) (Located, bool) {
	plan, id, ok := ParseRef(ref)
	if !ok {
		return Located{}, false
	}
	hits := x.byID[id]
	if plan != "" {
		for _, h := range hits {
			if h.Plan == plan {
				return h, true
			}
		}
		return Located{}, false
	}
	for _, h := range hits {
		if h.Plan == from {
			return h, true
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return Located{}, false
}

// Ambiguous returns the qualified spellings competing for a bare id when it
// resolves in more than one plan and the citing plan is not one of them.
func (x *Index) Ambiguous(ref, from string) []string {
	plan, id, ok := ParseRef(ref)
	if !ok || plan != "" {
		return nil
	}
	hits := x.byID[id]
	if len(hits) < 2 {
		return nil
	}
	for _, h := range hits {
		if h.Plan == from {
			return nil
		}
	}
	var out []string
	for _, h := range hits {
		out = append(out, h.Plan+":"+h.Entry.ID)
	}
	sort.Strings(out)
	return out
}

// SuccessorsOf returns every entry, in any plan, that supersedes id. More
// than one is a conflict (two plans raced to supersede the same decision);
// see Conflicts.
func (x *Index) SuccessorsOf(id string) []Located {
	return append([]Located(nil), x.successors[id]...)
}

// SuccessorOf returns the entry that supersedes id anywhere under the root.
// A supersession recorded in any plan retires the id everywhere
// (PlanDecisions DD-6). With competing successors the first in plan order
// is returned; Conflicts reports the contest.
func (x *Index) SuccessorOf(id string) (Located, bool) {
	succ := x.SuccessorsOf(id)
	if len(succ) == 0 {
		return Located{}, false
	}
	return succ[0], true
}

// Conflict is one decision with more than one successor.
type Conflict struct {
	ID         string    `json:"id"`
	Successors []Located `json:"successors"`
}

// Conflicts lists every id whose distinct successor branches still have more
// than one live descendant. Per-file CAS cannot prevent two plans from
// superseding the same decision on different branches; this is where the merge
// surfaces it. Advancing only one branch does not erase the fork. It clears
// only when one reconciling descendant supersedes every live branch. Copies of
// the same content-addressed successor id are one branch, not a conflict.
func (x *Index) Conflicts() []Conflict {
	var out []Conflict
	liveMemo := map[string]map[string]Located{}
	for id := range x.byID {
		immediate := x.uniqueSuccessors(id)
		if len(immediate) < 2 {
			continue
		}
		var frontiers []map[string]Located
		liveByID := map[string]Located{}
		for _, successor := range immediate {
			frontier := x.liveDescendants(successor.Entry.ID, liveMemo, map[string]bool{})
			frontiers = append(frontiers, frontier)
			for liveID, live := range frontier {
				liveByID[liveID] = live
			}
		}
		// A later decision reconciles the original fork when every immediate
		// branch reaches the same current frontier. If that shared descendant
		// later forks again, Conflicts reports the new fork at that descendant
		// rather than incorrectly reviving this already-reconciled one.
		converged := true
		for i := 1; i < len(frontiers); i++ {
			if !sameLocatedIDs(frontiers[0], frontiers[i]) {
				converged = false
				break
			}
		}
		if converged {
			continue
		}
		if len(liveByID) < 2 {
			continue
		}
		live := make([]Located, 0, len(liveByID))
		for _, successor := range liveByID {
			live = append(live, successor)
		}
		sort.Slice(live, func(i, j int) bool {
			if live[i].Entry.ID != live[j].Entry.ID {
				return live[i].Entry.ID < live[j].Entry.ID
			}
			return live[i].Plan < live[j].Plan
		})
		out = append(out, Conflict{ID: id, Successors: live})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (x *Index) uniqueSuccessors(id string) []Located {
	byID := map[string]Located{}
	for _, successor := range x.SuccessorsOf(id) {
		if _, seen := byID[successor.Entry.ID]; !seen {
			byID[successor.Entry.ID] = successor
		}
	}
	out := make([]Located, 0, len(byID))
	for _, successor := range byID {
		out = append(out, successor)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entry.ID < out[j].Entry.ID })
	return out
}

func (x *Index) liveDescendants(id string, memo map[string]map[string]Located, visiting map[string]bool) map[string]Located {
	if live, ok := memo[id]; ok {
		return live
	}
	if visiting[id] {
		// Supersession histories should be DAGs, but a malformed or synthetic
		// index can contain a cycle. Treat the back-edge identity as a live
		// frontier so traversal terminates without pretending the cycle erased
		// the branch.
		return x.locatedIdentity(id)
	}
	visiting[id] = true
	defer delete(visiting, id)
	successors := x.uniqueSuccessors(id)
	if len(successors) == 0 {
		live := x.locatedIdentity(id)
		memo[id] = live
		return live
	}
	live := map[string]Located{}
	for _, successor := range successors {
		for liveID, descendant := range x.liveDescendants(successor.Entry.ID, memo, visiting) {
			live[liveID] = descendant
		}
	}
	memo[id] = live
	return live
}

func (x *Index) locatedIdentity(id string) map[string]Located {
	if hits := x.byID[id]; len(hits) > 0 {
		chosen := hits[0]
		for _, hit := range hits[1:] {
			if hit.Plan < chosen.Plan {
				chosen = hit
			}
		}
		return map[string]Located{id: chosen}
	}
	return map[string]Located{}
}

func sameLocatedIDs(a, b map[string]Located) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if _, ok := b[id]; !ok {
			return false
		}
	}
	return true
}

// CurrentEntry is one line of `sdd decide current`: an id, the plans that
// carry it, and its entry (the first plan's copy — copies are identical by
// construction, since the id is the statement's digest).
type CurrentEntry struct {
	ID        string   `json:"id"`
	Plans     []string `json:"plans"`
	Date      string   `json:"date"`
	Statement string   `json:"statement"`
	Source    string   `json:"source,omitempty"`
}

// Current derives the standing decisions: every id not superseded by any
// entry in any plan, same-id copies collapsed, ordered by date then id.
// plan narrows the walk to entries carried by that plan.
func (x *Index) Current(plan string) []CurrentEntry {
	var out []CurrentEntry
	for id, hits := range x.byID {
		if _, gone := x.SuccessorOf(id); gone {
			continue
		}
		var plans []string
		carried := false
		for _, h := range hits {
			plans = append(plans, h.Plan)
			if h.Plan == plan {
				carried = true
			}
		}
		if plan != "" && !carried {
			continue
		}
		sort.Strings(plans)
		e := hits[0].Entry
		out = append(out, CurrentEntry{ID: id, Plans: plans, Date: e.Date, Statement: e.Statement, Source: e.Source})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Chain is `sdd decide lookup`'s answer: the entry, what it superseded,
// and what superseded it.
type Chain struct {
	Found        Located   `json:"found"`
	Supersedes   []Located `json:"supersedes,omitempty"`
	SupersededBy *Located  `json:"superseded_by,omitempty"`
}

// Lookup resolves ref from the perspective of plan `from` and walks one hop
// in each direction.
func (x *Index) Lookup(ref, from string) (Chain, bool) {
	hit, ok := x.Resolve(ref, from)
	if !ok {
		return Chain{}, false
	}
	c := Chain{Found: hit}
	for _, ref := range hit.Entry.SupersededRefs() {
		if prev, ok := x.Resolve(ref, hit.Plan); ok {
			c.Supersedes = append(c.Supersedes, prev)
		}
	}
	if next, ok := x.SuccessorOf(hit.Entry.ID); ok {
		c.SupersededBy = &next
	}
	return c, true
}

// ddRe is the design Design-Decision declaration, identical to the
// validator's `DD` definition pattern (internal/rules/index.go): a heading
// of depth 2–4 or a top-level bold bullet. rules_test asserts the two stay
// equal; the copy exists only to keep this package free of a rules import
// (rules imports decisions for the citation index).
var ddRe = regexp.MustCompile(`(?m)^\s*(?:#{2,4}\s+|-\s+\*\*)(DD-\d{1,4}[a-z]?)\b`)

// DDPattern exposes ddRe so the rules package can assert parity.
func DDPattern() *regexp.Regexp { return ddRe }

var anyHeadingRe = regexp.MustCompile(`(?m)^#{1,6}\s`)

// DesignDecision is one extracted DD: its id, its verbatim text, and the
// DD it declares it supersedes, if its text carries a `Supersedes DD-N` /
// `Supersedes: Other:DD-N` clause — the one mechanical parse conversion
// does, so a design that revises its own earlier decision compiles with
// the edge attached (an immutable compiled entry cannot gain it later).
type DesignDecision struct {
	ID         string
	Text       string
	Supersedes string // "" or "DD-N" / "<Qualifier>:DD-N" as written
}

var ddSupersedesRe = regexp.MustCompile(`(?i)\bsupersedes:?\s+((?:[A-Za-z0-9./_-]+:)?DD-\d{1,4}[a-z]?)\b`)

// ExtractDesignDecisions returns every DD declaration in a comment-stripped
// design body with its statement text, verbatim, in document order. For a
// bullet the text runs from the bullet to the next top-level list item or
// heading; for a heading it runs to the next heading. Leading `- **` / `#`
// markup is stripped and the rest is kept as written (PlanDecisions DD-4:
// a verbatim copy, no field extraction).
func ExtractDesignDecisions(body string) []DesignDecision {
	locs := ddRe.FindAllStringSubmatchIndex(body, -1)
	var out []DesignDecision
	for i, m := range locs {
		// The pattern's leading `\s*` can swallow preceding blank lines;
		// anchor the span at the declaration's own line.
		start := strings.LastIndexByte(body[:m[2]], '\n') + 1
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		span := body[start:end]
		isBullet := strings.HasPrefix(strings.TrimLeft(span, " \t"), "-")
		// Bound at the next heading, searching from the second line so a
		// heading-form DD does not terminate on itself.
		if nl := strings.IndexByte(span, '\n'); nl >= 0 {
			if loc := anyHeadingRe.FindStringIndex(span[nl+1:]); loc != nil {
				span = span[:nl+1+loc[0]]
			}
		}
		if isBullet {
			// A bullet's continuation lines are indented; the first
			// non-blank unindented line after the bullet (next top-level
			// bullet, a trailing paragraph, anything) ends it.
			lines := strings.SplitAfter(span, "\n")
			for i := 1; i < len(lines); i++ {
				l := lines[i]
				if strings.TrimSpace(l) == "" {
					continue
				}
				if !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
					span = strings.Join(lines[:i], "")
					break
				}
			}
		}
		text := strings.TrimSpace(span)
		text = strings.TrimLeft(text, "#")
		text = strings.TrimSpace(text)
		text = strings.TrimPrefix(text, "- ")
		text = strings.TrimSpace(text)
		dd := DesignDecision{ID: body[m[2]:m[3]], Text: text}
		if sm := ddSupersedesRe.FindStringSubmatch(text); sm != nil {
			dd.Supersedes = sm[1]
		}
		out = append(out, dd)
	}
	return out
}

// Digest returns the content digest of the file at path, or "" when it does
// not exist. Callers use it to report the expected-digest of a preview.
func Digest(path string) (string, error) {
	art, err := istore.Read(path)
	if err != nil {
		return "", err
	}
	if !art.Exists {
		return "", nil
	}
	return art.Digest, nil
}
