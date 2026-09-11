package portable

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForkWorkflowIntentIsolation(t *testing.T) {
	root := repoRoot(t)
	generated, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}

	isolated := map[string]string{
		"agents/quality-scanner.md":               "shared/review-prompts/quality.md",
		"agents/blind-spot-finder.md":             "shared/review-prompts/blind-spots.md",
		"shared/templates/quality-scan-prompt.md": "shared/templates/quality-scan-prompt.md",
	}
	for canonical, portablePath := range isolated {
		assertNoForkIntent(t, canonical, mustWorkflowBytes(t, root, canonical))
		assertNoForkIntent(t, portablePath, generated.Files[portablePath])
	}

	intentAware := map[string]string{
		"agents/researcher.md":    "shared/agent-prompts/researcher.md",
		"agents/plan-reviewer.md": "shared/agent-prompts/plan-reviewer.md",
		"agents/spec-reviewer.md": "shared/agent-prompts/spec-reviewer.md",
	}
	for canonical, portablePath := range intentAware {
		assertHasEffectiveContext(t, canonical, mustWorkflowBytes(t, root, canonical))
		assertHasEffectiveContext(t, portablePath, generated.Files[portablePath])
	}

	for _, pair := range [][2]string{
		{"commands/decide/SKILL.md", "skills/sdd-decide/SKILL.md"},
		{"skills/decision-log/SKILL.md", "skills/sdd-decision-log/SKILL.md"},
		{"commands/setup/SKILL.md", "skills/sdd-setup/SKILL.md"},
	} {
		canonical := mustWorkflowBytes(t, root, pair[0])
		portable := generated.Files[pair[1]]
		for _, term := range []string{"sdd decide capabilities --json", "decision_forks"} {
			if !containsFold(canonical, term) || !containsFold(portable, term) {
				t.Errorf("canonical/portable workflow pair %s -> %s does not agree on %q", pair[0], pair[1], term)
			}
		}
	}

	if !containsPath(generated.Generated, "skills/sdd-decide/SKILL.md") ||
		!containsPath(generated.Generated, "shared/agent-prompts/researcher.md") {
		t.Error("decide and researcher portable guidance must be generated from canonical sources")
	}
	if !containsPath(generated.Variants, "skills/sdd-setup/SKILL.md") {
		t.Error("setup portable guidance must come from its declared portable variant")
	}

	for _, rel := range []string{
		"skills/sdd-decide/SKILL.md", "skills/sdd-decision-log/SKILL.md", "skills/sdd-setup/SKILL.md",
		"shared/agent-prompts/researcher.md", "shared/agent-prompts/plan-reviewer.md",
		"shared/agent-prompts/spec-reviewer.md", "shared/review-prompts/quality.md",
		"shared/review-prompts/blind-spots.md", "shared/templates/quality-scan-prompt.md",
	} {
		want := generated.Files[rel]
		if want == nil {
			t.Fatalf("generated portable tree is missing %s", rel)
		}
		for _, outDir := range OutDirs {
			got, readErr := os.ReadFile(filepath.Join(root, outDir, filepath.FromSlash(rel)))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s/%s is not the output of portable sync", outDir, rel)
			}
		}
	}
}

func assertNoForkIntent(t *testing.T, name string, content []byte) {
	t.Helper()
	for _, forbidden := range []string{"sdd decide effective", "sdd decide history", "decision_forks", "fork authority"} {
		if containsFold(content, forbidden) {
			t.Errorf("intent-isolated %s received fork/ledger context %q", name, forbidden)
		}
	}
}

func assertHasEffectiveContext(t *testing.T, name string, content []byte) {
	t.Helper()
	for _, required := range []string{"decisionlog", "sdd decide capabilities --json", "decision_forks", "sdd decide effective --json", "sdd decide list --status accepted --json", "diagnostic"} {
		if !containsFold(content, required) {
			t.Errorf("intent-aware %s does not consult effective fork context %q", name, required)
		}
	}
}

func mustWorkflowBytes(t *testing.T, root, rel string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func containsFold(content []byte, term string) bool {
	return strings.Contains(strings.ToLower(string(content)), strings.ToLower(term))
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
