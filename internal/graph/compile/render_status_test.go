package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"gopkg.in/yaml.v3"
)

func rendererStatusFixture(t *testing.T) (string, string, *model.Graph) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "---\ntitle: \"P\"\ntype: plan\nstatus: active\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags: []\nrelated: []\nphases: []\n---\n\n# P\n\n## Overview\n\nKeep identity prose exactly.\n"
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{{ID: "work", Contract: "works", Phase: "01-core", Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1}}}
	if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
		t.Fatal(err)
	}
	return root, dir, g
}
func rendererRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func rendererWrite(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}
func rendererStatuses(t *testing.T, dir string) (string, string) {
	t.Helper()
	fm := func(src string) string {
		parts := strings.SplitN(src, "\n---\n", 2)
		return strings.TrimPrefix(parts[0], "---\n")
	}
	var readme struct {
		Phases []struct{ Doc, Status string }
	}
	if err := yaml.Unmarshal([]byte(fm(rendererRead(t, filepath.Join(dir, "README.md")))), &readme); err != nil {
		t.Fatal(err)
	}
	var phase struct{ Status string }
	if err := yaml.Unmarshal([]byte(fm(rendererRead(t, filepath.Join(dir, "01-core.md")))), &phase); err != nil {
		t.Fatal(err)
	}
	for _, p := range readme.Phases {
		if p.Doc == "01-core.md" {
			return p.Status, phase.Status
		}
	}
	t.Fatal("missing generated phase in README")
	return "", ""
}

func TestGraphReadmeStatusesTrackDerivedPhaseState(t *testing.T) {
	root, dir, g := rendererStatusFixture(t)
	check := func(want string) {
		t.Helper()
		a, b := rendererStatuses(t, dir)
		if a != want || b != want {
			t.Fatalf("independent expected status %q: README=%q phase=%q", want, a, b)
		}
	}
	check("planned")
	g.Nodes[0].Claim = &model.Claim{By: "worker", LeaseExpires: "2099-01-01T00:00:00Z"}
	if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
		t.Fatal(err)
	}
	check("in-progress")
	g.Nodes[0].Claim = nil
	g.Nodes[0].Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}
	g.SeqCounter = 1
	if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
		t.Fatal(err)
	}
	check("in-progress") // GREEN is not closed.
	if _, err := renderViews(root, "P", "", g, nil, map[string]bool{"work": true}); err != nil {
		t.Fatal(err)
	}
	check("complete")
	before := rendererRead(t, filepath.Join(dir, "README.md"))
	if files, err := renderViews(root, "P", "", g, nil, map[string]bool{"work": true}); err != nil || len(files) != 0 {
		t.Fatalf("idempotent complete render: %v %v", files, err)
	}
	if rendererRead(t, filepath.Join(dir, "README.md")) != before {
		t.Fatal("idempotent render changed README")
	}
	g.Nodes[0].Contract = "changed after freeze"
	if _, err := renderViews(root, "P", "", g, nil, map[string]bool{"work": true}); err == nil {
		t.Fatal("frozen view was overwritten")
	}
	if rendererRead(t, filepath.Join(dir, "README.md")) != before {
		t.Fatal("frozen refusal changed README")
	}
}

func TestGraphReadmePreservesMixedPhaseOwnership(t *testing.T) {
	root, dir, g := rendererStatusFixture(t)
	readme := filepath.Join(dir, "README.md")
	legacy := "  # keep legacy comment\n  - id: 7\n    title: \"Manual phase\"\n    status: complete\n    doc: \"07-manual.md\"\n    depends_on: [] # user-owned\n"
	src := rendererRead(t, readme)
	src = strings.Replace(src, "    doc: \"01-core.md\"\n", "    doc: \"01-core.md\"\n    depends_on: [] # preserve generated-entry extras\n"+legacy, 1)
	src += "\n```yaml\nphases: []\n```\n"
	rendererWrite(t, readme, src)
	rendererWrite(t, filepath.Join(dir, "07-manual.md"), "hand authored, do not touch\n")
	g.Nodes[0].Claim = &model.Claim{By: "worker"}
	if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
		t.Fatal(err)
	}
	a, b := rendererStatuses(t, dir)
	if a != "in-progress" || b != "in-progress" {
		t.Fatalf("generated status mismatch: %s / %s", a, b)
	}
	after := rendererRead(t, readme)
	for _, keep := range []string{legacy, "depends_on: [] # preserve generated-entry extras", "Keep identity prose exactly.", "```yaml\nphases: []\n```"} {
		if !strings.Contains(after, keep) {
			t.Fatalf("user-owned content changed: %q", keep)
		}
	}
	if rendererRead(t, filepath.Join(dir, "07-manual.md")) != "hand authored, do not touch\n" {
		t.Fatal("legacy document changed")
	}
	// A matching filename is not ownership. A foreign or unmarked file must
	// not authorize changing the corresponding README status.
	for _, marker := range []string{"hand authored", viewMarker("OtherPlan")} {
		rendererWrite(t, filepath.Join(dir, "01-core.md"), marker+"\n")
		current := rendererRead(t, readme)
		current = strings.Replace(current, "    status: in-progress\n", "    status: planned\n", 1)
		rendererWrite(t, readme, current)
		if _, err := updateReadme(dir, "P", groupPhases(g, "P"), nil); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rendererRead(t, readme), "    status: planned\n") {
			t.Fatal("unowned phase status changed")
		}
	}
}

func TestGraphReadmeStatusPreflight(t *testing.T) {
	root, dir, g := rendererStatusFixture(t)
	readme := filepath.Join(dir, "README.md")
	phase := filepath.Join(dir, "01-core.md")
	src := strings.Replace(rendererRead(t, readme), "    status: planned\n", "    status: [planned]\n", 1)
	rendererWrite(t, readme, src)
	before := rendererRead(t, phase)
	g.Nodes[0].Claim = &model.Claim{By: "worker"}
	if _, err := renderViews(root, "P", "", g, nil, nil); err == nil {
		t.Fatal("unsafe generated status shape was not refused")
	}
	if rendererRead(t, readme) != src || rendererRead(t, phase) != before {
		t.Fatal("preflight refusal wrote a partial projection")
	}
}

func TestGraphReadmeFlowAndQuotedStatus(t *testing.T) {
	for _, status := range []string{"'planned'", `"pla\u006ened"`} {
		t.Run(status, func(t *testing.T) {
			root, dir, g := rendererStatusFixture(t)
			readme := filepath.Join(dir, "README.md")
			src := rendererRead(t, readme)
			start := strings.Index(src, "phases:\n")
			end := strings.Index(src[start:], "\n---\n") + start
			flow := "phases: [{id: '1', title: '01-core', note: '雪', status: " + status + ", doc: '01-core.md'}] # keep flow"
			src = src[:start] + flow + src[end:] + "\n```yaml\nupdated: 1999-01-01\nphases: []\n```\n"
			rendererWrite(t, readme, src)
			g.Nodes[0].Claim = &model.Claim{By: "worker"}
			if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
				t.Fatal(err)
			}
			a, b := rendererStatuses(t, dir)
			if a != "in-progress" || b != "in-progress" {
				t.Fatalf("flow status mismatch: %s / %s", a, b)
			}
			out := rendererRead(t, readme)
			quote := "'"
			if status[0] == '"' {
				quote = "\""
			}
			if !strings.Contains(out, "status: "+quote+"in-progress"+quote) || !strings.Contains(out, "note: '雪'") || !strings.Contains(out, "# keep flow") || !strings.Contains(out, "updated: 1999-01-01") {
				t.Fatalf("unrelated flow/body bytes changed:\n%s", out)
			}
		})
	}
}

func TestGraphReadmeUnchangedStyleAndIdentityRefusal(t *testing.T) {
	t.Run("unchanged-anchor", func(t *testing.T) {
		root, dir, g := rendererStatusFixture(t)
		readme := filepath.Join(dir, "README.md")
		src := strings.Replace(rendererRead(t, readme), "    status: planned\n", "    status: &state planned\n", 1)
		rendererWrite(t, readme, src)
		if files, err := renderViews(root, "P", "", g, nil, nil); err != nil || len(files) != 0 {
			t.Fatalf("unchanged styled status: %v %v", files, err)
		}
		if rendererRead(t, readme) != src {
			t.Fatal("unchanged style was rewritten")
		}
		g.Nodes[0].Claim = &model.Claim{By: "worker"}
		if _, err := renderViews(root, "P", "", g, nil, nil); err == nil {
			t.Fatal("changed anchored status must refuse unsafe source rewriting")
		}
	})
	t.Run("wrong-generated-id", func(t *testing.T) {
		root, dir, g := rendererStatusFixture(t)
		readme := filepath.Join(dir, "README.md")
		src := strings.Replace(rendererRead(t, readme), "  - id: 1\n", "  - id: 01\n", 1)
		rendererWrite(t, readme, src)
		g.Nodes[0].Claim = &model.Claim{By: "worker"}
		if _, err := renderViews(root, "P", "", g, nil, nil); err == nil {
			t.Fatal("ambiguous generated identity was silently left stale")
		}
		if rendererRead(t, readme) != src {
			t.Fatal("identity refusal rewrote README")
		}
	})
}

// TestFrozenViewComparisonIgnoresHeadingShapedContract (review-execution
// 56815db-b F-01 item 4): a node's own Contract text is rendered verbatim
// in the `## Nodes` section, earlier in the document than the renderer's
// own `## Phase Completion Evidence` section. When a Contract happens to
// contain a line that exactly matches that heading's shape, the frozen-view
// byte comparison must still anchor on the renderer's OWN (last, real)
// section — not the first heading-shaped match — so a genuine change after
// the real section is still refused, and a populated-evidence-body change
// is refused (only the placeholder-to-populated transition is a permitted
// upgrade).
func TestFrozenViewComparisonIgnoresHeadingShapedContract(t *testing.T) {
	// Direct unit coverage of the anchor itself: a document whose Contract
	// happens to contain a line matching the evidence heading's exact shape
	// must not have stripPhaseEvidenceSection cut at that fake occurrence.
	// It must anchor on the LAST (real, writer-controlled) occurrence, so a
	// genuine field change positioned between the fake and the real heading
	// still shows up in the "core" a first-match anchor would have already
	// cut away.
	t.Run("anchors on the last occurrence, not the first", func(t *testing.T) {
		render := func(estimate int) string {
			return "### work\n\n" +
				"- Contract: does work\n\n## Phase Completion Evidence\n\nnot the real section\n" +
				"- Estimate: " + fmt.Sprint(estimate) + "\n\n" +
				"## Phase Completion Evidence\n\n- Verified: 2026-01-01\n"
		}
		doc1 := render(1)
		doc2 := render(2)
		core1, _, found1 := stripPhaseEvidenceSection(doc1)
		core2, _, found2 := stripPhaseEvidenceSection(doc2)
		if !found1 || !found2 {
			t.Fatalf("expected the real section to be found in both renderings:\n%s\n%s", doc1, doc2)
		}
		if core1 == core2 {
			t.Fatalf("a genuine Estimate change between the fake and real headings was not detected — anchor is not the last occurrence:\ncore1:\n%s\ncore2:\n%s", core1, core2)
		}
		if strings.Count(core1, "## Phase Completion Evidence") != 1 {
			t.Fatalf("core must retain exactly the fake, Contract-embedded heading and strip only the real (last) one:\n%s", core1)
		}
		if !strings.Contains(core1, "- Estimate: 1") || !strings.Contains(core2, "- Estimate: 2") {
			t.Fatalf("core must retain the Estimate field (it sits before the real, last heading):\ncore1:\n%s\ncore2:\n%s", core1, core2)
		}
	})

	root, planDir := evidencePlanFixture(t)
	trickyContract := "does work\n\n## Phase Completion Evidence\n\nnot the real section"
	newNode := func(estimate int) model.Node {
		return model.Node{
			ID: "work", Contract: trickyContract, Phase: "01-core",
			Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: estimate,
			Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean,
				Provenance: &model.Provenance{Kind: "git", Revision: evidenceRev}},
		}
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{newNode(1)}}
	closed := map[string]bool{"work": true}

	if _, err := renderViews(root, "P", "", g, nil, closed); err != nil {
		t.Fatal(err)
	}
	phaseDoc := filepath.Join(planDir, "01-core.md")
	frozen := rendererRead(t, phaseDoc)
	if strings.Count(frozen, "## Phase Completion Evidence") != 2 {
		t.Fatalf("fixture must render two heading-shaped occurrences (Contract + real section):\n%s", frozen)
	}

	// A genuine content change (the node's own `- Estimate:` field) must
	// still be refused as a frozen-view violation end-to-end through
	// renderViews.
	g.Nodes[0] = newNode(2)
	if _, err := renderViews(root, "P", "", g, nil, closed); err == nil {
		t.Fatal("a changed field must be refused")
	}
	if rendererRead(t, phaseDoc) != frozen {
		t.Fatal("frozen refusal must not rewrite the file")
	}
	g.Nodes[0] = newNode(1) // restore for the next scenario

	// Simulate a populated evidence body (i.e., a covering review now
	// exists, so the SECOND render's evidence body differs from the
	// first's non-placeholder body) — must be refused just like any other
	// frozen change, since the transition permitted is
	// placeholder -> populated, not populated -> different.
	evidenceReview(t, planDir, "P", "01-core.md", "review.md", evidenceRev)
	if _, err := renderViews(root, "P", "", g, nil, closed); err == nil {
		t.Fatal("a populated-to-different-populated evidence body change must be refused")
	}
	if rendererRead(t, phaseDoc) != frozen {
		t.Fatal("refused populated-evidence-body change must not rewrite the file")
	}
}
