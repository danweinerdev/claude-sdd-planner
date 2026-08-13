package portable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The portable tree has no plugin-defined subagent types, so each Claude
// agent definition (agents/<name>.md) is re-expressed as a rendered role
// prompt: the skill substitutes the {{PLACEHOLDER}} inputs and hands the
// prompt to whatever delegation mechanism the runtime provides. Deriving the
// prompts here — instead of hand-maintaining a parallel set — means an agent
// edit propagates to both harnesses in one place; the derivation is:
// frontmatter dropped, Path Resolution section dropped (the dispatcher passes
// resolved paths in), an Inputs block inserted after the intro paragraph, and
// the standard portable transforms applied.
type promptSpec struct {
	agent  string // agents/<agent>.md
	out    string // path in the portable tree
	title  string // H1 for the prompt (replaces the agent's H1)
	inputs []string
	note   string // sentence after the inputs list, "" for none
}

const missingInputNote = "If an input is missing or names a path that does not exist, report the mismatch as your first finding — do not improvise around it."

var promptSpecs = []promptSpec{
	{
		agent: "researcher", out: "shared/agent-prompts/researcher.md", title: "Researcher",
		inputs: []string{
			"- Topic / feature under consideration: `{{TOPIC}}`",
			"- Dispatching skill: `{{CALLER}}`",
			"- Planning root (artifacts): `{{PLANNING_ROOT}}`",
			"- Target repository: `{{TARGET_REPO}}`",
			"- Specific questions from the dispatcher: `{{QUESTIONS}}`",
		},
		note: missingInputNote,
	},
	{
		agent: "plan-reviewer", out: "shared/agent-prompts/plan-reviewer.md", title: "Plan / Design Reviewer",
		inputs: []string{
			"- Document under review (a plan README plus its phase docs, or a design README): `{{DOC_PATH}}`",
			"- Planning root (artifacts): `{{PLANNING_ROOT}}`",
			"- Target repository: `{{TARGET_REPO}}`",
		},
		note: "If the document path is missing or does not exist, report that as your finding — do not guess at a document.",
	},
	{
		agent: "spec-reviewer", out: "shared/agent-prompts/spec-reviewer.md", title: "Specification Reviewer",
		inputs: []string{
			"- Specification under review: `{{SPEC_PATH}}`",
			"- Planning root (artifacts): `{{PLANNING_ROOT}}`",
			"- Target repository: `{{TARGET_REPO}}`",
		},
		note: "If the spec path is missing or does not exist, report that as your finding — do not guess at a document.",
	},
	{
		agent: "drift-detector", out: "shared/review-prompts/plan-drift.md", title: "Plan Drift Review",
		inputs: []string{
			"- Target repository: `{{TARGET_REPO}}`",
			"- VCS: `{{VCS}}`",
			"- Frozen diff command: `{{DIFF_COMMAND}}`",
			"- Plan: `{{PLAN_PATH}}`",
			"- Phase: `{{PHASE_PATH}}`",
			"- Prior debriefs: `{{DEBRIEF_PATHS}}`",
			"- Structural-verification note: `{{LANGUAGE_NOTE}}`",
		},
		note: missingInputNote,
	},
	{
		agent: "quality-scanner", out: "shared/review-prompts/quality.md", title: "Quality Review",
		inputs: []string{
			"- Target repository: `{{TARGET_REPO}}`",
			"- VCS: `{{VCS}}`",
			"- Frozen diff command: `{{DIFF_COMMAND}}`",
			"- Structural-verification note: `{{LANGUAGE_NOTE}}`",
		},
		note: missingInputNote,
	},
	{
		agent: "spec-compliance", out: "shared/review-prompts/spec-compliance.md", title: "Spec Compliance Review",
		inputs: []string{
			"- Target repository: `{{TARGET_REPO}}`",
			"- VCS: `{{VCS}}`",
			"- Frozen diff command: `{{DIFF_COMMAND}}`",
			"- Specifications: `{{SPEC_PATHS}}`",
			"- Designs: `{{DESIGN_PATHS}}`",
		},
		note: missingInputNote,
	},
	{
		agent: "blind-spot-finder", out: "shared/review-prompts/blind-spots.md", title: "Blind Spot Review",
		inputs: []string{
			"- Target repository: `{{TARGET_REPO}}`",
			"- VCS: `{{VCS}}`",
			"- Frozen diff command: `{{DIFF_COMMAND}}`",
		},
		note: "",
	},
	// code-implementer is deliberately absent: implementation dispatches use
	// the stable identifier `implement_task` with the task inline (D-0009) —
	// there is no rendered role prompt in the portable tree.
}

var h1Re = regexp.MustCompile(`(?m)^# .*$`)

// transformPrompt derives one portable role prompt from an agent definition.
func transformPrompt(content string, spec promptSpec, path string) (string, error) {
	_, body := splitFrontmatter(content)
	if body == content {
		return "", fmt.Errorf("%s: agent has no frontmatter", path)
	}

	// Retitle and drop the Claude-specific resolution section — the
	// dispatcher resolves paths and passes them via Inputs.
	if !h1Re.MatchString(body) {
		return "", fmt.Errorf("%s: agent body has no H1 title", path)
	}
	replacedTitle := false
	body = h1Re.ReplaceAllStringFunc(body, func(m string) string {
		if replacedTitle {
			return m
		}
		replacedTitle = true
		return "# " + spec.title
	})
	body = swapSection(body, "## Path Resolution", "")

	// Build the Inputs section. When the agent already has one, its
	// "You are invoked with:" list is replaced by the {{PLACEHOLDER}} list
	// but the rest of the section (lane-critical prose like "you are NOT
	// given plans — ignore them if passed") survives. Otherwise the section
	// is inserted after the intro paragraph.
	var inputs strings.Builder
	inputs.WriteString("## Inputs\n\n")
	inputs.WriteString(strings.Join(spec.inputs, "\n"))
	inputs.WriteString("\n")
	if spec.note != "" {
		inputs.WriteString("\n" + spec.note + "\n")
	}
	if strings.Contains(body, "\n## Inputs\n") {
		tail := sectionBody(body, "## Inputs")
		var kept []string
		for _, block := range strings.Split(strings.TrimSpace(tail), "\n\n") {
			if strings.HasPrefix(block, "You are invoked with") {
				continue
			}
			kept = append(kept, block)
		}
		merged := strings.TrimRight(inputs.String(), "\n")
		if len(kept) > 0 {
			merged += "\n\n" + strings.Join(kept, "\n\n")
		}
		body = swapSection(body, "## Inputs", merged)
	} else {
		idx := strings.Index(body, "\n## ")
		if idx < 0 {
			return "", fmt.Errorf("%s: agent body has no sections", path)
		}
		body = body[:idx+1] + "\n" + inputs.String() + "\n" + body[idx+1:]
	}

	out, err := transformDoc(body, path)
	if err != nil {
		return "", err
	}
	// Tidy blank-line runs left by section removal.
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimLeft(out, "\n"), nil
}

// generatePrompts derives every portable role prompt from agents/.
func generatePrompts(repoRoot string, r *Result) error {
	for _, spec := range promptSpecs {
		src := filepath.Join(repoRoot, "agents", spec.agent+".md")
		raw, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		out, err := transformPrompt(string(raw), spec, "agents/"+spec.agent+".md")
		if err != nil {
			return err
		}
		r.Files[spec.out] = []byte(out)
		r.Generated = append(r.Generated, spec.out)
	}
	return nil
}
