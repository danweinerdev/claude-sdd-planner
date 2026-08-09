package rules

import (
	"regexp"
	"strings"
)

// Family (f): Validator._phase — SDD061 through SDD069, plus SDD076/SDD077
// (task justification quality, via _task_justification). SDD051 (required
// plan/phase/deliverable fields, via the generic _required helper shared with
// debrief artifacts) is reached from the same Python function but is outside
// this port's assigned range and is intentionally omitted.

func requiredTaskSubsection(body, name string) bool {
	re := regexp.MustCompile(`(?m)^ {0,3}###\s+` + regexp.QuoteMeta(name) + `\s*$`)
	return re.MatchString(body)
}

func init() {
	Register(&Rule{
		Code: "SDD061", Severity: Error, PyFunc: "_phase",
		What: "a phase's `tasks` frontmatter field is not a list",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			if _, ok := a.Meta["tasks"].([]any); ok {
				return
			}
			emit(Diagnostic{
				Code: "SDD061", Severity: Error, Path: a.Rel, Line: 1,
				Message:    "`tasks` must be a list.",
				Correction: "Use `tasks: []` when empty.",
			})
		},
		Bad: []Example{{Name: "tasks-not-list", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasksRaw("tasks: none\n"),
		}}},
		Good: []Example{{Name: "tasks-list", Files: map[string]string{
			"Plans/Sample/01-One.md": validPhase(),
		}}},
	})

	Register(&Rule{
		Code: "SDD062", Severity: Error, PyFunc: "_phase",
		What: "a phase's task entry is not a mapping",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			tasks, ok := a.Meta["tasks"].([]any)
			if !ok {
				return
			}
			for _, t := range tasks {
				if _, ok := t.(map[string]any); ok {
					continue
				}
				emit(Diagnostic{
					Code: "SDD062", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "A task entry is not a mapping.",
					Correction: "Add id, title, status, and verification fields.",
				})
			}
		},
		Bad: []Example{{Name: "task-not-mapping", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasksRaw("tasks:\n  - onlystring\n"),
		}}},
		Good: []Example{{Name: "task-mapping", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD063", Severity: Error, PyFunc: "_phase",
		What: "a phase's task entry is missing id/title/status/verification/justifies",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			for _, t := range asAnyList(a.Meta["tasks"]) {
				m := planEntry(t)
				if m == nil {
					continue
				}
				for _, field := range []string{"id", "title", "status", "verification", "justifies"} {
					if isEmptyMeta(m[field]) {
						emit(Diagnostic{
							Code: "SDD063", Severity: Error, Path: a.Rel, Line: 1,
							Message:    "Task is missing `" + field + "`.",
							Correction: "Add a nonempty `" + field + "`.",
						})
					}
				}
			}
		},
		Bad: []Example{{Name: "missing-verification", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    justifies: FR-01
`),
		}}},
		Good: []Example{{Name: "complete-task", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD064", Severity: Error, PyFunc: "_phase",
		What: "a task id is not `<phase>.N`",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			phaseID := metaStr(a.Meta, "phase")
			re := regexp.MustCompile(`^` + regexp.QuoteMeta(phaseID) + `\.\d+$`)
			for _, t := range asAnyList(a.Meta["tasks"]) {
				m := planEntry(t)
				if m == nil {
					continue
				}
				id := metaStr(m, "id")
				if re.MatchString(id) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD064", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Task id `" + id + "` is not in phase `" + phaseID + "`.",
					Correction: "Use `" + phaseID + ".N`.",
				})
			}
		},
		Bad: []Example{{Name: "wrong-phase-prefix", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "2.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
		Good: []Example{{Name: "matching-prefix", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD065", Severity: Error, PyFunc: "_phase",
		What: "a task's status is not an allowed task status",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			for _, t := range asAnyList(a.Meta["tasks"]) {
				m := planEntry(t)
				if m == nil {
					continue
				}
				status := metaStr(m, "status")
				if isTaskStatus(status) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD065", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Task `" + metaStr(m, "id") + "` has invalid status `" + status + "`.",
					Correction: "Use an allowed task status.",
				})
			}
		},
		Bad: []Example{{Name: "bad-task-status", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: bogus
    verification: x
    justifies: FR-01
`),
		}}},
		Good: []Example{{Name: "good-task-status", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD066", Severity: Error, PyFunc: "_phase",
		What: "a task has no `## <task-id>: ...` body section",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			secs := sections(a, 2)
			for _, t := range asAnyList(a.Meta["tasks"]) {
				m := planEntry(t)
				if m == nil {
					continue
				}
				id := metaStr(m, "id")
				if taskHeadingFor(secs, id) != "" {
					continue
				}
				emit(Diagnostic{
					Code: "SDD066", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Task `" + id + "` has no body section.",
					Correction: "Add `## " + id + ": ...` with task detail sections.",
				})
			}
		},
		Bad: []Example{{Name: "no-body-section", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`, true),
		}}},
		Good: []Example{{Name: "has-body-section", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD067", Severity: Error, PyFunc: "_phase",
		What: "a task's `### Subtasks`/`### Notes`/`### Completion Evidence` is missing or duplicated",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			secs := sections(a, 2)
			for _, t := range asAnyList(a.Meta["tasks"]) {
				m := planEntry(t)
				if m == nil {
					continue
				}
				id := metaStr(m, "id")
				heading := taskHeadingFor(secs, id)
				if heading == "" {
					continue
				}
				info := secs[heading]
				for _, required := range []string{"Subtasks", "Notes", "Completion Evidence"} {
					if requiredTaskSubsection(info.Body, required) {
						continue
					}
					emit(Diagnostic{
						Code: "SDD067", Severity: Error, Path: a.Rel, Line: info.Line,
						Message:    "Task `" + id + "` is missing `### " + required + "`.",
						Correction: "Add it inside the task section.",
					})
				}
				subtasks := headingBodies(info.Body, 3, "Subtasks")
				if len(subtasks) > 1 {
					emit(Diagnostic{
						Code: "SDD067", Severity: Error, Path: a.Rel, Line: info.Line,
						Message:    "Task `" + id + "` has duplicate visible `### Subtasks` sections.",
						Correction: "Keep exactly one Subtasks section inside the task.",
					})
				}
			}
		},
		Bad: []Example{{Name: "missing-notes", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`, false, true),
		}}},
		Good: []Example{{Name: "all-subsections", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD068", Severity: Error, PyFunc: "_phase",
		What: "a complete phase contains a task that is not complete",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" || a.Status() != "complete" {
				return
			}
			secs := sections(a, 2)
			for _, t := range asAnyList(a.Meta["tasks"]) {
				m := planEntry(t)
				if m == nil {
					continue
				}
				id := metaStr(m, "id")
				if taskHeadingFor(secs, id) == "" {
					continue // no body section: SDD066's finding, not double-reported here
				}
				if metaStr(m, "status") == "complete" {
					continue
				}
				emit(Diagnostic{
					Code: "SDD068", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Complete phase contains incomplete task `" + id + "`.",
					Correction: "Complete every task first.",
				})
			}
		},
		Bad: []Example{{Name: "incomplete-task", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseStatus("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
		Good: []Example{{Name: "all-complete", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseStatus("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD069", Severity: Error, PyFunc: "_phase",
		What: "a complete phase/task has an unchecked checkbox in Acceptance Criteria/Subtasks",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			secs := sections(a, 2)
			for _, t := range asAnyList(a.Meta["tasks"]) {
				m := planEntry(t)
				if m == nil {
					continue
				}
				id := metaStr(m, "id")
				heading := taskHeadingFor(secs, id)
				if heading == "" {
					continue
				}
				info := secs[heading]
				subtasks := headingBodies(info.Body, 3, "Subtasks")
				if metaStr(m, "status") == "complete" && len(subtasks) == 1 && hasUncheckedCheckbox(subtasks[0]) {
					emit(Diagnostic{
						Code: "SDD069", Severity: Error, Path: a.Rel, Line: info.Line,
						Message:    "Complete task `" + id + "` has unchecked subtasks.",
						Correction: "Verify and check every subtask.",
					})
				}
			}
			if a.Status() != "complete" {
				return
			}
			criteria, ok := secs["Acceptance Criteria"]
			if ok && hasUncheckedCheckbox(criteria.Body) {
				emit(Diagnostic{
					Code: "SDD069", Severity: Error, Path: a.Rel, Line: criteria.Line,
					Message:    "Complete phase has unchecked acceptance criteria.",
					Correction: "Verify and check every criterion.",
				})
			}
		},
		Bad: []Example{{Name: "unchecked-criteria", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseStatus("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
		}}},
		Good: []Example{{Name: "checked-criteria", Files: map[string]string{
			"Plans/Sample/01-One.md": checkedPhase("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
		}}},
	})
}

func isTaskStatus(s string) bool {
	for _, v := range taskStatusValues {
		if v == s {
			return true
		}
	}
	return false
}

var taskHeadingRe = func(id string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(id) + `(?:\s*:|\s|$)`)
}

func taskHeadingFor(secs map[string]sectionInfo, id string) string {
	re := taskHeadingRe(id)
	best := ""
	bestOrder := -1
	for heading, info := range secs {
		if !re.MatchString(heading) {
			continue
		}
		if bestOrder == -1 || info.Order < bestOrder {
			best, bestOrder = heading, info.Order
		}
	}
	return best
}

var checkboxRe = regexp.MustCompile(`^-\s+\[([ xX])\]`)

func hasUncheckedCheckbox(body string) bool {
	for _, l := range markdownLines(body) {
		_, stripped := markdownIndentation(l.Visible)
		m := checkboxRe.FindStringSubmatch(stripped)
		if m != nil && m[1] == " " {
			return true
		}
	}
	return false
}

// justificationPhrases ports sdd_validate.py's JUSTIFICATION_PLACEHOLDERS.
// Go's regexp (RE2) has no lookaround, so the Python pattern's leading
// `(?:^|(?<=[\s(\[,;:—-]))` anchor and its trailing `(?![\w/-])` on
// TBD/TODO/N/A/NA are reproduced with explicit boundary checks in
// isJustificationPlaceholder instead of being folded into the regexes below.
var justificationPhrases = []*regexp.Regexp{
	regexp.MustCompile(`(?i)might(?:\s+be)?\s+needed?(?:\s+(?:it|this|later|in\s+(?:the\s+)?future))*`),
	regexp.MustCompile(`(?i)may(?:\s+be)?\s+needed?`),
	regexp.MustCompile(`(?i)(?:just\s+)?in\s+case`),
	regexp.MustCompile(`(?i)for\s+(?:the\s+sake\s+of\s+)?(?:completeness|symmetry|consistency|parity)`),
	regexp.MustCompile(`(?i)(?:for|to\s+match)\s+(?:consistency|symmetry)\s+with`),
	regexp.MustCompile(`(?i)nice\s+to\s+have`),
	regexp.MustCompile(`(?i)future[\s-]*(?:proofing|proof|use|need|work|expansion)`),
	regexp.MustCompile(`(?i)(?:good|best)\s+practice`),
	regexp.MustCompile(`(?i)standard\s+practice`),
	regexp.MustCompile(`(?i)part\s+of\s+the\s+(?:architecture|design|plan|refactor)`),
}

var justificationNeededRe = regexp.MustCompile(`(?i)(?:it\s+is\s+|it's\s+)?(?:needed|required|necessary)\s*\.?\s*$`)

func isJustificationPlaceholder(text string) bool {
	for _, re := range justificationPhrases {
		if re.MatchString(text) {
			return true
		}
	}
	if justificationNeededRe.MatchString(strings.TrimSpace(text)) {
		return true
	}
	lower := strings.ToLower(text)
	for _, kw := range []string{"tbd", "todo", "n/a", "na"} {
		start := 0
		for {
			i := strings.Index(lower[start:], kw)
			if i < 0 {
				break
			}
			pos := start + i
			end := pos + len(kw)
			start = end
			if end < len(lower) {
				c := lower[end]
				if isWordByte(c) || c == '/' || c == '-' {
					continue
				}
			}
			return true
		}
	}
	return false
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// echoesTitle ports Validator._echoes_title: true when a justification adds no
// content words beyond the task title.
func echoesTitle(justification, title string) bool {
	stop := map[string]bool{
		"a": true, "an": true, "the": true, "to": true, "of": true, "for": true,
		"and": true, "or": true, "in": true, "on": true, "at": true, "by": true,
		"with": true, "is": true, "are": true, "be": true, "this": true, "that": true,
		"it": true, "its": true, "we": true, "so": true, "as": true, "from": true,
		"into": true, "add": true, "adds": true, "added": true, "adding": true,
		"implement": true, "implements": true, "implemented": true, "implementing": true,
		"task": true,
	}
	stem := func(word string) string {
		for _, suffix := range []string{"ing", "ed", "es", "s"} {
			if strings.HasSuffix(word, suffix) && len(word)-len(suffix) >= 4 {
				return word[:len(word)-len(suffix)]
			}
		}
		return word
	}
	content := func(text string) map[string]bool {
		out := map[string]bool{}
		for _, w := range wordRe.FindAllString(strings.ToLower(text), -1) {
			if !stop[w] {
				out[stem(w)] = true
			}
		}
		return out
	}
	titleWords := content(title)
	justWords := content(justification)
	if len(titleWords) == 0 || len(justWords) == 0 {
		return false
	}
	for w := range justWords {
		if !titleWords[w] {
			return false
		}
	}
	return true
}

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

func init() {
	Register(&Rule{
		Code: "SDD076", Severity: Error, PyFunc: "_task_justification",
		What: "a task's `justifies` is a placeholder rather than a stated demand",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			for _, t := range asAnyList(a.Meta["tasks"]) {
				m := planEntry(t)
				if m == nil {
					continue
				}
				value, ok := m["justifies"].(string)
				if !ok || strings.TrimSpace(value) == "" {
					continue
				}
				text := strings.TrimSpace(value)
				if !isJustificationPlaceholder(text) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD076", Severity: Error, Path: a.Rel, Line: 1,
					Message: "Task `" + metaStr(m, "id") + "` justifies itself with a placeholder: " +
						pyRepr(text) + ".",
					Correction: "State the demand: cite the FR-NN/NFR-NN/AC-NN/D-NNNN ids the task " +
						"serves, or name the concrete failure it prevents. A task with no " +
						"such demand is cut, not annotated.",
				})
			}
		},
		Bad: []Example{{Name: "placeholder-justification", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: might be needed later
`),
		}}},
		Good: []Example{{Name: "sourced-justification", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD077", Severity: Error, PyFunc: "_task_justification",
		What: "a task's `justifies` only restates its title",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			for _, t := range asAnyList(a.Meta["tasks"]) {
				m := planEntry(t)
				if m == nil {
					continue
				}
				value, ok := m["justifies"].(string)
				if !ok || strings.TrimSpace(value) == "" {
					continue
				}
				text := strings.TrimSpace(value)
				if isJustificationPlaceholder(text) {
					continue
				}
				if citeFRRe.MatchString(text) || citeNFRRe.MatchString(text) ||
					citeACRe.MatchString(text) || citeDRe.MatchString(text) {
					continue
				}
				title, _ := m["title"].(string)
				if !echoesTitle(text, title) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD077", Severity: Error, Path: a.Rel, Line: 1,
					Message: "Task `" + metaStr(m, "id") + "` justifies itself by restating its title: " +
						pyRepr(text) + ".",
					Correction: "`justifies` says why the task should be started, not what it does. " +
						"Cite the ids it serves, or name the failure it prevents.",
				})
			}
		},
		Bad: []Example{{Name: "echoes-title", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: Add retry logic
    status: planned
    verification: x
    justifies: Adds the retry logic
`),
		}}},
		Good: []Example{{Name: "distinct-justification", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: Add retry logic
    status: planned
    verification: x
    justifies: Prevents dropped webhooks when the upstream 503s
`),
		}}},
	})
}

// pyRepr renders a string the way Python's repr() would for the plain ASCII
// text this check sees: single-quoted, with embedded single quotes escaped.
// Diagnostics interpolate `{text!r}`, and the parity oracle compares message
// text verbatim, so this has to match Python's quoting rather than Go's.
func pyRepr(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'':
			b.WriteString("\\'")
		case '\\':
			b.WriteString("\\\\")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}
