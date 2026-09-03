package rules

import (
	"os"
	"path/filepath"
	"testing"
)

// D-0024: after a frozen review endpoint the whole planning root is lifecycle
// — specs, designs, the ledger, other phase docs and new artifacts there ride
// in the phase-close commit — while source under the target root stays
// material.
func TestPhaseLifecyclePathsCoverPlanningRoot(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"src/main.go":                   "package main\n",
		".plans/Plans/Sample/README.md": validPlan(false),
	}
	for rel, content := range phaseGateFiles(true, true) {
		files[".plans/"+rel] = content
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := LoadRoot(filepath.Join(dir, ".plans"))
	if err != nil {
		t.Fatal(err)
	}
	var phase, review *Artifact
	for _, a := range root.Artifacts {
		switch a.Rel {
		case "Plans/Sample/01-One.md":
			phase = a
		case "Retro/phase-review.md":
			review = a
		}
	}
	if phase == nil || review == nil {
		t.Fatalf("fixture did not load phase and review: %v", root.Artifacts)
	}
	allowed := phaseLifecyclePaths(root, phase, review, dir)
	lifecycle := func(p string) bool { return allowed[p] || underLifecycleDir(allowed, p) }
	for _, p := range []string{
		".plans/Plans/Sample/01-One.md",
		".plans/Plans/Sample/README.md",
		".plans/Retro/phase-review.md",
		".plans/Specs/Other/README.md",  // a new spec written during the phase
		".plans/Decisions/decisions.md", // a ledger entry
		".plans/Plans/Sample/02-Two.md", // a follow-up note in another phase doc
		".plans/Plans/Sample/Sample-Graph.json",
		".plans/planning-config.json",
	} {
		if !lifecycle(p) {
			t.Errorf("%s should be lifecycle after the frozen endpoint (D-0024)", p)
		}
	}
	for _, p := range []string{"src/main.go", ".plans-old/Specs/x.md", "plans/README.md"} {
		if lifecycle(p) {
			t.Errorf("%s must stay material", p)
		}
	}
}
