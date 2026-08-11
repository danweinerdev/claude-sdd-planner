package dlg

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// isoDate ports iso_date(): a real YYYY-MM-DD calendar date, or "" when the
// value is not one.
//
// Python rejects a datetime outright (only a plain date counts) and validates
// the calendar, so 2024-02-30 fails. Because nodeValue keeps scalars as source
// text, a YAML timestamp arrives here as its written form and is checked the
// same way.
func isoDate(v any) string {
	s, ok := v.(string)
	if !ok || !isoDateRe.MatchString(s) {
		return ""
	}
	year, _ := strconv.Atoi(s[0:4])
	month, _ := strconv.Atoi(s[5:7])
	day, _ := strconv.Atoi(s[8:10])
	if month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) {
		return ""
	}
	return s
}

func daysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	}
	return 0
}

// safeScope ports safe_scope(): a repository-relative path with no absolute
// prefix, drive letter, backslash, or traversal segment.
func safeScope(value string) bool {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
		return false
	}
	if strings.HasPrefix(value, "/") {
		return false
	}
	if strings.Contains(value, `\`) || driveLetterRe.MatchString(value) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

var driveLetterRe = regexp.MustCompile(`^[A-Za-z]:`)

// validateStringList ports validate_string_list: DLG027 for a non-list,
// DLG028 for a list holding a non-string or blank entry.
func validateStringList(l *Ledger, entryID, field string, value any, out *[]Diagnostic) {
	list, ok := value.([]any)
	if !ok {
		*out = append(*out, diag(l, "DLG027",
			"Decision `"+entryID+"` field `"+field+"` is not a list.",
			"Use `"+field+": []` or a list of nonempty strings.", l.Line(field+":"), "", Error))
		return
	}
	for _, item := range list {
		if isNonemptyString(item) {
			continue
		}
		*out = append(*out, diag(l, "DLG028",
			"Decision `"+entryID+"` field `"+field+"` contains a non-string or empty value.",
			"Use only nonempty strings.", l.Line(field+":"), "", Error))
		return
	}
}

var bodySectionRe = regexp.MustCompile(`(?m)^ {0,3}##\s+(D-\d{4,9})\b`)

// ValidateLedger ports validate_ledger. It returns the diagnostics and the
// entries that parsed as mappings, which the collection phase then compares
// across ledgers.
func ValidateLedger(l *Ledger) ([]Diagnostic, []map[string]any) {
	var out []Diagnostic
	meta := l.Meta

	if isSymlink(l.Path) {
		out = append(out, diag(l, "DLG019", "Ledger path is a symbolic link.",
			"Store the canonical ledger and archives as regular files in their owning repository.", 1, "", Error))
	}

	for _, field := range []string{"title", "type", "status", "created", "updated", "tags", "related", "decisions"} {
		if isMissing(meta, field) {
			out = append(out, diag(l, "DLG010",
				"Required ledger field `"+field+"` is missing or empty.",
				"Add a valid `"+field+"` value.", l.Line(field+":"), "", Error))
		}
	}
	if !isNonemptyString(meta["title"]) {
		out = append(out, diag(l, "DLG011", "Ledger `title` must be a nonempty string.",
			"Set a descriptive string title.", l.Line("title:"), "", Error))
	}
	if str(meta["type"]) != "decision-log" {
		out = append(out, diag(l, "DLG012", "Ledger `type` is not `decision-log`.",
			"Set `type: decision-log`.", l.Line("type:"), "", Error))
	}
	ledgerStatus := str(meta["status"])
	if ledgerStatus != "active" && ledgerStatus != "archived" {
		out = append(out, diag(l, "DLG013", "Ledger status must be `active` or `archived`.",
			"Use the lifecycle status matching the file role.", l.Line("status:"), "", Error))
	}
	created := isoDate(meta["created"])
	updated := isoDate(meta["updated"])
	for _, f := range [2]struct {
		field string
		value string
	}{{"created", created}, {"updated", updated}} {
		if f.value == "" {
			out = append(out, diag(l, "DLG014",
				"Ledger `"+f.field+"` must be a real YYYY-MM-DD date.",
				"Set `"+f.field+"` to an ISO calendar date.", l.Line(f.field+":"), "", Error))
		}
	}
	if created != "" && updated != "" && created > updated {
		out = append(out, diag(l, "DLG015", "Ledger `created` is later than `updated`.",
			"Correct the ledger dates.", l.Line("updated:"), "", Error))
	}
	for _, field := range [2]string{"tags", "related"} {
		validateStringList(l, "<ledger>", field, meta[field], &out)
	}

	name := filepath.Base(l.Path)
	archived := archiveNameRe.MatchString(name)
	canonical := canonicalName(l.Path)
	if !archived && !canonical {
		out = append(out, diag(l, "DLG016", "Noncanonical ledger filename `"+name+"`.",
			"Use `Decisions/decisions.md`, repository-root `DECISIONS.md`, or `archive-YYYY.md`.", 1, "", Error))
	}
	if archived && ledgerStatus != "archived" {
		out = append(out, diag(l, "DLG017", "Archive filename does not have `status: archived`.",
			"Set the archive status to `archived`.", l.Line("status:"), "", Error))
	}
	if canonical && ledgerStatus != "active" {
		out = append(out, diag(l, "DLG018", "Canonical ledger does not have `status: active`.",
			"Keep the canonical ledger active.", l.Line("status:"), "", Error))
	}
	if name == "DECISIONS.md" {
		if repo := gitRoot(l.Path); repo != "" && !sameDir(filepath.Dir(l.Path), repo) {
			out = append(out, diag(l, "DLG042", "External `DECISIONS.md` is not at the repository root.",
				"Move it to the root of the repository it represents.", 1, "", Error))
		}
	}

	entries, ok := meta["decisions"].([]any)
	if !ok {
		out = append(out, diag(l, "DLG020", "`decisions` must be a YAML list.",
			"Use `decisions: []` when empty.", l.Line("decisions:"), "", Error))
		return out, nil
	}

	var valid []map[string]any
	latest := ""
	for index, raw := range entries {
		entry, isMap := raw.(map[string]any)
		if !isMap {
			out = append(out, diag(l, "DLG021",
				"Decision at index "+strconv.Itoa(index)+" is not a mapping.",
				"Use the documented decision entry mapping.", l.Line("decisions:"), "", Error))
			continue
		}
		entryID := "<index " + strconv.Itoa(index) + ">"
		if s, ok := entry["id"].(string); ok {
			entryID = s
		}
		idLine := l.Line("id: " + entryID)

		for _, field := range []string{"id", "kind", "status", "date", "decided_by", "statement", "rationale"} {
			if isMissing(entry, field) {
				out = append(out, diag(l, "DLG022",
					"Decision `"+entryID+"` is missing `"+field+"`.",
					"Add a nonempty `"+field+"`.", idLine, "", Error))
			}
		}
		if id, ok := entry["id"].(string); !ok || !decisionIDRe.MatchString(id) {
			out = append(out, diag(l, "DLG023", "Decision `"+entryID+"` has an invalid id.",
				"Use `D-NNNN` with at least four digits.", idLine, "", Error))
		}
		if !kinds[str(entry["kind"])] {
			out = append(out, diag(l, "DLG024", "Decision `"+entryID+"` has an invalid kind.",
				"Use one of: "+sortedKeys(kinds)+".", l.Line("kind:"), "", Error))
		}
		entryStatus := str(entry["status"])
		if !statuses[entryStatus] {
			out = append(out, diag(l, "DLG025", "Decision `"+entryID+"` has an invalid status.",
				"Use one of: "+sortedKeys(statuses)+".", l.Line("status:"), "", Error))
		}
		decidedBy := str(entry["decided_by"])
		if !deciders[decidedBy] {
			out = append(out, diag(l, "DLG026", "Decision `"+entryID+"` has an invalid `decided_by`.",
				"Use `agent`, `user`, or `user-approved` as allowed by lifecycle status.", l.Line("decided_by:"), "", Error))
		}
		if decidedBy == "agent" && entryStatus != "proposed" {
			out = append(out, diag(l, "DLG041",
				"Decision `"+entryID+"` attributes a non-proposed entry to `agent`.",
				"Only an unconfirmed proposal may use `decided_by: agent`; user acceptance changes provenance to `user-approved`.",
				l.Line("decided_by:"), "", Error))
		}
		if decidedBy == "user-approved" && entryStatus != "accepted" && entryStatus != "superseded" {
			out = append(out, diag(l, "DLG044",
				"Decision `"+entryID+"` uses `user-approved` with status `"+pyValue(entry["status"])+"`.",
				"Use `user-approved` only for an accepted agent proposal and its later superseded state.",
				l.Line("decided_by:"), "", Error))
		}
		for _, field := range [2]string{"statement", "rationale"} {
			if !isNonemptyString(entry[field]) {
				out = append(out, diag(l, "DLG029",
					"Decision `"+entryID+"` field `"+field+"` must be a nonempty string.",
					"Record a nonempty `"+field+"` string.", l.Line(field+":"), "", Error))
			}
		}
		entryDate := isoDate(entry["date"])
		if entryDate == "" {
			out = append(out, diag(l, "DLG030",
				"Decision `"+entryID+"` date must be a real YYYY-MM-DD date.",
				"Set an ISO calendar date.", l.Line("date:"), "", Error))
		} else if latest == "" || entryDate > latest {
			latest = entryDate
		}
		if str(entry["kind"]) == "answered-question" && !isNonemptyString(entry["question"]) {
			out = append(out, diag(l, "DLG031",
				"Answered question `"+entryID+"` lacks a nonempty `question`.",
				"Record the question that was answered.", idLine, "", Error))
		}
		for _, field := range [2]string{"question", "confirmation"} {
			if _, present := entry[field]; present && !isNonemptyString(entry[field]) {
				out = append(out, diag(l, "DLG032",
					"Decision `"+entryID+"` field `"+field+"` must be a nonempty string when present.",
					"Remove it or record a nonempty `"+field+"`.", l.Line(field+":"), "", Error))
			}
		}
		for _, field := range [3]string{"rejected", "scope", "tags"} {
			if _, present := entry[field]; present {
				validateStringList(l, entryID, field, entry[field], &out)
			}
		}
		if scope, ok := entry["scope"].([]any); ok {
			for _, v := range scope {
				s, isStr := v.(string)
				if !isStr || safeScope(s) {
					continue
				}
				out = append(out, diag(l, "DLG033",
					"Decision `"+entryID+"` has unsafe scope `"+s+"`.",
					"Use a repository-relative path without backslashes, `.` or `..` segments.",
					l.Line("scope:"), "", Error))
			}
		}
		if _, present := entry["refresh_when"]; present {
			validateStringList(l, entryID, "refresh_when", entry["refresh_when"], &out)
			if str(entry["kind"]) != "assumption" {
				out = append(out, diag(l, "DLG034",
					"Non-assumption `"+entryID+"` declares `refresh_when`.",
					"Use refresh triggers only on assumption entries.", l.Line("refresh_when:"), "", Error))
			}
		}
		if v, present := entry["reversibility"]; present && !reversibility[str(v)] {
			out = append(out, diag(l, "DLG035", "Decision `"+entryID+"` has invalid reversibility.",
				"Use `one-way` or `two-way`.", l.Line("reversibility:"), "", Error))
		}
		for _, field := range [2]string{"supersedes", "superseded_by"} {
			v, present := entry[field]
			if !present {
				continue
			}
			s, isStr := v.(string)
			if !isStr || !decisionIDRe.MatchString(s) {
				out = append(out, diag(l, "DLG036",
					"Decision `"+entryID+"` field `"+field+"` is not a decision id.",
					"Use an existing `D-NNNN` id.", l.Line(field+":"), "", Error))
			}
		}
		if ledgerStatus == "archived" && (entryStatus == "accepted" || entryStatus == "proposed") {
			out = append(out, diag(l, "DLG037",
				"Archive contains live decision `"+entryID+"` with status `"+pyValue(entry["status"])+"`.",
				"Keep accepted and proposed entries in the canonical active ledger.", idLine, "", Error))
		}
		valid = append(valid, entry)
	}

	if updated != "" && latest != "" && updated < latest {
		out = append(out, diag(l, "DLG038", "Ledger `updated` predates its newest decision.",
			"Advance `updated` to at least the newest decision date.", l.Line("updated:"), "", Error))
	}

	// Optional per-decision body sections must correspond to real entries and
	// appear at most once.
	var bodyIDs []string
	for _, m := range bodySectionRe.FindAllStringSubmatch(visibleBody(l.Body), -1) {
		bodyIDs = append(bodyIDs, m[1])
	}
	known := map[string]bool{}
	for _, e := range valid {
		if id, ok := e["id"].(string); ok {
			known[id] = true
		}
	}
	counts := map[string]int{}
	for _, id := range bodyIDs {
		counts[id]++
	}
	var dupes, unknown []string
	for id, n := range counts {
		if n > 1 {
			dupes = append(dupes, id)
		}
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	sortStrings(dupes)
	sortStrings(unknown)
	for _, id := range dupes {
		out = append(out, diag(l, "DLG039", "Body section `"+id+"` is duplicated.",
			"Keep at most one optional body section per decision.", l.BodyLine, "", Error))
	}
	for _, id := range unknown {
		out = append(out, diag(l, "DLG040", "Body section `"+id+"` has no frontmatter entry.",
			"Add the canonical frontmatter entry or remove the stale body section.", l.BodyLine, "", Error))
	}
	return out, valid
}

// str renders a value as a string for comparison, "" for anything else.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// pyValue renders a value the way Python interpolates it into an f-string:
// None for a missing value, otherwise its text.
func pyValue(v any) string {
	if v == nil {
		return "None"
	}
	if s, ok := v.(string); ok {
		return s
	}
	if b, ok := v.(bool); ok {
		if b {
			return "True"
		}
		return "False"
	}
	return ""
}
