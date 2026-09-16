package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func TestExtractSafeTarRejectsTraversalAndLinks(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind byte
	}{
		{"../escape", tar.TypeReg},
		{"..\\escape", tar.TypeReg},
		{"C:/escape", tar.TypeReg},
		{"safe/file:stream", tar.TypeReg},
		{"link", tar.TypeSymlink},
	} {
		var b bytes.Buffer
		tw := tar.NewWriter(&b)
		if err := tw.WriteHeader(&tar.Header{Name: tc.name, Typeflag: tc.kind, Mode: 0o644, Size: 0}); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := extractSafeTar(b.Bytes(), t.TempDir()); err == nil {
			t.Fatalf("%q was accepted", tc.name)
		}
	}
}

func TestBenchmarkProposalIsReadonlyCoveredAndMembershipComplete(t *testing.T) {
	raw, err := json.Marshal(benchmarkProposal([]map[string]string{{"root": "repository", "path": "cmd/sdd/main.go"}}))
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := decodeHistoricalProposal(raw)
	if err != nil {
		t.Fatalf("decode historical benchmark proposal: %v", err)
	}
	if _, err := model.DecodeProposal(raw); err == nil || !strings.Contains(err.Error(), "observed-v1 is historical") {
		t.Fatalf("production authoring did not reject archived observed-v1 proposal: %v", err)
	}
	if len(proposal.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(proposal.Nodes))
	}
	if !reflect.DeepEqual(proposal.Nodes[0].Inputs, proposal.Nodes[1].Inputs) || !reflect.DeepEqual(proposal.Nodes[0].Gate, proposal.Nodes[1].Gate) {
		t.Fatal("baseline and candidate do not share the same target inputs and execution profile")
	}
	for _, node := range proposal.Nodes[:2] {
		if findings := model.ValidateEvidenceGate(&node); len(findings) != 0 {
			t.Fatalf("%s evidence gate is invalid: %v", node.ID, findings)
		}
		if len(node.Artifacts) != 0 {
			t.Fatalf("%s has write artifacts: %v", node.ID, node.Artifacts)
		}
		wholeInputs := map[string]bool{}
		for _, input := range node.Inputs {
			if input.Root == "repository" && input.Section == nil {
				wholeInputs[input.Path] = true
			}
		}
		for _, required := range []string{"cmd/sdd/cost_integration_test.go", "cmd/sdd/root_test.go", "cmd/sdd/test_command_test.go"} {
			if !wholeInputs[required] {
				t.Errorf("%s lacks whole input %s", node.ID, required)
			}
		}
		if len(node.Gate.Tests) != 4 {
			t.Errorf("%s tests = %d, want 4", node.ID, len(node.Gate.Tests))
		}
		for _, test := range node.Gate.Tests {
			if !wholeInputs[test.File] {
				t.Errorf("%s test %s is not an input", node.ID, test.File)
			}
		}
		for _, requirement := range []string{"FR-01", "AC-01", "AC-02", "NFR-01"} {
			if !contains(node.Justifies, requirement) {
				t.Errorf("%s does not justify %s", node.ID, requirement)
			}
		}
	}
	review := proposal.Nodes[2]
	if review.ID != "benchmark-review" || review.Gate.Type != model.GateReview || review.Gate.Lanes != nil {
		t.Fatalf("review gate is not full: %+v", review)
	}
	if !contains(review.Deps, "benchmark-baseline") || !contains(review.Deps, "benchmark-candidate") {
		t.Fatalf("review coverage deps = %v", review.Deps)
	}
	if !contains(review.Justifies, "AC-01") {
		t.Fatalf("review does not cover related-spec AC-01: %v", review.Justifies)
	}
}

func TestWindowsGitChildrenReceiveOnlyProcessLocalLongPathsOverride(t *testing.T) {
	config := gitChildConfig()
	if runtime.GOOS != "windows" {
		if len(config) != 0 {
			t.Fatalf("non-Windows config = %v", config)
		}
		return
	}
	want := []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.longpaths", "GIT_CONFIG_VALUE_0=true"}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("Git child config = %v, want %v", config, want)
	}
}

func TestReusableBenchmarkDeclarationMustMatchExactly(t *testing.T) {
	raw, err := json.Marshal(benchmarkProposal(nil))
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := decodeHistoricalProposal(raw)
	if err != nil {
		t.Fatal(err)
	}
	want, got := proposal.Nodes[0], proposal.Nodes[0]
	if err := validateBenchmarkDeclaration(want, got); err != nil {
		t.Fatal(err)
	}
	got.Inputs = append([]model.Input(nil), got.Inputs...)
	got.Inputs[0].Path = "unexpected.go"
	if err := validateBenchmarkDeclaration(want, got); err == nil {
		t.Fatal("changed reusable benchmark declaration was accepted")
	}
}

func TestTargetBindingAllowsOptimizedWorkingSourceButNotChangedTarget(t *testing.T) {
	primary, source := t.TempDir(), t.TempDir()
	rel := "cmd/sdd/test.go"
	for root, body := range map[string][]byte{primary: []byte("baseline"), source: []byte("optimized")} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := runner{root: primary, cfg: config{Source: source}}
	binding, err := r.targetBinding(summary{SourceHashes: map[string]string{rel: digest([]byte("baseline"))}})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.PrimaryMatches || binding.SourceMatches {
		t.Fatalf("binding = %+v", binding)
	}
	if err := os.WriteFile(filepath.Join(primary, filepath.FromSlash(rel)), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	binding, err = r.targetBinding(summary{SourceHashes: map[string]string{rel: digest([]byte("baseline"))}})
	if err != nil {
		t.Fatal(err)
	}
	if binding.PrimaryMatches {
		t.Fatal("changed frozen target was accepted")
	}
}

func TestControllerIdentitiesAreIndependentOfTargetBinding(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "baseline.exe")
	candidate := filepath.Join(t.TempDir(), "candidate.exe")
	if err := os.WriteFile(baseline, []byte("baseline-controller"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("optimized-controller"), 0o755); err != nil {
		t.Fatal(err)
	}
	identities, err := controllerIdentities(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if identities["baseline"] == identities["candidate"] || identities["baseline"] == "" || identities["candidate"] == "" {
		t.Fatalf("controller identities = %v", identities)
	}
}

func TestReadonlyStateAndFreshSyncGuards(t *testing.T) {
	if err := validateReadonlyState("snapshot", "snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := validateReadonlyState("before", "after"); err == nil {
		t.Fatal("changed state passed the read-only guard")
	}
	var checks []metric
	for pair := 1; pair <= 3; pair++ {
		checks = append(checks,
			metric{Operation: fmt.Sprintf("compare-pair-%d-baseline-readonly-check", pair), Exit: 0, Counters: map[string]int64{"profile_resolutions": 1, "input_resolutions": 1, "bundle_read_requests": 4}},
			metric{Operation: fmt.Sprintf("compare-pair-%d-candidate-readonly-check", pair), Exit: 0, Counters: map[string]int64{"profile_resolutions": 1, "input_resolutions": 1, "bundle_read_requests": 3}},
		)
	}
	if err := validateCheckMetrics(checks); err != nil {
		t.Fatal(err)
	}
	checks[1].Counters["bundle_read_requests"] = 4
	if err := validateCheckMetrics(checks); err == nil {
		t.Fatal("unexpected candidate recheck count was accepted")
	}
	outcomes := []syncOutcome{
		{Controller: "baseline", Recorded: true, AnchorSnapshot: true, Result: "pass", Sequence: 1, Metric: metric{Exit: 0, Counters: map[string]int64{"profile_resolutions": 1, "input_resolutions": 1, "bundle_read_requests": 8, "legacy_anchor_scans": 1}}},
		{Controller: "candidate", Recorded: true, AnchorSnapshot: true, Result: "pass", Sequence: 2, Metric: metric{Exit: 0, Counters: map[string]int64{"profile_resolutions": 1, "input_resolutions": 1, "bundle_read_requests": 6, "legacy_anchor_scans": 0}}},
	}
	if err := validateSyncOutcomes(outcomes); err != nil {
		t.Fatal(err)
	}
	outcomes[1].Historical = true
	if err := validateSyncOutcomes(outcomes); err == nil {
		t.Fatal("historical replay passed the fresh-sync guard")
	}
}

func TestAttemptIdentityRequiresDistinctLiveClaims(t *testing.T) {
	attempts := map[string]benchmarkAttempt{
		"baseline":  {AttemptID: "at-baseline", ClaimInstance: "claim-a", WorkspaceDigest: "workspace-a"},
		"candidate": {AttemptID: "at-candidate", ClaimInstance: "claim-b", WorkspaceDigest: "workspace-b"},
	}
	if err := validateAttemptIdentities(attempts); err != nil {
		t.Fatal(err)
	}
	attempts["candidate"] = benchmarkAttempt{AttemptID: "at-candidate", ClaimInstance: "claim-a", WorkspaceDigest: "workspace-b"}
	if err := validateAttemptIdentities(attempts); err == nil {
		t.Fatal("shared claim instance was accepted")
	}
}

func TestLiveClaimValidationUsesClaimIdentityNotDerivedStateName(t *testing.T) {
	var status graphStatusEnvelope
	raw := `{"nodes":[{"id":"benchmark-baseline","state":"READY","claimed_by":"evidence-cost-exercise"},{"id":"benchmark-candidate","state":"READY","claimed_by":"evidence-cost-exercise"}]}`
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatal(err)
	}
	if err := validateLiveBenchmarkClaims(status); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestApplyProposalsOmitToolOwnedFrontmatter(t *testing.T) {
	for name, payload := range map[string]string{
		"spec":           primarySpecProposal(),
		"plan":           primaryPlanProposal(),
		"benchmark plan": benchmarkPlanProposal(),
	} {
		parts := strings.SplitN(payload, "---", 3)
		if len(parts) != 3 {
			t.Fatalf("%s proposal has malformed frontmatter", name)
		}
		frontmatter := "\n" + parts[1]
		for _, field := range []string{"type:", "status:", "created:", "updated:"} {
			if strings.Contains(frontmatter, "\n"+field) {
				t.Errorf("%s proposal carries tool-owned field %s", name, field)
			}
		}
	}
}

func TestLifecyclePathsAndOrder(t *testing.T) {
	spec, plan := primaryArtifactPaths()
	if spec != ".exercise-plans/Specs/EvidenceCost/README.md" {
		t.Fatalf("spec path = %q", spec)
	}
	if plan != ".exercise-plans/Plans/EvidenceCostRealFeature/README.md" {
		t.Fatalf("plan path = %q", plan)
	}
	if got := benchmarkArtifactPath(); got != ".exercise-plans/Plans/EvidenceCostGeneratedBenchmark/README.md" {
		t.Fatalf("benchmark path = %q", got)
	}
	if got := strings.Join(applyCommandArgs(spec, "spec"), " "); got != "apply .exercise-plans/Specs/EvidenceCost/README.md --create --type spec --json" {
		t.Fatalf("spec apply args = %q", got)
	}
	if got := strings.Join(applyCommandArgs(plan, "plan"), " "); got != "apply .exercise-plans/Plans/EvidenceCostRealFeature/README.md --create --type plan --json" {
		t.Fatalf("plan apply args = %q", got)
	}
	steps := primaryLifecycleSteps()
	want := []struct{ name, stage string }{
		{"spec-submit", "before-graph"},
		{"spec-approve", "before-graph"},
		{"plan-approve", "after-compile"},
		{"plan-activate", "after-compile"},
	}
	if len(steps) != len(want) {
		t.Fatalf("steps = %#v", steps)
	}
	for i := range want {
		if steps[i].Name != want[i].name || steps[i].Stage != want[i].stage {
			t.Fatalf("step %d = %#v, want %#v", i, steps[i], want[i])
		}
	}
}

func TestSelectSourcePathsIncludesTestsAndEmbeddedAssets(t *testing.T) {
	got := selectSourcePaths([]string{
		"README.md",
		"cmd/sdd/main.go",
		"cmd/sdd/most_test.go",
		"internal/graph/proposal/schema.json",
		"internal/x/testdata/report.xml",
		"go.mod",
		"go.sum",
	})
	want := []string{"cmd/sdd/main.go", "cmd/sdd/most_test.go", "go.mod", "go.sum", "internal/graph/proposal/schema.json", "internal/x/testdata/report.xml"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestCanonicalProspectivePathRequiresExistingParent(t *testing.T) {
	missingParent := filepath.Join(t.TempDir(), "missing", "run")
	if _, err := canonicalProspectivePath(missingParent); err == nil {
		t.Fatal("accepted a work root whose parent does not exist")
	}
}

func TestCanonicalProspectivePathResolvesExistingLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating an unprivileged directory symlink is not portable on Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalProspectivePath(link)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(got, target) {
		t.Fatalf("got %s, want %s", got, target)
	}
}

func TestExtractSafeTarWritesRegularFile(t *testing.T) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	body := []byte("ok\n")
	if err := tw.WriteHeader(&tar.Header{Name: "cmd/sdd/x.go", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := extractSafeTar(b.Bytes(), root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "cmd", "sdd", "x.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q", got)
	}
}

func TestWorkRootRelationshipHelpers(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if !within(child, root) {
		t.Fatal("child not within root")
	}
	if within(root, child) {
		t.Fatal("root within child")
	}
	if !samePath(root, root) {
		t.Fatal("same path false")
	}
}
