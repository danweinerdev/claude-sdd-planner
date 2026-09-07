package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

func historyGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	argv := append([]string{"-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid", "-c", "core.hooksPath=" + filepath.Join(dir, "no-hooks")}, args...)
	cmd := exec.Command("git", argv...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func retirementFixture(t *testing.T) (string, string, model.RetirementSource) {
	t.Helper()
	repo := t.TempDir()
	root := filepath.Join(repo, ".plans")
	dir := filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := ".plans/Plans/Demo/01 Old.md"
	text := "---\ntitle: Old\ntype: phase\nplan: Demo\nphase: 1\ntasks:\n  - id: '1.1'\n    title: First\n  - id: '1.2'\n    title: Second\n---\n# Old\nThis mentions 9.9 but does not declare it.\n"
	if err := os.WriteFile(filepath.Join(repo, rel), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	historyGit(t, repo, "init", "-q")
	historyGit(t, repo, "add", rel)
	historyGit(t, repo, "commit", "-qm", "historical fixture")
	revision := historyGit(t, repo, "rev-parse", "HEAD")
	if err := os.Remove(filepath.Join(repo, rel)); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: 1, SeqCounter: 4, Nodes: []model.Node{{ID: "replacement", Contract: "new behavior", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1}}}
	if err := gstore.Save(gstore.PathFor(dir), g); err != nil {
		t.Fatal(err)
	}
	return root, dir, model.RetirementSource{VCS: "git", Revision: revision, Path: rel, SourceID: "1.1"}
}

func TestRetirementPinsHistoricalIDAfterFileDeletion(t *testing.T) {
	root, dir, source := retirementFixture(t)
	before, _ := os.ReadFile(gstore.PathFor(dir))
	symbolic := source
	symbolic.Revision = "HEAD"
	record, err := RetireWithSource(root, "Demo", "task-1-1", symbolic, []string{"replacement"}, true)
	if err != nil || record.Source.Revision != source.Revision {
		t.Fatalf("pin: %+v %v", record, err)
	}
	afterDry, _ := os.ReadFile(gstore.PathFor(dir))
	if string(before) != string(afterDry) {
		t.Fatal("dry run changed graph")
	}
	if _, err := RetireWithSource(root, "Demo", "task-1-1", symbolic, []string{"replacement"}, false); err != nil {
		t.Fatal(err)
	}
	g, err := gstore.Load(gstore.PathFor(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.RetirementSources["task-1-1"], record) || g.SeqCounter != 4 || len(g.Nodes) != 1 {
		t.Fatalf("unexpected retirement: %+v", g)
	}
	if problems := rules.RetirementProblems(dir, g); len(problems) != 0 {
		t.Fatal(problems)
	}
	stable, _ := os.ReadFile(gstore.PathFor(dir))
	if _, err := RetireWithSource(root, "Demo", "task-1-1", source, []string{"replacement"}, false); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(gstore.PathFor(dir))
	if string(stable) != string(again) {
		t.Fatal("idempotent retirement changed bytes")
	}
	split, _, err := applySplit(g, "replacement", &model.Proposal{Version: 1, Nodes: []model.Node{{ID: "child-a"}, {ID: "child-b"}}})
	if err != nil || !reflect.DeepEqual(split.RetirementSources, g.RetirementSources) {
		t.Fatalf("split lost historical provenance: %v", err)
	}
}

func TestRetirementInvalidEvidenceWritesNothing(t *testing.T) {
	for _, kind := range []string{"missing-id", "mention-only", "missing-file", "unavailable-commit", "path-escape", "unknown-replacement", "self-replacement", "live-id"} {
		t.Run(kind, func(t *testing.T) {
			root, dir, source := retirementFixture(t)
			id, replacements := "task-1-1", []string{"replacement"}
			switch kind {
			case "missing-id":
				source.SourceID = "1.99"
			case "mention-only":
				source.SourceID = "9.9"
			case "missing-file":
				source.Path = "missing.md"
			case "unavailable-commit":
				source.Revision = strings.Repeat("0", 40)
			case "path-escape":
				source.Path = "../old.md"
			case "unknown-replacement":
				replacements = []string{"missing"}
			case "self-replacement":
				replacements = []string{id}
			case "live-id":
				id = "replacement"
			}
			before, _ := os.ReadFile(gstore.PathFor(dir))
			if _, err := RetireWithSource(root, "Demo", id, source, replacements, false); err == nil {
				t.Fatal("invalid history accepted")
			}
			after, _ := os.ReadFile(gstore.PathFor(dir))
			if string(before) != string(after) {
				t.Fatal("refusal changed graph")
			}
		})
	}
}

func TestRetirementSourceCannotBeRewritten(t *testing.T) {
	root, dir, source := retirementFixture(t)
	if _, err := RetireWithSource(root, "Demo", "task-1-1", source, []string{"replacement"}, false); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(gstore.PathFor(dir))
	source.SourceID = "1.2" // Also real, but not the original recorded identity.
	if _, err := RetireWithSource(root, "Demo", "task-1-1", source, []string{"replacement"}, false); err == nil {
		t.Fatal("immutable provenance was overwritten")
	}
	after, _ := os.ReadFile(gstore.PathFor(dir))
	if string(before) != string(after) {
		t.Fatal("conflict changed provenance")
	}
}
