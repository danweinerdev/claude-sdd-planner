package dlg

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ValidateCollection ports validate_collection: the checks that need every
// ledger at once — id uniqueness and sequencing across files, supersession
// link integrity, and the pairwise conflict candidates.
//
// The canonical ledger and its archives are one logical ledger split across
// files, so an id is unique across the set, not per file. That is why these
// checks cannot live in ValidateLedger.

type indexedEntry struct {
	Ledger *Ledger
	Entry  map[string]any
}

// entryNumber returns the numeric part of a D-NNNN id.
func entryNumber(id string) int {
	n, _ := strconv.Atoi(strings.SplitN(id, "-", 2)[1])
	return n
}

func ValidateCollection(ledgers []*Ledger, entriesByLedger map[string][]map[string]any) []Diagnostic {
	var out []Diagnostic

	// Index by id, first writer winning; a repeat is DLG050.
	indexed := map[string]indexedEntry{}
	var indexedOrder []string
	for _, l := range ledgers {
		for _, entry := range entriesByLedger[l.Path] {
			id, ok := entry["id"].(string)
			if !ok || !decisionIDRe.MatchString(id) {
				continue
			}
			if _, dup := indexed[id]; dup {
				out = append(out, diag(l, "DLG050",
					"Duplicate decision id `"+id+"` across ledger files.",
					"Renumber the later entry and update all links.", l.Line("id: "+id), "", Error))
				continue
			}
			indexed[id] = indexedEntry{Ledger: l, Entry: entry}
			indexedOrder = append(indexedOrder, id)
		}
	}

	// DLG064: the id sequence must have no gap. Python compares each id
	// against its predecessor starting from a virtual 0, so a ledger starting
	// at D-0002 is itself a gap.
	var numeric []int
	for id := range indexed {
		numeric = append(numeric, entryNumber(id))
	}
	sortInts(numeric)
	prev := 0
	for _, n := range numeric {
		if n != prev+1 {
			lowest := ""
			for id := range indexed {
				if lowest == "" || entryNumber(id) < entryNumber(lowest) {
					lowest = id
				}
			}
			out = append(out, diag(indexed[lowest].Ledger, "DLG064",
				fmt.Sprintf("Decision id sequence jumps from `D-%04d` to `D-%04d`.", prev, n),
				"Restore retained entries, renumber an uncommitted later entry to the next sequential id, "+
					"or waive DLG064 if the gap predates this ledger's sequencing.",
				1, "", Warning))
			break
		}
		prev = n
	}

	// DLG065: within one file, entries must be in ascending id order.
	for _, l := range ledgers {
		var ordered []int
		for _, entry := range entriesByLedger[l.Path] {
			if id, ok := entry["id"].(string); ok && decisionIDRe.MatchString(id) {
				ordered = append(ordered, entryNumber(id))
			}
		}
		if !ascending(ordered) {
			out = append(out, diag(l, "DLG065", "Decision entries are not in ascending id order.",
				"Keep append-only entries ordered by their sequential ids, or waive DLG065 if the "+
					"disorder is inherited history that append-only rules forbid rewriting.",
				l.Line("decisions:"), "", Warning))
		}
	}

	// Supersession link integrity. Python iterates a dict, whose order is
	// insertion order; indexedOrder preserves that so diagnostics come out in
	// the same sequence.
	graph := map[string]string{}
	for _, entryID := range indexedOrder {
		rec := indexed[entryID]
		l, entry := rec.Ledger, rec.Entry
		status := str(entry["status"])
		supersedes, _ := entry["supersedes"].(string)
		supersededBy, _ := entry["superseded_by"].(string)

		if status == "superseded" && supersededBy == "" {
			out = append(out, diag(l, "DLG051", "Superseded `"+entryID+"` lacks `superseded_by`.",
				"Link the accepted replacement.", l.Line("id: "+entryID), "", Error))
		}
		if supersededBy != "" && status != "superseded" {
			out = append(out, diag(l, "DLG052",
				"Decision `"+entryID+"` has `superseded_by` but is not superseded.",
				"Set the lifecycle status correctly or remove the link.", l.Line("superseded_by:"), "", Error))
		}
		if supersedes != "" && status != "accepted" && status != "superseded" {
			out = append(out, diag(l, "DLG053",
				"Replacement `"+entryID+"` with `supersedes` has invalid status `"+pyValue(entry["status"])+"`.",
				"A replacement is accepted initially and may later become superseded itself.",
				l.Line("supersedes:"), "", Error))
		}

		for _, pair := range [2][2]string{{"supersedes", "superseded_by"}, {"superseded_by", "supersedes"}} {
			field, reverse := pair[0], pair[1]
			value, ok := entry[field].(string)
			if !ok || !decisionIDRe.MatchString(value) {
				continue
			}
			if value == entryID {
				out = append(out, diag(l, "DLG054",
					"Decision `"+entryID+"` links to itself through `"+field+"`.",
					"Link two distinct decisions.", l.Line(field+":"), "", Error))
				continue
			}
			target, found := indexed[value]
			switch {
			case !found:
				out = append(out, diag(l, "DLG055",
					"Decision `"+entryID+"` "+field+" unknown `"+value+"`.",
					"Reference an existing decision in the canonical ledger or archives.",
					l.Line(field+":"), "", Error))
			case str(target.Entry[reverse]) != entryID:
				out = append(out, diag(l, "DLG056",
					"Decision `"+entryID+"` "+field+" link to `"+value+"` is not reciprocated.",
					"Add matching `"+reverse+": "+entryID+"`.", l.Line(field+":"), "", Error))
			default:
				if field == "supersedes" {
					if str(target.Entry["status"]) != "superseded" {
						out = append(out, diag(l, "DLG057",
							"Decision `"+entryID+"` supersedes `"+value+"`, but the old decision is not superseded.",
							"Set the old decision status to `superseded`.",
							l.Line("supersedes: "+value), "", Error))
					}
					if entryNumber(entryID) <= entryNumber(value) {
						out = append(out, diag(l, "DLG067",
							"Replacement `"+entryID+"` does not have a newer id than `"+value+"`.",
							"Append the replacement with the next sequential decision id.",
							l.Line("supersedes: "+value), "", Error))
					}
					replacementDate := isoDate(entry["date"])
					replacedDate := isoDate(target.Entry["date"])
					if replacementDate != "" && replacedDate != "" && replacementDate < replacedDate {
						out = append(out, diag(l, "DLG068",
							"Replacement `"+entryID+"` predates `"+value+"`.",
							"Correct the dates so replacement history moves forward.",
							l.Line("supersedes: "+value), "", Error))
					}
				}
				if field == "superseded_by" {
					ts := str(target.Entry["status"])
					if ts != "accepted" && ts != "superseded" {
						out = append(out, diag(l, "DLG058",
							"Decision `"+entryID+"` is replaced by `"+value+"`, but the replacement has invalid status `"+pyValue(target.Entry["status"])+"`.",
							"A replacement must be accepted or part of a later supersession chain.",
							l.Line("superseded_by: "+value), "", Error))
					}
				}
			}
		}
		if decisionIDRe.MatchString(supersedes) {
			graph[entryID] = supersedes
		}
	}

	for _, cycle := range cyclePaths(graph) {
		l := indexed[cycle[0]].Ledger
		out = append(out, diag(l, "DLG059",
			"Supersession cycle detected: "+strings.Join(append(append([]string{}, cycle...), cycle[0]), " -> ")+".",
			"Break the cycle and restore one-way replacement history.",
			l.Line("id: "+cycle[0]), "", Error))
	}

	// DLG066: a superseded entry must reach an accepted terminal replacement.
	//
	// Python's while/else is the subtle part, and easy to invert: the `else`
	// runs when the loop ends WITHOUT break, and its body is `continue` — so
	// a chain that walks to exhaustion is skipped, and the terminal check
	// runs only after a `break`. A break means the walk stopped at something
	// concrete: a missing entry, a non-string link, an unknown target, or an
	// accepted replacement. Only the first three can leave a dangling
	// terminal, which is what the check below then reports.
	for _, entryID := range indexedOrder {
		rec := indexed[entryID]
		if str(rec.Entry["status"]) != "superseded" {
			continue
		}
		seen := map[string]bool{}
		currentID := entryID
		broke := false
		for !seen[currentID] {
			seen[currentID] = true
			current, found := indexed[currentID]
			if !found {
				broke = true
				break
			}
			replacement, ok := current.Entry["superseded_by"].(string)
			if !ok {
				broke = true
				break
			}
			target, found := indexed[replacement]
			if !found {
				broke = true
				break
			}
			if str(target.Entry["status"]) == "accepted" {
				broke = true
				break
			}
			currentID = replacement
		}
		if !broke {
			continue // loop exhausted the chain: Python's while/else `continue`
		}
		terminal, found := indexed[currentID]
		if found && str(terminal.Entry["status"]) == "superseded" && str(terminal.Entry["superseded_by"]) == "" {
			out = append(out, diag(rec.Ledger, "DLG066",
				"Supersession chain from `"+entryID+"` has no accepted terminal replacement.",
				"Link the chain to its accepted replacement.", rec.Ledger.Line("id: "+entryID), "", Error))
		}
	}

	out = append(out, pairwiseCandidates(indexed, indexedOrder)...)
	return out
}

// pairwiseCandidates ports the DLG060/061/062/063 block: advisory signals that
// two live decisions may conflict, for a human to judge.
func pairwiseCandidates(indexed map[string]indexedEntry, order []string) []Diagnostic {
	var out []Diagnostic
	type candidate struct {
		id     string
		ledger *Ledger
		entry  map[string]any
	}
	var candidates, rejected []candidate
	for _, id := range order {
		rec := indexed[id]
		switch str(rec.Entry["status"]) {
		case "accepted", "proposed":
			candidates = append(candidates, candidate{id, rec.Ledger, rec.Entry})
		case "rejected":
			rejected = append(rejected, candidate{id, rec.Ledger, rec.Entry})
		}
	}

	for i, left := range candidates {
		for _, right := range candidates[i+1:] {
			if !scopesOverlap(left.entry["scope"], right.entry["scope"]) {
				continue
			}
			leftQuestion := normalizedText(left.entry["question"])
			if leftQuestion != "" && leftQuestion == normalizedText(right.entry["question"]) &&
				normalizedText(left.entry["statement"]) != normalizedText(right.entry["statement"]) {
				out = append(out, diag(left.ledger, "DLG060",
					"`"+left.id+"` and `"+right.id+"` answer the same question differently.",
					"Judge whether they conflict, refine one another, or have disjoint scope.",
					left.ledger.Line("id: "+left.id), "", Candidate))
			}
			if chosenRejected(left.entry, right.entry) || chosenRejected(right.entry, left.entry) {
				out = append(out, diag(left.ledger, "DLG061",
					"`"+left.id+"` and `"+right.id+"` choose and reject the same option.",
					"Judge whether they conflict or have disjoint scope.",
					left.ledger.Line("id: "+left.id), "", Candidate))
			}
			leftTerm := definitionTerm(left.entry)
			if leftTerm != "" && leftTerm == definitionTerm(right.entry) &&
				normalizedText(left.entry["statement"]) != normalizedText(right.entry["statement"]) {
				out = append(out, diag(left.ledger, "DLG062",
					"`"+left.id+"` and `"+right.id+"` define `"+leftTerm+"` differently.",
					"Judge whether the definitions conflict or have disjoint scope.",
					left.ledger.Line("id: "+left.id), "", Candidate))
			}
		}
		for _, old := range rejected {
			negative := strings.TrimRight(normalizedText(old.entry["statement"]), ".!?")
			if negative == "" || !scopesOverlap(left.entry["scope"], old.entry["scope"]) {
				continue
			}
			if strings.Contains(normalizedText(left.entry["statement"]), negative) {
				out = append(out, diag(left.ledger, "DLG063",
					"`"+left.id+"` may select rejected decision `"+old.id+"`.",
					"Judge whether the prior rejection applies to this scope.",
					left.ledger.Line("id: "+left.id), "", Candidate))
			}
		}
	}
	return out
}

// cyclePaths ports cycle_paths(): every distinct cycle in a functional graph,
// each as its lexicographically minimal rotation.
func cyclePaths(graph map[string]string) [][]string {
	found := map[string][]string{}
	for start := range graph {
		var order []string
		positions := map[string]int{}
		current := start
		for {
			if _, ok := graph[current]; !ok {
				break
			}
			if _, seen := positions[current]; seen {
				break
			}
			positions[current] = len(order)
			order = append(order, current)
			current = graph[current]
		}
		at, seen := positions[current]
		if !seen {
			continue
		}
		body := order[at:]
		best := ""
		var bestRot []string
		for i := range body {
			rot := append(append([]string{}, body[i:]...), body[:i]...)
			key := strings.Join(rot, "\x00")
			if best == "" || key < best {
				best, bestRot = key, rot
			}
		}
		found[best] = bestRot
	}
	var keys []string
	for k := range found {
		keys = append(keys, k)
	}
	sortStrings(keys)
	out := make([][]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, found[k])
	}
	return out
}

// normalizedText ports normalized(): lowercase with whitespace collapsed.
func normalizedText(v any) string {
	s, _ := v.(string)
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// scopesOverlap ports scopes_overlap(). An empty or absent scope is unscoped
// and overlaps everything.
func scopesOverlap(left, right any) bool {
	l, lok := left.([]any)
	r, rok := right.([]any)
	if !lok || len(l) == 0 || !rok || len(r) == 0 {
		return true
	}
	for _, a := range l {
		as, ok := a.(string)
		if !ok {
			continue
		}
		for _, b := range r {
			bs, ok := b.(string)
			if !ok {
				continue
			}
			ap := strings.TrimRight(as, "/")
			bp := strings.TrimRight(bs, "/")
			if ap == bp || strings.HasPrefix(ap, bp+"/") || strings.HasPrefix(bp, ap+"/") {
				return true
			}
		}
	}
	return false
}

// chosenRejected ports chosen_rejected().
func chosenRejected(chosen, rejecting map[string]any) bool {
	statement := normalizedText(chosen["statement"])
	list, ok := rejecting["rejected"].([]any)
	if !ok {
		return false
	}
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			continue
		}
		n := normalizedText(s)
		if n != "" && strings.Contains(statement, n) {
			return true
		}
	}
	return false
}

var defQuestionRe = regexp.MustCompile(`(?:what (?:is|does)|define)\s+(.+?)(?:\?|$)`)
var defStatementRe = regexp.MustCompile(`^(.+?)\s+(?:means|is defined as|refers to)\s+`)

// definitionTerm ports definition_term().
func definitionTerm(entry map[string]any) string {
	if str(entry["kind"]) != "definition" {
		return ""
	}
	if q := normalizedText(entry["question"]); q != "" {
		if m := defQuestionRe.FindStringSubmatch(q); m != nil {
			return strings.Trim(m[1], " `\"'")
		}
	}
	if m := defStatementRe.FindStringSubmatch(normalizedText(entry["statement"])); m != nil {
		return strings.Trim(m[1], " `\"'")
	}
	return ""
}

func sortInts(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}

func ascending(xs []int) bool {
	for i := 1; i < len(xs); i++ {
		if xs[i-1] > xs[i] {
			return false
		}
	}
	return true
}
