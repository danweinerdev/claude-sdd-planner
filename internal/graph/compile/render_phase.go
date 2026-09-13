package compile

// Phase-doc rendering: renderPhaseDoc projects one phase's nodes into a
// generated markdown view (Designs/SddGraph DD-1, DD-2), and planWrite/
// writeView/fillDates carry the write-path invariants that projection
// depends on — the generated-view marker refusal, the frozen-view refusal
// (and its reopen/reclose escape), and date/placeholder stability. Split
// out of render.go at the phase-doc/README seam (task 6): a pure move, no
// behavior change.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// renderPhaseDoc produces one phase view. created/updated are supplied by
// the writer so unchanged content stays byte-identical across days. st and
// closed carry the derived truth the view projects (DD-2: a projection may
// show derived state precisely because it is never parsed back).
func renderPhaseDoc(planDir, plan string, g *model.Graph, ph phaseGroup, created, updated, repoRoot string, repo vcs.Repo, st map[string]states.NodeState, closed map[string]bool) (string, error) {
	frozen := allClosed(ph.Nodes, closed)
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: \"%s\"\ntype: phase\nplan: \"%s\"\nphase: %d\nstatus: %s\n", ph.Title, plan, ph.Ordinal, phaseStatus(ph.Nodes, closed))
	fmt.Fprintf(&b, "created: %s\nupdated: %s\n", created, updated)
	fmt.Fprintf(&b, "deliverable: \"Graph view: %d node(s) under phase label %s\"\ntasks: []\n---\n\n", len(ph.Nodes), ph.Title)
	fmt.Fprintf(&b, "# Phase %d: %s\n\n%s\n\n", ph.Ordinal, ph.Title, viewMarker(plan))
	if frozen {
		fmt.Fprintf(&b, "%s\n\n", frozenViewMarker)
	}

	b.WriteString("## Overview\n\n")
	fmt.Fprintf(&b, "Rendered view of %d node(s) from the plan graph (schema v%d, seq %d).\n", len(ph.Nodes), g.Version, g.SeqCounter)
	b.WriteString("Observations shown are raw records; completion-grade closure derives from\nfull review gates and is never stored or hand-edited here.\n\n")

	b.WriteString("## Nodes\n")
	for _, n := range ph.Nodes {
		fmt.Fprintf(&b, "\n### %s\n\n", n.ID)
		fmt.Fprintf(&b, "- Contract: %s\n", n.Contract)
		fmt.Fprintf(&b, "- Justifies: %s\n", joinOr(n.Justifies, "(nothing — will not compile)"))
		fmt.Fprintf(&b, "- Depends on: %s\n", joinOr(n.Deps, "(nothing)"))
		fmt.Fprintf(&b, "- Gate: %s\n", describeGate(n.Gate))
		fmt.Fprintf(&b, "- Hazards: %s\n", describeHazards(n.Hazards))
		if len(n.Artifacts) > 0 {
			fmt.Fprintf(&b, "- Artifacts: %s\n", strings.Join(n.Artifacts, ", "))
		}
		if len(n.Inputs) > 0 {
			fmt.Fprintf(&b, "- Inputs: %s\n", describeInputs(n.Inputs))
		}
		fmt.Fprintf(&b, "- Estimate: %d\n", n.Estimate)
		if n.History != "" {
			fmt.Fprintf(&b, "- History: %s\n", n.History)
		}
		fmt.Fprintf(&b, "- Observation: %s\n", describeObservation(n.Verification))
		fmt.Fprintf(&b, "- Closure: %s\n", describeClosure(n.ID, st, closed))
		if n.Claim != nil {
			fmt.Fprintf(&b, "- Claim: %s (lease expires %s)\n", n.Claim.By, n.Claim.LeaseExpires)
		}
	}

	b.WriteString("\n## Acceptance Criteria\n\n")
	box := "[ ]"
	if frozen {
		box = "[x]"
	}
	fmt.Fprintf(&b, "- %s Every node in this phase is truly closed: a passing observation, and\n      coverage by a passing frozen full review gate (derived from the graph;\n      never checked off by hand).\n\n", box)
	if frozen {
		before, after, err := renderPhaseEvidenceHalves(planDir, plan, ph, repoRoot, "{VERIFIED_DATE}")
		if err != nil {
			return "", err
		}
		// Skip the identity probe entirely when this render would be a
		// byte-identical no-op: compare everything EXCEPT the identity-
		// recheck line itself (the only piece a probe produces) against the
		// existing on-disk frozen view with ITS OWN identity-recheck line
		// similarly excised. Only when nothing else would change is the
		// existing line reused verbatim instead of re-probing revision
		// existence; any other difference (a node field, a review now
		// covering the phase, ...) still falls through to a real probe, so
		// the frozen-view refusal for a genuine change is never masked
		// (review-execution ef1962e F-01 item 4).
		// A nil repo never reaches identityRecheckLine's real probe branch
		// (it renders "no recheck ran" unconditionally) — there is nothing
		// to memoize, and reusing a stale existing line here would mask a
		// resolution regression as a no-op instead of letting planWrite's
		// frozen-view comparison see and refuse it (item 7).
		if existing, rerr := os.ReadFile(filepath.Join(planDir, ph.Doc)); rerr == nil && repo != nil {
			if existingCore, existingBody, found := stripPhaseEvidenceSection(string(existing)); found &&
				strings.Contains(string(existing), frozenViewMarker) &&
				strings.TrimSpace(existingBody) != "Pending — not complete." {
				if loc := identityRecheckLineRe.FindStringIndex(existingBody); loc != nil {
					existingLine := existingBody[loc[0]:loc[1]]
					existingBefore := existingBody[:loc[0]]
					existingAfter := existingBody[loc[1]:]
					normalizeCore := func(s string) string {
						return createdLineRe.ReplaceAllString(updatedLineRe.ReplaceAllString(s, "updated: {DATE}"), "created: {DATE}")
					}
					normalizeBefore := func(s string) string {
						return verifiedLineRe.ReplaceAllString(s, "- Verified: {VERIFIED_DATE}")
					}
					wantCore := strings.TrimRight(b.String(), "\n")
					if normalizeCore(wantCore) == normalizeCore(existingCore) &&
						normalizeBefore(before) == normalizeBefore(existingBefore) &&
						strings.TrimRight(after, "\n") == strings.TrimRight(existingAfter, "\n") {
						b.WriteString("## Phase Completion Evidence\n\n")
						b.WriteString(existingBefore)
						b.WriteString(existingLine)
						b.WriteString(existingAfter)
						b.WriteByte('\n')
						return b.String(), nil
					}
				}
			}
		}
		b.WriteString("## Phase Completion Evidence\n\n")
		rev := phaseCheckpoint(ph.Nodes)
		line, err := identityRecheckLine(repo, rev, "{VERIFIED_DATE}")
		if err != nil {
			return "", err
		}
		b.WriteString(strings.TrimRight(before+line+after, "\n") + "\n")
	} else {
		b.WriteString("## Phase Completion Evidence\n\n")
		b.WriteString("Pending — not complete.\n")
	}
	return b.String(), nil
}

// describeClosure projects the two-axis closure distinction (D-0022):
// closed vs assumed-closed for GREEN nodes, the derived state otherwise.
// A nil st/closed (a caller without derive inputs) reads as the zero state.
func describeClosure(id string, st map[string]states.NodeState, closed map[string]bool) string {
	switch {
	case closed[id]:
		return "**closed** — GREEN and covered by a passing frozen full review gate"
	case st[id].State == states.Green:
		return "assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)"
	case st[id].State == "":
		return "underived (no state inputs at render time)"
	default:
		return fmt.Sprintf("open — state %s", st[id].State)
	}
}

func joinOr(values []string, empty string) string {
	if len(values) == 0 {
		return empty
	}
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "`" + v + "`"
	}
	return strings.Join(quoted, ", ")
}

func describeGate(gate model.Gate) string {
	switch gate.Type {
	case model.GateTests:
		if len(gate.Tests) == 0 {
			return "tests (none named yet)"
		}
		var parts []string
		for _, t := range gate.Tests {
			s := fmt.Sprintf("`%s` in %s", t.ID, t.File)
			if len(t.Satisfies) > 0 {
				s += fmt.Sprintf(" (satisfies %s)", strings.Join(t.Satisfies, ", "))
			}
			parts = append(parts, s)
		}
		return "tests — " + strings.Join(parts, "; ")
	case model.GateCommand:
		return fmt.Sprintf("command — `%s`", gate.Command)
	case model.GateReview:
		if gate.Lanes == nil {
			return "review — full (carries completion-grade closure)"
		}
		return "review — lanes: " + strings.Join(gate.Lanes, ", ")
	case model.GateUnspecified:
		return "UNSPECIFIED (conversion sentinel; blocks compile until an operator states how this node is verified)"
	default:
		return gate.Type
	}
}

func describeHazards(h model.Hazards) string {
	switch {
	case h == nil:
		return "UNTRIAGED (blocks compile until resolved)"
	case len(h) == 0:
		return "none (explicit claim)"
	default:
		return strings.Join(h, ", ")
	}
}

// describeInputs renders a node's declared read-only inputs for views:
// `root:path` (whole file) or `root:path#Heading / Path` (section).
func describeInputs(inputs []model.Input) string {
	parts := make([]string, len(inputs))
	for i, in := range inputs {
		parts[i] = "`" + describeInputSpec(in) + "`"
	}
	return strings.Join(parts, ", ")
}

func describeObservation(v *model.Verification) string {
	if v == nil {
		return "none yet"
	}
	s := fmt.Sprintf("**%s** at seq %d — isolation %s", v.Result, v.Seq, v.Isolation)
	if v.Provenance != nil {
		p := v.Provenance.Kind
		if v.Provenance.Revision != "" {
			p += " " + shortRev(v.Provenance.Revision)
		}
		if v.Provenance.Changelist != "" {
			p += " CL " + v.Provenance.Changelist
		}
		s += ", provenance " + p
	}
	return s
}

func shortRev(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

var (
	createdLineRe = regexp.MustCompile(`(?m)^created: (\S+)`)
	updatedLineRe = regexp.MustCompile(`(?m)^updated: (\S+)`)
	// planEvidenceHeadingRe locates the evidence section the graph-view
	// insertion must stay ahead of.
	planEvidenceHeadingRe = regexp.MustCompile(`(?m)^## Plan Completion Evidence\s*$`)
	// identityRecheckLineRe matches the single `- Identity recheck: ...`
	// line (plus its trailing blank line) identityRecheckLine renders —
	// the only piece of a phase's evidence body a real repository probe
	// produces. renderPhaseDoc's no-op short-circuit excises exactly this
	// span from both the wanted and existing evidence bodies before
	// deciding whether the probe can be skipped.
	identityRecheckLineRe = regexp.MustCompile(`(?m)^- Identity recheck:.*\n\n`)
	// verifiedLineRe matches evidence's `- Verified: DATE` line, filled
	// with the SAME `updated` stamp fillDates uses — normalizing it
	// alongside created/updated lets renderPhaseDoc's no-op short-circuit
	// compare a freshly rendered ({VERIFIED_DATE}-templated) evidence body
	// against the existing on-disk (already date-filled) one.
	verifiedLineRe = regexp.MustCompile(`(?m)^- Verified: (\S+)`)
	// legacyGraphViewHeadingRe recognizes an orphaned `## Graph View`
	// section left by a render that predates the begin marker: legacyGraphViewSpan
	// uses it to find and replace the whole legacy span instead of
	// upserting a second section alongside it.
	legacyGraphViewHeadingRe = regexp.MustCompile(`(?m)^## Graph View\s*$`)
	// observationLineRe matches a node's `- Observation: ...` line (plus its
	// trailing newline) — the one per-node line whose content legitimately
	// changes across a reopen (a review finding demotes a node) and reclose
	// (it re-verifies): a fresh seq, result, isolation, and provenance
	// revision. reclosedNoOtherChange excises exactly this span from both
	// renderings before deciding whether a frozen-view difference is a
	// genuine history change or only a legitimate reclose.
	observationLineRe = regexp.MustCompile(`(?m)^- Observation: .*\n`)
)

// reclosedNoOtherChange reports whether existingCore and contentCore (both
// already date-normalized) differ ONLY in their per-node `- Observation:
// ...` lines, with contentCore's phase now closed again (frozen). That is
// exactly the shape a legitimate reopen-then-reclose leaves behind: a
// node's contract_rev or verification moved (so its Observation line
// changed and, upstream, the CLOSED predicate briefly went false and then
// true again), while every other projected field (Contract, Gate, Hazards,
// Justifies, ...) is unchanged. Any other difference — a node's own content
// changing, or the phase failing to re-close — falls through to the
// ordinary frozen-view refusal.
func reclosedNoOtherChange(existingCore, contentCore string, contentFrozen bool) bool {
	if !contentFrozen {
		return false
	}
	strip := func(s string) string { return observationLineRe.ReplaceAllString(s, "") }
	if strip(existingCore) != strip(contentCore) {
		return false
	}
	// Require at least one Observation line to actually differ — otherwise
	// this is the ordinary byte-identical case already handled earlier, and
	// treating it as a "reclose" would be a no-op label change, not wrong,
	// but reclosedNoOtherChange should only be asked when planWrite already
	// knows the cores differ.
	return existingCore != contentCore
}

// fenceLineRe matches a fenced-code-block delimiter line (``` or ~~~,
// optionally indented up to 3 spaces per CommonMark, optional info string) —
// findOutsideFences tracks these line by line to skip matches that fall
// inside a fenced code block (e.g. README prose showing a fenced example of
// the very heading it documents).
var fenceLineRe = regexp.MustCompile(`(?m)^ {0,3}(` + "```" + `+|~~~+)`)

// findOutsideFences returns the first match of re in src whose start position
// is NOT inside a fenced code block, tracking ``` /~~~ fences line by line
// (open/close by run length and marker, per CommonMark) rather than assuming
// the whole document is prose — a README with a fenced example containing a
// `## Graph View`-shaped line must not have that heading recognized as the
// real (generated) section.
func findOutsideFences(src string, re *regexp.Regexp) []int {
	var fenceMarker string
	fenceOpen := false
	pos := 0
	for pos <= len(src) {
		lineEnd := strings.IndexByte(src[pos:], '\n')
		var line string
		if lineEnd < 0 {
			line = src[pos:]
		} else {
			line = src[pos : pos+lineEnd]
		}
		if m := fenceLineRe.FindStringSubmatch(line); m != nil {
			marker := m[1]
			switch {
			case !fenceOpen:
				fenceOpen = true
				fenceMarker = string(marker[0])
			case marker[0] == fenceMarker[0] && len(marker) >= len(fenceMarker):
				fenceOpen = false
				fenceMarker = ""
			}
		} else if !fenceOpen {
			if loc := re.FindStringIndex(line); loc != nil {
				return []int{pos + loc[0], pos + loc[1]}
			}
		}
		if lineEnd < 0 {
			break
		}
		pos += lineEnd + 1
	}
	return nil
}

// legacyGraphViewSpan locates an orphaned `## Graph View` section that
// carries no `graph-view:begin` marker (planReadmeUpdate's caller has
// already confirmed graphViewBegin is absent) — the shape a render from
// before the begin marker existed leaves behind: a `## Graph View` heading,
// optionally followed later by a `graph-view:end` marker. found is false
// when no such heading exists at all (a genuinely first render). When the
// heading is found but no end marker follows it, the span extends to the
// next depth<=2 heading (or EOF) exactly like nextH2HeadingRe's other
// callers — the heading's own natural section extent.
func legacyGraphViewSpan(src string) (start, end int, found bool) {
	loc := findOutsideFences(src, legacyGraphViewHeadingRe)
	if loc == nil {
		return 0, 0, false
	}
	start = loc[0]
	if endLoc := strings.Index(src[loc[1]:], graphViewEnd); endLoc >= 0 {
		return start, loc[1] + endLoc + len(graphViewEnd), true
	}
	if rest := nextH2HeadingRe.FindStringIndex(src[loc[1]:]); rest != nil {
		return start, loc[1] + rest[0], true
	}
	return start, len(src), true
}

// planWrite decides one view target's fate without writing: whether a write
// is needed, and the exact bytes to write. It carries BOTH refusal rules —
// an existing non-generated file (hand-authored or frozen v1 history), and
// an existing FROZEN VIEW whose content would change (DD-2: a projection of
// closed work is history; byte-identical re-renders stay no-ops). Shared by
// preflight (dry-run, before the graph write) and writeView, so a refusal
// can never fire after the graph moved.
func planWrite(path, content, plan string) (write bool, filled string, err error) {
	today := now().Format("2006-01-02")
	existing, readErr := os.ReadFile(path)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return false, "", readErr
		}
		return true, fillDates(content, today, today), nil
	}
	if !strings.Contains(string(existing), viewMarker(plan)) {
		return false, "", fmt.Errorf("compile: %s exists and is not a generated view; refusing to overwrite it (a hand-authored or frozen document is taken over by `sdd graph convert`, never clobbered by a render)", path)
	}
	created := today
	if m := createdLineRe.FindStringSubmatch(string(existing)); m != nil {
		created = m[1]
	}
	prevUpdated := today
	if m := updatedLineRe.FindStringSubmatch(string(existing)); m != nil {
		prevUpdated = m[1]
	}
	// Byte-stability check: same content under the existing stamps means no
	// write at all — and therefore no frozen-view refusal either, so
	// compile stays runnable on a completed plan.
	if fillDates(content, created, prevUpdated) == string(existing) {
		return false, "", nil
	}
	if strings.Contains(string(existing), frozenViewMarker) {
		// The frozen-view invariant is about REOPENING, not about byte
		// drift: refuse only when the projected history itself would
		// change — the graph's closure regressed (no longer frozen-shaped)
		// or a node's own projected content (contract, gate, observation,
		// closure, ...) differs. A renderer upgrade that only ADDS to the
		// derived `## Phase Completion Evidence` section (e.g. this
		// completion-evidence rendering itself) changes none of that: the
		// projection of history is unchanged, only the renderer's ability
		// to state it is. Comparing both renderings with their evidence
		// sections stripped isolates exactly that distinction, so compile
		// stays able to pick up a renderer fix on an already-completed
		// plan without ever silently rewriting what the projection says
		// happened.
		existingCore, existingEvidenceBody, existingHasEvidence := stripPhaseEvidenceSection(string(existing))
		contentCore, contentEvidenceBody, contentHasEvidence := stripPhaseEvidenceSection(content)
		filledContentCore := fillDates(contentCore, created, prevUpdated)
		refuse := !contentHasEvidence || !existingHasEvidence || filledContentCore != existingCore
		// The reopen-then-reclose escape (task 1): when the graph knows the
		// phase was legitimately reopened (a node's verification or
		// contract_rev moved) and has re-closed it, the ONLY difference in the
		// projected history is the per-node Observation line(s) — every other
		// projected field (Contract, Gate, Hazards, Justifies, ...) is
		// unchanged, and the new rendering is ITSELF frozen (every node closed
		// again). That is a re-close, not a genuine history change: re-render
		// instead of refusing.
		if refuse && contentHasEvidence && existingHasEvidence &&
			reclosedNoOtherChange(existingCore, filledContentCore, strings.Contains(content, frozenViewMarker)) {
			refuse = false
		}
		if !refuse {
			// The projection of history (everything before the evidence
			// section) is unchanged. A renderer upgrade may only ADD
			// evidence (placeholder "Pending — not complete." ->
			// populated body); once both the frozen view and the new
			// rendering already carry a populated (non-placeholder) body,
			// a differing body is a change like any other frozen change
			// and must be refused too (review-execution 56815db-b F-01
			// item 4).
			existingPending := strings.TrimSpace(existingEvidenceBody) == "Pending — not complete."
			filledContentBody := fillDates(contentEvidenceBody, created, prevUpdated)
			if !existingPending && filledContentBody != existingEvidenceBody {
				refuse = true
				// When the ONLY difference is the identity-recheck line
				// regressing from a real probe result to "no recheck ran",
				// the cause is a repository that no longer resolves at
				// render time — name that as the cause instead of the
				// generic reopened-phase message, while still refusing
				// (review-execution ef1962e F-01 item 7).
				if noRecheckRegression(existingEvidenceBody, filledContentBody) {
					return false, "", fmt.Errorf("compile: %s is a frozen view, and this render could not confirm the recorded revision still exists — no target repository resolved at render time, so its Identity recheck line would regress from a real probe result to \"no recheck ran\"; resolve the target repository (or delete the frozen view file explicitly and recompile if the phase was legitimately reopened) and try again", path)
				}
			}
		}
		if refuse {
			return false, "", fmt.Errorf("compile: %s is a frozen view — every node in it was closed when it was rendered, and the graph now disagrees with that frozen history; if the phase was legitimately reopened (a review finding demoted a node), delete the frozen view file explicitly and recompile", path)
		}
	}
	return true, fillDates(content, created, today), nil
}

// noRecheckRegression reports whether existingBody and newBody differ ONLY
// in their `- Identity recheck:` line, with the new line specifically
// regressing to "no recheck ran" (no resolved target repository at render
// time) from an existing line that reported a real probe outcome — the one
// frozen-view refusal shape whose cause is a missing repository resolution,
// not a genuine change to the projected history.
func noRecheckRegression(existingBody, newBody string) bool {
	newLoc := identityRecheckLineRe.FindStringIndex(newBody)
	if newLoc == nil || !strings.Contains(newBody[newLoc[0]:newLoc[1]], "no recheck ran") {
		return false
	}
	existingLoc := identityRecheckLineRe.FindStringIndex(existingBody)
	if existingLoc == nil || strings.Contains(existingBody[existingLoc[0]:existingLoc[1]], "no recheck ran") {
		return false
	}
	strippedExisting := existingBody[:existingLoc[0]] + existingBody[existingLoc[1]:]
	strippedNew := newBody[:newLoc[0]] + newBody[newLoc[1]:]
	return strippedExisting == strippedNew
}

// phaseEvidenceHeadingRe locates a phase doc's `## Phase Completion
// Evidence` section — always the view's last section (renderPhaseDoc emits
// nothing after it), so everything from the heading to EOF is exactly that
// section's extent. A node's own Contract text is rendered verbatim earlier
// in the document (the `## Nodes` section) and could itself contain a line
// that matches this heading shape; stripPhaseEvidenceSection anchors on the
// LAST match rather than the first for exactly that reason — the renderer's
// own section is always the final occurrence, a boundary the writer
// controls, never the content a node happens to carry (review-execution
// 56815db-b F-01 item 4).
var phaseEvidenceHeadingRe = regexp.MustCompile(`(?m)^## Phase Completion Evidence\s*$`)

// stripPhaseEvidenceSection returns a rendered phase doc with its `## Phase
// Completion Evidence` section (heading and body) removed, the section's
// own body (trimmed), and whether the heading was found. Used only to
// isolate frozen-view byte comparison from evidence-only content growth
// (planWrite) — never to change what is written.
func stripPhaseEvidenceSection(content string) (core, body string, found bool) {
	locs := phaseEvidenceHeadingRe.FindAllStringIndex(content, -1)
	if len(locs) == 0 {
		return content, "", false
	}
	loc := locs[len(locs)-1]
	core = strings.TrimRight(content[:loc[0]], "\n")
	body = strings.TrimSpace(content[loc[1]:])
	return core, body, true
}

// writeView writes one rendered view with date stability: an existing
// generated view keeps its created date, and keeps its bytes entirely when
// nothing but the updated stamp would change.
func writeView(path, content, plan string) (wrote bool, err error) {
	write, filled, err := planWrite(path, content, plan)
	if err != nil || !write {
		return false, err
	}
	return true, istore.WriteAtomic(path, filled)
}

// fillDates substitutes the renderer's date placeholders: the frontmatter
// `created:`/`updated:` stamps (count=1 each — exactly one frontmatter
// line), and every `{VERIFIED_DATE}` placeholder (count=all) WITHIN the `##
// Phase Completion Evidence` section only — never document-wide, because a
// node's own Contract text (rendered verbatim in `## Nodes`, earlier in the
// document) could legitimately contain that literal string and must survive
// unchanged (review-execution ef1962e F-01 item 3). Reusing the same
// `updated` value as the evidence date means a byte-identical re-render
// (nothing else changed) still stamps `updated` exactly once and stays
// stable — the evidence date does not independently drift across days the
// way a live time.Now() read at evidence-render time would (which would
// defeat frozen-view byte-stability: a frozen view could never be
// re-rendered as a no-op once a day passed).
func fillDates(content, created, updated string) string {
	content = strings.Replace(content, "created: {DATE}", "created: "+created, 1)
	content = strings.Replace(content, "updated: {DATE}", "updated: "+updated, 1)
	core, body, found := stripPhaseEvidenceSection(content)
	if !found {
		// No `## Phase Completion Evidence` heading in content: either a
		// bare evidence-body fragment a caller (planWrite's frozen-view
		// comparison) is filling in isolation — never the whole document,
		// so there is no Contract text this replace could clobber — or a
		// document that genuinely carries no evidence section at all.
		// Either way an unconditional replace is exactly right here.
		return strings.ReplaceAll(content, "{VERIFIED_DATE}", updated)
	}
	filledBody := strings.ReplaceAll(body, "{VERIFIED_DATE}", updated)
	return core + "\n\n## Phase Completion Evidence\n\n" + filledBody + "\n"
}
