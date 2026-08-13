package portable

import (
	"strings"
	"testing"
)

func TestFilterMarkers(t *testing.T) {
	in := strings.Join([]string{
		"keep",
		"<!-- claude-only -->",
		"claude stuff",
		"<!-- /claude-only -->",
		"also keep",
		"<!-- portable-only",
		"portable stuff",
		"-->",
		"tail",
	}, "\n")
	out, err := filterMarkers(in, "t.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "keep\nalso keep\nportable stuff\ntail"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestFilterMarkersErrors(t *testing.T) {
	for _, in := range []string{
		"<!-- claude-only -->\nnever closed",
		"<!-- /claude-only -->",
		"<!-- claude-only -->\n<!-- portable-only\n-->\n<!-- /claude-only -->",
	} {
		if _, err := filterMarkers(in, "t.md"); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestRewriteTerms(t *testing.T) {
	cases := map[string]string{
		"`/sdd-planner:plan`":            "`sdd-plan`",
		"see commands/plan/SKILL.md §3":  "see skills/sdd-plan/SKILL.md §3",
		"run `/decide check` weekly":     "run `sdd-decide check` weekly",
		"the skills/decision-log skill":  "the skills/sdd-decision-log skill",
		"read skills/go-specifications/": "read shared/language-specs/go.md",
		"a plan for the design":          "a plan for the design", // bare words untouched
	}
	for in, want := range cases {
		if got := rewriteTerms(in); got != want {
			t.Errorf("rewriteTerms(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSwapSection(t *testing.T) {
	in := "# T\n\n## Path Resolution\nold body\nmore old\n\n## Next\nkeep"
	out := swapSection(in, "## Path Resolution", "REPLACED")
	want := "# T\n\nREPLACED\n\n## Next\nkeep"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
	// Section at end of file.
	in2 := "# T\n\n## Path Resolution\nold tail"
	if got := swapSection(in2, "## Path Resolution", "R"); got != "# T\n\nR\n\n" {
		t.Fatalf("tail swap got %q", got)
	}
	// Absent heading: unchanged.
	if got := swapSection("# T\nbody", "## Path Resolution", "R"); got != "# T\nbody" {
		t.Fatalf("absent heading changed content: %q", got)
	}
}

func TestPortableDescription(t *testing.T) {
	in := "Create a plan. Triggers: /plan, create a plan, plan this"
	want := "Create a plan. create a plan, plan this"
	if got := portableDescription(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// No Triggers clause: unchanged.
	if got := portableDescription("Just a description."); got != "Just a description." {
		t.Fatalf("got %q", got)
	}
}

func TestTransformSkill(t *testing.T) {
	in := "---\nname: plan\ndescription: \"Make plans. Triggers: /plan, plan this\"\n---\n\n# /plan — Make Plans\n\n## Path Resolution\nglob stuff\n\n## Process\nInvoke the `sdd-planner:researcher` agent and go.\n"
	out, err := transformSkill(in, "plan", "commands/plan/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"name: sdd-plan\n",
		"description: \"Make plans. plan this\"\n",
		"# Make Plans\n",
		"## Resources\n",
		"shared/agent-prompts/researcher.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	for _, bad := range []string{"## Path Resolution", "sdd-planner:", "# /plan"} {
		if strings.Contains(out, bad) {
			t.Errorf("output still contains %q", bad)
		}
	}
}

func TestTransformLangSpec(t *testing.T) {
	in := "---\nname: go-specifications\ndescription: \"Structural verification for Go — vet. Load when reviewing Go code (.go; go.mod).\"\ndisable-model-invocation: true\n---\n\n# Go — Structural Verification\n\nBody here.\n"
	out, err := transformLangSpec(in, "skills/go-specifications/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# Go — Structural Verification\n\nApply when planning, implementing, or reviewing code detected by `.go`; `go.mod`. Dispatched via `shared/language-verification.md`.\n") {
		t.Fatalf("apply-line wrong:\n%s", out)
	}
	if strings.Contains(out, "---") || strings.Contains(out, "disable-model-invocation") {
		t.Fatalf("frontmatter leaked:\n%s", out)
	}
	// Mixed file-shaped and plain tokens.
	in2 := strings.Replace(in, "(.go; go.mod)", "(.c, .h; CMake, Makefile)", 1)
	out2, err := transformLangSpec(in2, "t.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "detected by `.c`, `.h`; CMake, Makefile.") {
		t.Fatalf("token backticking wrong:\n%s", out2)
	}
}

func TestTransformPrompt(t *testing.T) {
	spec := promptSpec{
		agent: "x", out: "shared/review-prompts/x.md", title: "X Review",
		inputs: []string{"- Repo: `{{TARGET_REPO}}`"},
		note:   "Report missing inputs first.",
	}
	// Agent WITH its own Inputs section: placeholder list replaces the
	// "You are invoked with" block, other prose survives.
	in := "---\nname: x\n---\n\n# X Agent\n\nIntro.\n\n## Path Resolution\nglob stuff\n\n## Inputs\n\nYou are invoked with:\n- **Target repo path**\n\nYou are **not** given plans.\n\n## Process\nGo.\n"
	out, err := transformPrompt(in, spec, "agents/x.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# X Review\n", "- Repo: `{{TARGET_REPO}}`", "Report missing inputs first.",
		"You are **not** given plans.", "## Process",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, bad := range []string{"You are invoked with", "## Path Resolution", "# X Agent", "---"} {
		if strings.Contains(out, bad) {
			t.Errorf("output still contains %q:\n%s", bad, out)
		}
	}
	if strings.Count(out, "## Inputs") != 1 {
		t.Errorf("want exactly one Inputs section:\n%s", out)
	}
	// Agent WITHOUT an Inputs section: one is inserted after the intro.
	in2 := "---\nname: x\n---\n\n# X Agent\n\nIntro.\n\n## Process\nGo.\n"
	out2, err := transformPrompt(in2, spec, "agents/x.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "Intro.\n\n## Inputs\n\n- Repo: `{{TARGET_REPO}}`") {
		t.Errorf("inserted Inputs misplaced:\n%s", out2)
	}
}
