// Package portable generates the portable (OpenCode/Codex) plugin tree from
// the canonical Claude tree at the repository root.
//
// The repository root is the Claude plugin and the single source of truth:
// commands/, agents/, skills/, and shared/ are hand-edited there. The
// portable/ tree is a generated artifact — the same content re-expressed in
// the layout the other harnesses consume (skills/sdd-<name>/, prompt files
// instead of agent definitions, shared/ docs with harness-neutral wording).
// Never edit portable/ by hand; run `sdd plugin sync` after editing the
// canonical tree, and `sdd plugin check` (wired into `make test`) fails when
// the two drift apart.
package portable

import (
	"fmt"
	"regexp"
	"strings"
)

// skillNames are the canonical lifecycle skill directory names under
// commands/. They double as the vocabulary for term rewriting: a reference to
// `/plan` or `/sdd-planner:plan` in canonical prose becomes `sdd-plan` in the
// portable tree, where skills are invoked by description matching rather than
// slash command.
var skillNames = []string{
	"brainstorm", "code-review", "debrief", "decide", "design", "implement",
	"plan", "poke-holes", "research", "setup", "specify", "validate",
	// Model-only reference skill that ships in both trees.
	"decision-log",
}

var (
	namesAlt = func() string {
		quoted := make([]string, len(skillNames))
		for i, n := range skillNames {
			quoted[i] = regexp.QuoteMeta(n)
		}
		return strings.Join(quoted, "|")
	}()

	// `/sdd-planner:plan` (with or without backticks) → `sdd-plan`.
	namespacedRe = regexp.MustCompile(`/sdd-planner:(` + namesAlt + `)\b`)

	// commands/plan/SKILL.md → skills/sdd-plan/SKILL.md; also bare
	// commands/plan/ directory references.
	commandsPathRe = regexp.MustCompile(`commands/(` + namesAlt + `)/`)

	// Backticked slash invocations: `/plan`, `/decide check` →
	// `sdd-plan`, `sdd-decide check`. Only inside backticks — a bare /plan in
	// prose is too ambiguous to rewrite mechanically (use a marker there).
	backtickSlashRe = regexp.MustCompile("`/(" + namesAlt + ")((?: [a-z][a-z-]*)*)`")

	// skills/decision-log → skills/sdd-decision-log (the one model-only skill
	// that keeps skill form in the portable tree).
	decisionLogPathRe = regexp.MustCompile(`skills/decision-log\b`)

	// skills/<lang>-specifications → shared/language-specs/<lang>.md: the
	// language reference skills flatten to plain shared docs in the portable
	// tree, since only Claude auto-loads description-matched skills.
	langSkillPathRe = regexp.MustCompile(`skills/(cpp|rust|go|python|typescript|java|swift)-specifications\S*`)
)

// Marker grammar, chosen so canonical files remain valid Claude content:
//
//	<!-- claude-only -->        dropped from portable output (markers and body)
//	...claude-specific prose...
//	<!-- /claude-only -->
//
//	<!-- portable-only          body is inside an HTML comment, so Claude
//	...portable prose...        ignores it; the generator uncomments it for
//	-->                         the portable tree.
//
// Markers must sit alone on their line. Nesting is not supported and is
// reported as an error rather than silently mangled.
const (
	claudeOpen   = "<!-- claude-only -->"
	claudeClose  = "<!-- /claude-only -->"
	portableOpen = "<!-- portable-only"
	portableEnd  = "-->"
)

// filterMarkers renders content for the portable tree: claude-only blocks are
// removed, portable-only blocks are uncommented.
func filterMarkers(content, path string) (string, error) {
	lines := strings.Split(content, "\n")
	var out []string
	const (
		stateText = iota
		stateClaude
		statePortable
	)
	state := stateText
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch state {
		case stateText:
			switch trimmed {
			case claudeOpen:
				state = stateClaude
			case portableOpen:
				state = statePortable
			case claudeClose:
				return "", fmt.Errorf("%s:%d: %s without opening marker", path, i+1, claudeClose)
			default:
				out = append(out, line)
			}
		case stateClaude:
			switch trimmed {
			case claudeClose:
				state = stateText
			case claudeOpen, portableOpen:
				return "", fmt.Errorf("%s:%d: nested marker inside claude-only block", path, i+1)
			}
			// body dropped
		case statePortable:
			switch trimmed {
			case portableEnd:
				state = stateText
			case claudeOpen, portableOpen:
				return "", fmt.Errorf("%s:%d: nested marker inside portable-only block", path, i+1)
			default:
				out = append(out, line)
			}
		}
	}
	if state != stateText {
		return "", fmt.Errorf("%s: unterminated harness marker block", path)
	}
	return strings.Join(out, "\n"), nil
}

// rewriteTerms translates Claude-tree vocabulary into portable-tree
// vocabulary. Order matters: namespaced and path forms are rewritten before
// the bare backticked slash form so each occurrence is rewritten exactly once.
func rewriteTerms(content string) string {
	content = namespacedRe.ReplaceAllString(content, "sdd-$1")
	content = commandsPathRe.ReplaceAllString(content, "skills/sdd-$1/")
	content = backtickSlashRe.ReplaceAllString(content, "`sdd-$1$2`")
	content = decisionLogPathRe.ReplaceAllString(content, "skills/sdd-decision-log")
	content = langSkillPathRe.ReplaceAllString(content, "shared/language-specs/$1.md")
	return content
}

// agentRewrites maps Claude subagent invocations to the portable
// collaboration idiom. In the portable tree there are no plugin-defined agent
// types: a skill renders the corresponding prompt file and hands it to
// whatever delegation mechanism the runtime provides (or performs the work
// itself). See shared/agent-runtime.md.
// Ordered: whole-phrase rules first (they keep the surrounding grammar
// intact), bare-token fallbacks last. A rewrite here must read as a sentence
// in every context it can appear in — check `grep -o` over commands/ when
// adding one.
var agentRewrites = []struct{ from, to string }{
	// Phrase forms.
	{"(delegated to `sdd-planner:researcher`)",
		"(delegated to a collaboration subagent when available)"},
	{"Invoke the `sdd-planner:researcher` agent",
		"Dispatch a collaboration subagent rendering `shared/agent-prompts/researcher.md` (or perform that prompt yourself if collaboration is unavailable)"},
	{"Invoke `sdd-planner:researcher` with",
		"Dispatch a collaboration subagent rendering `shared/agent-prompts/researcher.md` (or perform that prompt yourself) with"},
	{"Invoke the `sdd-planner:plan-reviewer` agent",
		"Dispatch a collaboration subagent in a fresh non-inheriting context rendering `shared/agent-prompts/plan-reviewer.md` (if collaboration is unavailable, perform the review yourself following that prompt and label it **self-review**)"},
	{"Invoke the `sdd-planner:spec-reviewer` agent",
		"Dispatch a collaboration subagent in a fresh non-inheriting context rendering `shared/agent-prompts/spec-reviewer.md` (if collaboration is unavailable, perform the review yourself following that prompt and label it **self-review**)"},
	{"Dispatch `sdd-planner:code-implementer` agents",
		"Dispatch implementation subagents (stable dispatch identifier `implement_task`)"},
	{"launch a `sdd-planner:code-implementer` agent",
		"launch an implementation subagent (stable dispatch identifier `implement_task`)"},
	{"resume the `sdd-planner:code-implementer` agent",
		"resume the implementation subagent"},
	// Bare-token fallbacks.
	{"`sdd-planner:researcher`", "the researcher prompt (`shared/agent-prompts/researcher.md`)"},
	{"`sdd-planner:plan-reviewer`", "the plan-review prompt (`shared/agent-prompts/plan-reviewer.md`)"},
	{"`sdd-planner:spec-reviewer`", "the spec-review prompt (`shared/agent-prompts/spec-reviewer.md`)"},
	{"`sdd-planner:quality-scanner`", "the quality-scan prompt (`shared/templates/quality-scan-prompt.md`)"},
	{"`sdd-planner:drift-detector`", "the `review_plan_drift` lane (`shared/review-prompts/plan-drift.md`)"},
	{"`sdd-planner:spec-compliance`", "the `review_spec_compliance` lane (`shared/review-prompts/spec-compliance.md`)"},
	{"`sdd-planner:blind-spot-finder`", "the `review_blind_spots` lane (`shared/review-prompts/blind-spots.md`)"},
	{"`sdd-planner:code-implementer`", "an implementation subagent (dispatch identifier `implement_task`)"},
	{"the Task tool", "the runtime's delegation mechanism"},
}

// resourcesSection is the stock portable replacement for the Claude-specific
// "## Path Resolution" section every lifecycle skill carries. The Claude form
// (glob ~/.claude/plugins/cache, sort -V) is meaningless outside Claude Code;
// the portable form defers to shared/agent-runtime.md, the portable tree's
// resolution spec.
const resourcesSection = `## Resources

Before opening ` + "`shared/...`" + `, follow symlinks in this loaded file's path, then derive ` + "`<plugin-root>`" + ` from ` + "`<plugin-root>/skills/<name>/SKILL.md`" + `; fallback search roots are repository/user ` + "`.agents/`" + ` (including ` + "`$HOME/.agents/plugins/*/`" + `), Codex ` + "`${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`" + `, and runtime-configured skill roots. Accept only a root containing this skill, ` + "`shared/agent-runtime.md`" + `, and the matching plugin manifest; never use the working directory. Then read ` + "`<plugin-root>/shared/agent-runtime.md`" + ` and ` + "`<plugin-root>/shared/path-resolution.md`" + `, and resolve every ` + "`shared/<path>`" + ` reference in this skill against ` + "`<plugin-root>`" + `.

**Resource boundary:** Read the plugin, all ` + "`SKILL.md`" + ` files, and ` + "`shared/`" + ` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.`

// "# /brainstorm — Explore Possibilities" → "# Explore Possibilities":
// slash-command titles have no meaning where there are no slash commands.
var slashTitleRe = regexp.MustCompile(`(?m)^# /[a-z-]+ — `)

// swapSection replaces the named section (heading line through the character
// before the next same-or-higher-level heading, or end of file) with
// replacement. Returns content unchanged when the heading is absent.
func swapSection(content, heading, replacement string) string {
	start := -1
	if strings.HasPrefix(content, heading+"\n") {
		start = 0
	} else if i := strings.Index(content, "\n"+heading+"\n"); i >= 0 {
		start = i + 1
	}
	if start < 0 {
		return content
	}
	rest := content[start+len(heading):]
	end := len(content)
	if i := strings.Index(rest, "\n#"); i >= 0 {
		end = start + len(heading) + i + 1
	}
	return content[:start] + replacement + "\n\n" + content[end:]
}

// rewriteDispatch applies the collaboration-idiom rewrites and the stock
// section swap.
func rewriteDispatch(content string) string {
	content = swapSection(content, "## Path Resolution", resourcesSection)
	content = slashTitleRe.ReplaceAllString(content, "# ")
	for _, r := range agentRewrites {
		content = strings.ReplaceAll(content, r.from, r.to)
	}
	return content
}

// transformDoc is the standard portable rendering for a markdown document:
// marker filtering, dispatch/section rewriting, then term rewriting.
func transformDoc(content, path string) (string, error) {
	filtered, err := filterMarkers(content, path)
	if err != nil {
		return "", err
	}
	return rewriteTerms(rewriteDispatch(filtered)), nil
}

// splitFrontmatter splits a document into its YAML frontmatter (without the
// --- fences) and body. Documents without frontmatter return an empty first
// value.
func splitFrontmatter(content string) (fm, body string) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return "", content
	}
	return rest[:end], rest[end+len("\n---\n"):]
}

var (
	triggersRe = regexp.MustCompile(`Triggers:\s*(.*)$`)
	descLineRe = regexp.MustCompile(`(?m)^description:\s*"?(.*?)"?\s*$`)
)

// portableDescription reshapes a skill description for the portable tree:
// the "Triggers:" label and its slash-command entries are dropped (there are
// no slash commands to type), while the natural-language trigger phrases are
// kept — they are what description-matched skill selection keys on.
func portableDescription(desc string) string {
	if m := triggersRe.FindStringSubmatchIndex(desc); m != nil {
		tail := desc[m[2]:m[3]]
		var kept []string
		for _, tok := range strings.Split(tail, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" || strings.HasPrefix(tok, "/") {
				continue
			}
			kept = append(kept, tok)
		}
		desc = strings.TrimSpace(desc[:m[0]]) + " " + strings.Join(kept, ", ")
	}
	desc = strings.TrimSpace(desc)
	desc = rewriteTerms(desc)
	return desc
}

// transformSkill renders a canonical commands/<name>/SKILL.md (or model-only
// skills/<name>/SKILL.md) into portable skills/sdd-<name>/SKILL.md form:
// renamed, description cleaned, body marker-filtered and term-rewritten.
// Frontmatter keys other than name/description are preserved verbatim.
func transformSkill(content, name, path string) (string, error) {
	fm, body := splitFrontmatter(content)
	if fm == "" {
		return "", fmt.Errorf("%s: skill has no frontmatter", path)
	}
	desc := ""
	if m := descLineRe.FindStringSubmatch(fm); m != nil {
		desc = portableDescription(m[1])
	} else {
		return "", fmt.Errorf("%s: skill frontmatter has no description", path)
	}
	var extra []string
	for _, line := range strings.Split(fm, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "name:") || strings.HasPrefix(t, "description:") {
			continue
		}
		extra = append(extra, line)
	}
	outBody, err := transformDoc(body, path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: sdd-%s\n", name)
	fmt.Fprintf(&b, "description: %q\n", desc)
	for _, line := range extra {
		b.WriteString(line + "\n")
	}
	b.WriteString("---\n")
	b.WriteString(outBody)
	return b.String(), nil
}

var parenRe = regexp.MustCompile(`\(([^)]+)\)\.?"?\s*$`)

// transformLangSpec flattens a skills/<lang>-specifications/SKILL.md wrapper
// into the plain shared/language-specs/<lang>.md document the portable tree
// carries: frontmatter dropped, with an "Apply when" line derived from the
// wrapper's description inserted after the title so the loading condition
// survives the loss of description-matched auto-loading.
func transformLangSpec(content, path string) (string, error) {
	fm, body := splitFrontmatter(content)
	if fm == "" {
		return "", fmt.Errorf("%s: language skill has no frontmatter", path)
	}
	m := descLineRe.FindStringSubmatch(fm)
	if m == nil {
		return "", fmt.Errorf("%s: language skill frontmatter has no description", path)
	}
	pm := parenRe.FindStringSubmatch(m[1])
	if pm == nil {
		return "", fmt.Errorf("%s: language skill description has no (ext; ext) parenthetical", path)
	}
	// Backtick the file-shaped tokens (anything with a dot: extensions,
	// go.mod); leave plain words (CMake, Makefile) as prose.
	var exts []string
	for _, group := range strings.Split(pm[1], ";") {
		var toks []string
		for _, tok := range strings.Split(group, ",") {
			tok = strings.TrimSpace(tok)
			if strings.Contains(tok, ".") {
				tok = "`" + tok + "`"
			}
			toks = append(toks, tok)
		}
		exts = append(exts, strings.Join(toks, ", "))
	}
	applyLine := fmt.Sprintf(
		"Apply when planning, implementing, or reviewing code detected by %s. Dispatched via `shared/language-verification.md`.",
		strings.Join(exts, "; "))

	outBody, err := transformDoc(body, path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimLeft(outBody, "\n"), "\n")
	var b strings.Builder
	inserted := false
	for _, line := range lines {
		b.WriteString(line + "\n")
		if !inserted && strings.HasPrefix(line, "# ") {
			b.WriteString("\n" + applyLine + "\n")
			inserted = true
		}
	}
	if !inserted {
		return "", fmt.Errorf("%s: language skill body has no H1 title", path)
	}
	// Normalize: exactly one trailing newline.
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}
