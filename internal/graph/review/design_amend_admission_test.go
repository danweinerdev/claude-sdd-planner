package review

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

const designOnlyAmendFinding = "  - id: F-01\n    severity: major\n    title: \"design contract needs revision\"\n    status: open\n    action: revise\n    nodes: [a]\n    revise:\n      contract: \"does a under the revised design\"\n"

func TestDesignOnlyAmendAllowsMissingLaneRowsWithoutRecordingReviewEvidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lanes map[string]string
	}{
		{name: "zero lane rows", lanes: map[string]string{}},
		{name: "partial lane rows", lanes: map[string]string{"review_quality": "CHANGES/Amend"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := work("a", nil)
			a.Verification = pass(1)
			root, planDir := fixture(t, 1, a, fullGate("g1", []string{"a"}))
			writeFile(t, root, "reviews/design.md", artifactText("resolved", true, "Amend", tc.lanes, designOnlyAmendFinding))
			artifact := filepath.Join(root, "reviews", "design.md")

			checked, err := Check(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artifact})
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			if len(checked.Problems) != 0 {
				t.Fatalf("a design-only Amend review may omit code-lane rows: %+v", checked.Problems)
			}

			res, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artifact})
			if err != nil {
				t.Fatalf("record: %v", err)
			}
			if res.Plan == nil || len(res.Plan.Amendments) != 1 || res.Observation != nil {
				t.Fatalf("Amend must return only an amendment preview: %+v", res)
			}
			g, err := gstore.Load(gstore.PathFor(planDir))
			if err != nil {
				t.Fatal(err)
			}
			if g.NodeByID("g1").Verification != nil || states.Derive(states.Inputs{Graph: g})["g1"].State == states.Green {
				t.Fatalf("preview must neither record an observation nor green the review gate: %+v", g.NodeByID("g1"))
			}
		})
	}
}

func TestMissingLaneRowsRemainRequiredForAligned(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lanes map[string]string
	}{
		{name: "zero lane rows", lanes: map[string]string{}},
		{name: "partial lane rows", lanes: map[string]string{"review_quality": "PASS/Aligned"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := fixture(t, 0, work("a", nil), fullGate("g1", []string{"a"}))
			writeFile(t, root, "reviews/aligned.md", artifactText("resolved", true, "Aligned", tc.lanes, ""))
			artifact := filepath.Join(root, "reviews", "aligned.md")

			if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artifact}); err == nil || !strings.Contains(err.Error(), "is absent") {
				t.Fatalf("Aligned must still require every configured lane: %v", err)
			}
			checked, err := Check(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artifact})
			if err != nil {
				t.Fatal(err)
			}
			if len(checked.Problems) == 0 {
				t.Fatal("Check must report missing lanes for Aligned")
			}
		})
	}
}

func TestDesignOnlyAmendStillRefusesMalformedOrDuplicatePresentLaneRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		text func() string
		want string
	}{
		{
			name: "malformed token",
			text: func() string {
				return artifactText("resolved", true, "Amend", map[string]string{"review_quality": "TODO/Unfilled"}, designOnlyAmendFinding)
			},
			want: "review_quality reports",
		},
		{
			name: "duplicate lane",
			text: func() string {
				src := artifactText("resolved", true, "Amend", map[string]string{"review_quality": "CHANGES/Amend"}, designOnlyAmendFinding)
				return strings.Replace(src, "findings:\n", "  - lane: review_quality\n    result: CHANGES/Amend\n    evidence: \"second review\"\nfindings:\n", 1)
			},
			want: "review_quality appears more than once",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := fixture(t, 0, work("a", nil), fullGate("g1", []string{"a"}))
			writeFile(t, root, "reviews/bad.md", tc.text())
			artifact := filepath.Join(root, "reviews", "bad.md")

			if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artifact}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Record must refuse a present invalid lane row with %q: %v", tc.want, err)
			}
			checked, err := Check(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artifact})
			if err != nil {
				t.Fatal(err)
			}
			if len(checked.Problems) == 0 || !strings.Contains(strings.Join(checked.Problems, "\n"), tc.want) {
				t.Fatalf("Check must report %q: %+v", tc.want, checked.Problems)
			}
		})
	}
}
