package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// AC-08 (user-entrypoint): the real binary, run as a subprocess against a
// planning root whose validation must consult git, exits 2 with the cause
// when git cannot run — never 0 (clean) and never 1 (authoritative
// findings). The same root with git available exits 0.
func TestValidationOperationalExit(t *testing.T) {
	bin := stressBinary(t)
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	doc := "---\n" +
		"title: \"Clean\"\ntype: research\nstatus: draft\n" +
		"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\ntags: [\"a\"]\nrelated: []\n---\n\n" +
		"# Clean\n\n## Summary\n\nText.\n\n## Context\n\nText.\n\n## Findings\n\nText.\n\n## Analysis\n\nText.\n\n## Open Questions\n\nText.\n"
	if err := os.MkdirAll(filepath.Join(root, "Research"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Research", "clean.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(gitExe, "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	base := []string{"SDD_VCS_DISABLE_P4=1", "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	run := func(path string) (int, string) {
		cmd := exec.Command(bin, "validate", "--root", root)
		cmd.Dir = root
		cmd.Env = append(append([]string{}, base...), "PATH="+path)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running sdd: %v", err)
		}
		return code, string(out)
	}

	if code, out := run(filepath.Dir(gitExe)); code != 0 {
		t.Fatalf("control with git: exit %d\n%s", code, out)
	}
	code, out := run(t.TempDir())
	if code != 2 {
		t.Fatalf("validate without git: exit %d, want 2 (operational)\n%s", code, out)
	}
	if !strings.Contains(out, "git") || !strings.Contains(strings.ToLower(out), "could not") {
		t.Errorf("operational exit must name the cause; got:\n%s", out)
	}
	if strings.Contains(out, "0 error") || strings.Contains(out, "valid: true") {
		t.Errorf("operational failure reported as clean:\n%s", out)
	}
}

// lifecycleOperationalFixture builds a planning root whose plan targets a
// real Git repository, so every verb under test must consult git.
func lifecycleOperationalFixture(t *testing.T) (root, planningRev, mapPath string) {
	t.Helper()
	root = t.TempDir()
	target := t.TempDir()
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		argv := append([]string{"-C", dir, "-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid"}, args...)
		out, err := exec.Command(gitExe, argv...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(base, rel, body string) {
		t.Helper()
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(target, "init", "-q", "-b", "main")
	write(target, "base.txt", "base\n")
	git(target, "add", "base.txt")
	git(target, "commit", "-q", "-m", "base")
	rev := git(target, "rev-parse", "HEAD")
	write(target, "next.txt", "next\n")
	git(target, "add", "next.txt")
	git(target, "commit", "-q", "-m", "next")
	newRev := git(target, "rev-parse", "HEAD")

	write(root, "planning-config.json",
		fmt.Sprintf(`{"planningRoot":".","planMapping":{"Demo":"target"},"repositories":{"target":{"path":%q}}}`, target))
	write(root, "Plans/Demo/README.md",
		"---\ntitle: Demo\ntype: plan\nstatus: active\ncreated: 2026-09-12\nupdated: 2026-09-12\n"+
			"tags: []\nrelated: []\nphases: []\n---\n\n# Demo\n\n## Overview\n\nText.\n")

	g := &model.Graph{Version: 1, Nodes: []model.Node{{
		ID: "work", Contract: "works", Gate: model.Gate{Type: model.GateCommand, Command: "true"},
		Hazards: model.Hazards{}, Estimate: 1,
		Verification: &model.Verification{Result: model.ResultPass, Seq: 1,
			Isolation:  model.IsolationClean,
			Provenance: &model.Provenance{Kind: "git", Revision: rev}},
	}}}
	if err := gstore.Save(gstore.PathFor(filepath.Join(root, "Plans", "Demo")), g); err != nil {
		t.Fatal(err)
	}
	mapPath = filepath.Join(root, "rewrite-map.txt")
	write(root, "rewrite-map.txt", rev+" "+newRev+"\n")

	// `evidence add` resolves its repository by walking up from the artifact
	// to a .git marker, so the planning root is a committed repository too:
	// with git present the verb reaches a real HEAD and a clean tree.
	git(root, "init", "-q", "-b", "main")
	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "planning root")
	planningRev = git(root, "rev-parse", "HEAD")
	return root, planningRev, mapPath
}

// TestTransitionGateOperationalSweepExits (review-execution F-02): the
// lifecycle transition gate (gateDiagnostics) runs its before and after
// sweeps through the checked evaluation entry points and exits 2 with the
// cause when either sweep is operational. Today gateDiagnostics calls
// rules.RunWithWaivers (the unchecked form) for both sweeps and never
// inspects the operational outcome, so the same SDD198 finding in both
// sweeps dedups to an empty introduced set and `plan approve` proceeds —
// this test documents that defect red and must start failing once
// gateDiagnostics switches to the checked entry point.
//
// `plan approve` is the cheapest verb that reaches gateDiagnostics: it needs
// only a plan at status `draft` with no other blocking findings, no task
// IDs, and no candidate-artifact freezing pass.
func TestTransitionGateOperationalSweepExits(t *testing.T) {
	bin := stressBinary(t)
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		argv := append([]string{"-C", dir, "-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid"}, args...)
		out, err := exec.Command(gitExe, argv...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("planning-config.json", `{"planningRoot":"."}`)
	write("Plans/Demo/README.md",
		"---\ntitle: Demo\ntype: plan\nstatus: draft\ncreated: 2026-09-12\nupdated: 2026-09-12\n"+
			"tags: []\nrelated: []\nphases: []\n---\n\n# Demo\n\n## Overview\n\nText.\n")

	// The planning root itself must be a committed repository: gateDiagnostics
	// scopes its rule sweeps against the repository the artifact lives in, and
	// several rules (e.g. the clean-worktree check) query it directly.
	git(root, "init", "-q", "-b", "main")
	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "planning root")

	base := []string{"SDD_VCS_DISABLE_P4=1", "HOME=" + t.TempDir(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	run := func(path string, extraArgs ...string) (int, string) {
		t.Helper()
		args := append([]string{"plan", "approve", "Plans/Demo/README.md"}, extraArgs...)
		cmd := exec.Command(bin, args...)
		cmd.Dir = root
		cmd.Env = append(append([]string{}, base...), "PATH="+path)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running sdd plan approve: %v", err)
		}
		return code, string(out)
	}

	// Control: git available, --dry-run so it never mutates the fixture. The
	// transition may refuse (exit 1) or succeed (exit 0) depending on
	// findings, but it must never be an operational exit — establishing that
	// the fixture itself is not the cause of an operational failure.
	if code, out := run(filepath.Dir(gitExe), "--dry-run"); code == 2 {
		t.Fatalf("control with git exited 2 (operational):\n%s", out)
	}

	statusBefore, err := os.ReadFile(filepath.Join(root, "Plans", "Demo", "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	code, out := run(t.TempDir())
	if code != 2 {
		t.Fatalf("without git: exit %d, want 2 (operational)\n%s", code, out)
	}
	low := strings.ToLower(out)
	if !strings.Contains(low, "git") || !strings.Contains(low, "could not") {
		t.Errorf("the operational exit must name the cause; got:\n%s", out)
	}

	statusAfter, err := os.ReadFile(filepath.Join(root, "Plans", "Demo", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(statusAfter) != string(statusBefore) {
		t.Errorf("an operational sweep must not let the transition proceed; artifact changed:\nbefore:\n%s\nafter:\n%s",
			statusBefore, statusAfter)
	}
}

// reviewResolveOperationalFixture builds a planning root with a phase-gate
// review artifact whose frontmatter passes review resolve's earlier checks
// (review.go:625-675): all four lanes present with real evidence, verdict
// Aligned, no open findings, no untracked followups. Only the final
// candidateArtifactErrors sweep (review.go:713-719) remains to run.
func reviewResolveOperationalFixture(t *testing.T) (root, reviewPath string) {
	t.Helper()
	root = t.TempDir()
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		argv := append([]string{"-C", dir, "-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid"}, args...)
		out, err := exec.Command(gitExe, argv...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("planning-config.json", `{"planningRoot":"."}`)
	write("Plans/Demo/README.md",
		"---\ntitle: Demo\ntype: plan\nstatus: active\ncreated: 2026-09-12\nupdated: 2026-09-12\n"+
			"tags: []\nrelated: []\nphases: []\n---\n\n# Demo\n\n## Overview\n\nText.\n")
	write("Plans/Demo/01-phase.md",
		"---\ntitle: Phase One\ntype: phase\nstatus: in-progress\ncreated: 2026-09-12\nupdated: 2026-09-12\n"+
			"tags: []\nrelated: []\ntasks: []\n---\n\n# Phase One\n\n## Overview\n\nText.\n")

	// The planning root must be a committed repository first, so the review's
	// `rev` range and the planning revision it reads at scaffold time both
	// name real, ancestor-related commits.
	git(root, "init", "-q", "-b", "main")
	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "base")
	base := git(root, "rev-parse", "HEAD")
	write("Plans/Demo/01-phase.md",
		"---\ntitle: Phase One\ntype: phase\nstatus: in-progress\ncreated: 2026-09-12\nupdated: 2026-09-13\n"+
			"tags: []\nrelated: []\ntasks: []\n---\n\n# Phase One\n\n## Overview\n\nText updated.\n")
	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "phase update")
	endpoint := git(root, "rev-parse", "HEAD")
	rangeRev := base + ".." + endpoint

	reviewPath = "Plans/Demo/reviews/01-review-demo-" + endpoint[:7] + ".md"
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: \"Phase review: 01-phase\"\n")
	b.WriteString("type: review\n")
	b.WriteString("status: open\n")
	b.WriteString("created: 2026-09-13\n")
	b.WriteString("updated: 2026-09-13\n")
	b.WriteString("tags: [review]\n")
	b.WriteString("related: [\"Plans/Demo/01-phase.md\"]\n")
	b.WriteString("review_of: \"Plans/Demo/01-phase.md\"\n")
	fmt.Fprintf(&b, "rev: %q\n", rangeRev)
	b.WriteString("review_scope: phase\n")
	b.WriteString("frozen: false\n")
	b.WriteString("verdict: Aligned\n")
	fmt.Fprintf(&b, "reviewed_planning_revision: %q\n", endpoint)
	b.WriteString("review_mode: independent\n")
	b.WriteString("lane_results:\n")
	for _, l := range []string{"review_plan_drift", "review_quality", "review_spec_compliance", "review_blind_spots"} {
		fmt.Fprintf(&b, "  - lane: %s\n", l)
		b.WriteString("    result: PASS/Aligned\n")
		fmt.Fprintf(&b, "    reviewed_identity: %q\n", rangeRev)
		b.WriteString("    evidence: \"agent read the full diff and phase doc; nothing outstanding\"\n")
	}
	b.WriteString("findings: []\n")
	b.WriteString("followups: []\n")
	b.WriteString("---\n\n")
	b.WriteString("# Phase review: 01-phase\n\n")
	fmt.Fprintf(&b, "Reviewed `Plans/Demo/01-phase.md` at frozen identity `%s`.\n\n", rangeRev)
	b.WriteString("## Findings\n\nNone.\n\n")
	b.WriteString("## Resolution Log\n\nNone.\n")
	write(reviewPath, b.String())

	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "add review")
	return root, reviewPath
}

// TestReviewResolveOperationalSweepExits (review-execution F-01): `review
// resolve` on a phase-gate review calls candidateArtifactErrors
// (transition.go:317-355) as its final gate before writing `status: resolved`
// and `frozen: true`. Today that function calls the unchecked
// rules.RunWithWaivers and filters findings by `d.Path == rel`, which drops
// the synthesized SDD198 (Path ".") an operational sweep failure produces —
// so review resolve silently proceeds to write on an unvalidated root. This
// test documents that defect red and must start failing once
// candidateArtifactErrors switches to the checked entry point.
func TestReviewResolveOperationalSweepExits(t *testing.T) {
	bin := stressBinary(t)
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	root, reviewPath := reviewResolveOperationalFixture(t)

	base := []string{"SDD_VCS_DISABLE_P4=1", "HOME=" + t.TempDir(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	run := func(path string, extraArgs ...string) (int, string) {
		t.Helper()
		args := append([]string{"review", "resolve", reviewPath}, extraArgs...)
		cmd := exec.Command(bin, args...)
		cmd.Dir = root
		cmd.Env = append(append([]string{}, base...), "PATH="+path)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running sdd review resolve: %v", err)
		}
		return code, string(out)
	}

	// Control: git available, --dry-run so it never mutates the fixture. The
	// gate may refuse (exit 1) or succeed (exit 0), but it must never be an
	// operational exit — establishing the fixture itself is not the cause.
	if code, out := run(filepath.Dir(gitExe), "--dry-run"); code == 2 {
		t.Fatalf("control with git exited 2 (operational):\n%s", out)
	}

	before, err := os.ReadFile(filepath.Join(root, reviewPath))
	if err != nil {
		t.Fatal(err)
	}

	code, out := run(t.TempDir())
	if code != 2 {
		t.Fatalf("without git: exit %d, want 2 (operational)\n%s", code, out)
	}
	low := strings.ToLower(out)
	if !strings.Contains(low, "git") || !strings.Contains(low, "could not") {
		t.Errorf("the operational exit must name the cause; got:\n%s", out)
	}

	after, err := os.ReadFile(filepath.Join(root, reviewPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("an operational sweep must not let review resolve write; artifact changed:\nbefore:\n%s\nafter:\n%s",
			before, after)
	}
	if strings.Contains(string(after), "status: resolved") || strings.Contains(string(after), "frozen: true") {
		t.Errorf("an operational sweep must not resolve or freeze the review:\n%s", after)
	}
}

// TestLifecycleVerbsOperationalExit (FR-16, AC-08, DD-10): the lifecycle
// verbs that consult the VCS — `evidence add` and `graph remap-revisions` —
// exit 2 with the cause named when git cannot run, never 1 (an authoritative
// refusal about the repository's contents) and never 0. With git present the
// same invocations do not exit 2.
func TestLifecycleVerbsOperationalExit(t *testing.T) {
	bin := stressBinary(t)
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	root, planningRev, mapPath := lifecycleOperationalFixture(t)

	base := []string{"SDD_VCS_DISABLE_P4=1", "HOME=" + t.TempDir(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	run := func(path string, args ...string) (int, string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = root
		cmd.Env = append(append([]string{}, base...), "PATH="+path)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running sdd %s: %v", strings.Join(args, " "), err)
		}
		return code, string(out)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"evidence add", []string{"evidence", "add", filepath.Join(root, "Plans", "Demo", "README.md"),
			"--plan", "--verified-by", "go test ./...", "--result", "ok",
			"--revision", planningRev, "--dry-run"}},
		{"graph remap-revisions", []string{"graph", "remap-revisions", "--plan", "Demo",
			"--map", mapPath, "--dry-run"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Control: git available. Whatever the verb decides, it is never
			// an operational exit.
			if code, out := run(filepath.Dir(gitExe), tc.args...); code == 2 {
				t.Fatalf("control with git exited 2 (operational):\n%s", out)
			}

			code, out := run(t.TempDir(), tc.args...)
			if code != 2 {
				t.Fatalf("without git: exit %d, want 2 (operational)\n%s", code, out)
			}
			low := strings.ToLower(out)
			if !strings.Contains(low, "git") || !strings.Contains(low, "could not") {
				t.Errorf("the operational exit must name the cause; got:\n%s", out)
			}
			// The inability must never be dressed as an authoritative answer
			// about the repository's contents.
			for _, forbidden := range []string{"does not exist", "not a Git repository",
				"uncommitted changes", "is not a full native revision identifier"} {
				if strings.Contains(out, forbidden) {
					t.Errorf("an operational failure was reported as %q:\n%s", forbidden, out)
				}
			}
		})
	}
}

// installNShotGitShim is internal/rules/operational_test.go's helper of the
// same name, copied here (the gate builds the real binary as a subprocess,
// which cannot share test-package internals across packages): a shim git
// that answers n invocations by delegating to the real binary before
// removing its own execute bit, so detection succeeds and only the
// following probe fails to start.
func installNShotGitShim(t *testing.T, n int) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	chmod, err := exec.LookPath("chmod")
	if err != nil {
		t.Skip("chmod not installed")
	}
	// cat must be resolved to an absolute path here (not merely available
	// as a bare command): the shim's PATH becomes exactly shimDir once
	// installed, so "cat" alone would not resolve inside the script's own
	// invocation of itself.
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat not installed")
	}
	counterFile := filepath.Join(t.TempDir(), "count")
	if err := os.WriteFile(counterFile, []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	shimDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"n=$(" + cat + " '" + counterFile + "')\n" +
		"n=$((n + 1))\n" +
		"echo $n > '" + counterFile + "'\n" +
		"if [ $n -ge " + fmt.Sprint(n) + " ]; then " + chmod + " -x \"$0\"; fi\n" +
		"exec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir)
}

// retireCompileOperationalFixture builds a git-backed planning root whose
// plan's compiled graph already carries a retirement source pinned to a
// real historical commit, so VerifyRetirementSource's detection succeeds
// and only the RevisionExists probe is left for the n-shot git shim to
// fail operationally. It also stages a trivial valid payload so `compile`
// reaches semanticFindings (and therefore RetirementProblems) rather than
// refusing earlier on missing input.
func retireCompileOperationalFixture(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		argv := append([]string{"-C", dir, "-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid"}, args...)
		out, err := exec.Command(gitExe, argv...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("planning-config.json", `{"planningRoot":"."}`)
	write("Plans/Demo/README.md",
		"---\ntitle: Demo\ntype: plan\nstatus: active\ncreated: 2026-09-12\nupdated: 2026-09-12\n"+
			"tags: []\nrelated: []\nphases: []\n---\n\n# Demo\n\n## Overview\n\nText.\n")
	write("old.md", "---\ntasks:\n  - id: \"1.1\"\n---\n")

	git(root, "init", "-q", "-b", "main")
	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "base")
	rev := git(root, "rev-parse", "HEAD")

	graph := `{"version":1,"nodes":[],"retired":["old"],"retirement_sources":{"old":{"source":{"vcs":"git","revision":"` + rev + `","path":"old.md","source_id":"1.1"}}}}`
	write("Plans/Demo/Demo-Graph.json", graph)
	write("payload.json", `{"version":1,"nodes":[]}`)

	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "graph and payload")
	return root
}

// TestRetireAndCompileOperationalSourceExit (review-execution F-02): `graph
// retire` and `compile` both consume a retirement source's verification
// through RetirementProblems inside RetireWithSource's apply and
// semanticFindings respectively. Both call the lossy RetirementProblems
// today, which folds an operational VerifyRetirementSource failure into an
// ordinary problem string — so an unanswered historical-commit probe is
// reported and refused as though it were a genuine finding (exit 1) rather
// than surfaced as operational (exit 2), and worse, `compile` may still
// proceed if RetirementProblems' folded string doesn't trip a refusal path
// the caller checks for. This test documents the current (wrong) behavior
// red: once both callers switch to RetirementProblemsChecked and propagate
// the error, both must exit 2, name the cause, and leave the graph file
// byte-for-byte unchanged / make no write.
func TestRetireAndCompileOperationalSourceExit(t *testing.T) {
	bin := stressBinary(t)
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}

	base := []string{"SDD_VCS_DISABLE_P4=1", "HOME=" + t.TempDir(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	run := func(dir, path string, args ...string) (int, string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append(append([]string{}, base...), "PATH="+path)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running sdd %s: %v", strings.Join(args, " "), err)
		}
		return code, string(out)
	}

	t.Run("graph retire", func(t *testing.T) {
		root := retireCompileOperationalFixture(t)
		graphPath := filepath.Join(root, "Plans", "Demo", "Demo-Graph.json")
		args := []string{"graph", "retire", "--plan", "Demo", "--id", "other",
			"--source-rev", "2222222222222222222222222222222222222222",
			"--source-path", "old.md", "--source-id", "1.2", "--dry-run"}

		// Control: git available. The retirement source is bogus (a fake
		// revision), so the verb may refuse (exit 1), but it must never be
		// operational (exit 2) — establishing the fixture itself is not the
		// cause.
		if code, out := run(root, filepath.Dir(gitExe), args...); code == 2 {
			t.Fatalf("control with git exited 2 (operational):\n%s", out)
		}

		before, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatal(err)
		}

		installNShotGitShim(t, 3)
		shimPath := os.Getenv("PATH")
		code, out := run(root, shimPath, args...)
		if code != 2 {
			t.Fatalf("without a working git: exit %d, want 2 (operational)\n%s", code, out)
		}
		low := strings.ToLower(out)
		if !strings.Contains(low, "git") || !strings.Contains(low, "could not") {
			t.Errorf("the operational exit must name the cause; got:\n%s", out)
		}

		after, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("an operational retirement-source verification must not let retire write; graph changed:\nbefore:\n%s\nafter:\n%s",
				before, after)
		}
	})

	t.Run("compile", func(t *testing.T) {
		// Control fixture: its own copy, so compile (which consumes the
		// staged proposal and appends to the graph on success) never shares
		// mutable state with the fixture the operational run inspects.
		controlRoot := retireCompileOperationalFixture(t)
		if out, errb, err := runSdd(bin, controlRoot, "graph", "propose", "--plan", "Demo", "--file", "payload.json"); err != nil {
			t.Fatalf("setup control propose: %v\nstdout: %s\nstderr: %s", err, out, errb)
		}
		if code, out := run(controlRoot, filepath.Dir(gitExe), "compile", "--plan", "Demo", "--json"); code == 2 {
			t.Fatalf("control with git exited 2 (operational):\n%s", out)
		}

		root := retireCompileOperationalFixture(t)
		graphPath := filepath.Join(root, "Plans", "Demo", "Demo-Graph.json")
		if out, errb, err := runSdd(bin, root, "graph", "propose", "--plan", "Demo", "--file", "payload.json"); err != nil {
			t.Fatalf("setup propose: %v\nstdout: %s\nstderr: %s", err, out, errb)
		}

		before, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatal(err)
		}

		installNShotGitShim(t, 3)
		shimPath := os.Getenv("PATH")
		code, out := run(root, shimPath, "compile", "--plan", "Demo", "--json")
		if code != 2 {
			t.Fatalf("without a working git: exit %d, want 2 (operational)\n%s", code, out)
		}
		low := strings.ToLower(out)
		if !strings.Contains(low, "git") || !strings.Contains(low, "could not") {
			t.Errorf("the operational exit must name the cause; got:\n%s", out)
		}

		after, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("an operational retirement-source verification must not let compile write; graph changed:\nbefore:\n%s\nafter:\n%s",
				before, after)
		}
	})
}

// splitAmendAuditOperationalFixture builds a git-backed planning root whose
// graph carries a retirement source pinned to a real historical commit (so
// VerifyRetirementSource's detection succeeds and only the RevisionExists
// probe is left for the n-shot git shim to fail operationally) alongside a
// live "big"/"helper"/"feature-gate" shape mirroring
// internal/graph/ops/amend_test.go's fixtureRoot: enough live structure that
// `graph split` has a node to retire and `graph amend` has an admissible
// review gate to revise. `graph audit` needs neither and reads the same
// graph read-only.
func splitAmendAuditOperationalFixture(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		argv := append([]string{"-C", dir, "-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid"}, args...)
		out, err := exec.Command(gitExe, argv...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("planning-config.json", `{"planningRoot":"."}`)
	write("Specs/Sample/README.md",
		"---\ntitle: \"Sample Spec\"\ntype: spec\nstatus: approved\ncreated: 2026-08-01\nupdated: 2026-08-01\n"+
			"tags: [spec]\nrelated: []\n---\n\n# Sample Spec\n\n## Acceptance Criteria\n\n- [ ] **AC-01**: The API answers.\n")
	write("Plans/SamplePlan/README.md",
		"---\ntitle: \"Sample Plan\"\ntype: plan\nstatus: draft\ncreated: 2026-08-01\nupdated: 2026-08-01\n"+
			"tags: []\nrelated: [Specs/Sample]\nphases: []\n---\n\n# Sample Plan\n")
	write("old.md", "---\ntasks:\n  - id: \"1.1\"\n---\n")

	git(root, "init", "-q", "-b", "main")
	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "base")
	rev := git(root, "rev-parse", "HEAD")

	graph := `{"version":1,"nodes":[` +
		`{"id":"big","contract":"does too much","justifies":["AC-01"],` +
		`"gate":{"type":"tests","tests":[{"id":"test_big","file":"t.ext","satisfies":["external-format"]}]},` +
		`"hazards":["external-format"],"estimate":3,"phase":"01-core","deps":["helper"]},` +
		`{"id":"helper","contract":"helps","justifies":["AC-01"],` +
		`"gate":{"type":"tests","tests":[{"id":"test_helper","file":"t.ext"}]},"hazards":[],"estimate":1},` +
		`{"id":"feature-gate","contract":"survives review","justifies":["AC-01"],` +
		`"gate":{"type":"review"},"hazards":[],"estimate":1,"deps":["big"]}` +
		`],"retired":["old"],"retirement_sources":{"old":{"source":{"vcs":"git","revision":"` + rev + `","path":"old.md","source_id":"1.1"}}}}`
	write("Plans/SamplePlan/SamplePlan-Graph.json", graph)

	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "graph")
	return root
}

// TestSplitAmendAuditOperationalSourceExit (review-execution F-01): the
// shared graph validation behind `graph split`, `graph amend`, and
// `graph audit` (compile.Sources.Validate, called from ops.splitWith,
// ops.AmendFromReview, and compile.Audit) discards VerifyRetirementSource's
// operational error today (anchor.go:97's `findings, _ :=
// semanticFindings(...)`), so an unanswered historical-commit probe is
// folded into an ordinary (or absent) finding rather than surfaced as
// operational. This test documents that defect red: once Validate
// propagates the error and every caller checks it, split and amend must
// exit 2 before their before/after comparison runs (and before any write),
// and audit's report must name the operational failure instead of
// presenting OK/clean.
func TestSplitAmendAuditOperationalSourceExit(t *testing.T) {
	bin := stressBinary(t)
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}

	base := []string{"SDD_VCS_DISABLE_P4=1", "HOME=" + t.TempDir(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	run := func(dir, path string, args ...string) (int, string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append(append([]string{}, base...), "PATH="+path)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running sdd %s: %v", strings.Join(args, " "), err)
		}
		return code, string(out)
	}

	t.Run("split", func(t *testing.T) {
		root := splitAmendAuditOperationalFixture(t)
		graphPath := filepath.Join(root, "Plans", "SamplePlan", "SamplePlan-Graph.json")
		payload := filepath.Join(root, "children.json")
		if err := os.WriteFile(payload, []byte(`{
  "version": 1,
  "nodes": [
    {"id": "big-parse", "contract": "parses the input", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "test_parse", "file": "t.ext", "satisfies": ["external-format"]}]},
     "hazards": ["external-format"]},
    {"id": "big-render", "contract": "renders the output", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "test_render", "file": "t.ext"}]},
     "hazards": []}
  ]
}
`), 0o644); err != nil {
			t.Fatal(err)
		}
		args := []string{"graph", "split", "--plan", "SamplePlan", "--node", "big", "--file", payload}

		// Control: git available. The split may succeed or refuse, but it
		// must never be operational (exit 2) — establishing the fixture
		// itself is not the cause.
		if code, out := run(root, filepath.Dir(gitExe), args...); code == 2 {
			t.Fatalf("control with git exited 2 (operational):\n%s", out)
		}

		// Fresh fixture for the shimmed run: the control invocation above
		// may have mutated the graph (retired "big"), and before/after must
		// be compared against the copy the shimmed run actually reads.
		root = splitAmendAuditOperationalFixture(t)
		graphPath = filepath.Join(root, "Plans", "SamplePlan", "SamplePlan-Graph.json")
		before, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatal(err)
		}

		installNShotGitShim(t, 3)
		shimPath := os.Getenv("PATH")
		code, out := run(root, shimPath, args...)
		if code != 2 {
			t.Fatalf("without a working git: exit %d, want 2 (operational)\n%s", code, out)
		}
		low := strings.ToLower(out)
		if !strings.Contains(low, "git") || !strings.Contains(low, "could not") {
			t.Errorf("the operational exit must name the cause; got:\n%s", out)
		}
		after, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("an operational retirement-source verification must not let split write; graph changed:\nbefore:\n%s\nafter:\n%s",
				before, after)
		}
	})

	t.Run("amend", func(t *testing.T) {
		root := splitAmendAuditOperationalFixture(t)
		graphPath := filepath.Join(root, "Plans", "SamplePlan", "SamplePlan-Graph.json")

		var b strings.Builder
		b.WriteString("---\ntitle: \"Gate review\"\ntype: review\nstatus: resolved\nreview_of: \"Plans/SamplePlan/README.md\"\n")
		b.WriteString("frozen: true\nverdict: Amend\nreview_mode: single-agent\nlane_results:\n")
		for _, lane := range model.ReviewLanes {
			b.WriteString("  - lane: " + lane + "\n    result: PASS/Aligned\n    evidence: \"looked\"\n")
		}
		b.WriteString("findings:\n  - id: F-01\n    severity: major\n    title: \"big ignores archived rows\"\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: \"does too much, and excludes archived rows\"\n")
		b.WriteString("---\n\n# Gate review\n\nBody.\n")
		reviewRel := "Plans/SamplePlan/reviews/01-sample-review.md"
		reviewPath := filepath.Join(root, filepath.FromSlash(reviewRel))
		if err := os.MkdirAll(filepath.Dir(reviewPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(reviewPath, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		args := []string{"graph", "amend", "--plan", "SamplePlan", "--node", "feature-gate",
			"--from-review", reviewRel, "--dry-run"}

		// Control: git available, --dry-run so it never mutates the fixture.
		// The amend may succeed (print a preview) or refuse, but it must
		// never be operational (exit 2) — establishing the fixture itself
		// is not the cause.
		if code, out := run(root, filepath.Dir(gitExe), args...); code == 2 {
			t.Fatalf("control with git exited 2 (operational):\n%s", out)
		}
		before, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatal(err)
		}

		installNShotGitShim(t, 3)
		shimPath := os.Getenv("PATH")
		code, out := run(root, shimPath, args...)
		if code != 2 {
			t.Fatalf("without a working git: exit %d, want 2 (operational)\n%s", code, out)
		}
		low := strings.ToLower(out)
		if !strings.Contains(low, "git") || !strings.Contains(low, "could not") {
			t.Errorf("the operational exit must name the cause; got:\n%s", out)
		}
		after, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("an operational retirement-source verification must not let amend write; graph changed:\nbefore:\n%s\nafter:\n%s",
				before, after)
		}
	})

	t.Run("audit", func(t *testing.T) {
		root := splitAmendAuditOperationalFixture(t)
		args := []string{"graph", "audit", "--plan", "SamplePlan", "--json"}

		// Control: git available. Audit may report OK or not, but it must
		// never be operational (exit 2) — establishing the fixture itself
		// is not the cause.
		if code, out := run(root, filepath.Dir(gitExe), args...); code == 2 {
			t.Fatalf("control with git exited 2 (operational):\n%s", out)
		}

		installNShotGitShim(t, 3)
		shimPath := os.Getenv("PATH")
		code, out := run(root, shimPath, args...)
		if code != 2 {
			t.Fatalf("without a working git: exit %d, want 2 (operational)\n%s", code, out)
		}
		low := strings.ToLower(out)
		if !strings.Contains(low, "git") || !strings.Contains(low, "could not") {
			t.Errorf("the operational exit must name the cause; got:\n%s", out)
		}
		if strings.Contains(out, `"ok": true`) || strings.Contains(out, `"ok":true`) {
			t.Errorf("an operational failure must not be reported as an OK audit result:\n%s", out)
		}
	})
}
