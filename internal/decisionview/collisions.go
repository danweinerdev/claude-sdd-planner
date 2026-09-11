package decisionview

import (
	"regexp"
	"strings"
)

// CollisionCandidates reports signals, never a semantic verdict or precedence
// rule. A nil overlap callback is conservative until artifact-related context
// is available: it cannot dismiss a possibly connected scope as unrelated.
func CollisionCandidates(records []ResolvedDecision, overlap func(any, any) bool) []Diagnostic {
	if overlap == nil {
		overlap = func(any, any) bool { return true }
	}
	var live, rejected []ResolvedDecision
	for _, r := range records {
		if r.Applicability == "binding" || r.OriginalStatus == "proposed" {
			live = append(live, r)
		}
		if r.OriginalStatus == "rejected" {
			rejected = append(rejected, r)
		}
	}
	var out []Diagnostic
	add := func(code string, a, b ResolvedDecision, reason string) {
		correction := "Judge whether the decisions conflict, refine one another or have disjoint scope; no automatic resolution is authorized."
		if a.Original["reversibility"] == "one-way" || b.Original["reversibility"] == "one-way" {
			correction += " A one-way constraint is involved; escalate explicitly."
		}
		out = append(out, Diagnostic{Code: code, Severity: Candidate, Path: a.Source.Path, Message: string(a.ID) + " and " + string(b.ID) + " " + reason, Correction: correction})
	}
	for i, a := range live {
		for _, b := range live[i+1:] {
			if !overlap(a.Original["scope"], b.Original["scope"]) {
				continue
			}
			q := collisionText(a.Original["question"])
			if q != "" && q == collisionText(b.Original["question"]) && collisionText(a.Original["statement"]) != collisionText(b.Original["statement"]) {
				add("DLG060", a, b, "answer the same question differently")
			}
			if collisionRejected(a.Original, b.Original) || collisionRejected(b.Original, a.Original) {
				add("DLG061", a, b, "choose and reject the same option")
			}
			term := collisionTerm(a.Original)
			if term != "" && term == collisionTerm(b.Original) && collisionText(a.Original["statement"]) != collisionText(b.Original["statement"]) {
				add("DLG062", a, b, "define the same term differently")
			}
		}
		for _, b := range rejected {
			negative := strings.TrimRight(collisionText(b.Original["statement"]), ".!?")
			if negative != "" && overlap(a.Original["scope"], b.Original["scope"]) && strings.Contains(collisionText(a.Original["statement"]), negative) {
				add("DLG063", a, b, "may select an explicitly rejected decision")
			}
		}
	}
	return out
}
func collisionText(value any) string {
	s, _ := value.(string)
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}
func collisionRejected(chosen, rejecting map[string]any) bool {
	statement := collisionText(chosen["statement"])
	list, _ := rejecting["rejected"].([]any)
	for _, item := range list {
		if negative := collisionText(item); negative != "" && strings.Contains(statement, negative) {
			return true
		}
	}
	return false
}

var collisionQuestionTerm = regexp.MustCompile(`(?:what (?:is|does)|define)\s+(.+?)(?:\?|$)`)
var collisionStatementTerm = regexp.MustCompile(`^(.+?)\s+(?:means|is defined as|refers to)\s+`)

func collisionTerm(entry map[string]any) string {
	if entry["kind"] != "definition" {
		return ""
	}
	if m := collisionQuestionTerm.FindStringSubmatch(collisionText(entry["question"])); m != nil {
		return strings.Trim(m[1], " `\"'")
	}
	if m := collisionStatementTerm.FindStringSubmatch(collisionText(entry["statement"])); m != nil {
		return strings.Trim(m[1], " `\"'")
	}
	return ""
}
