// Multi-process concurrency stress (Plans/SddGraph task 3.6): every
// guarantee the graph packages prove in-process must survive N REAL sdd
// processes racing on one graph file. These tests build the actual binary
// and drive it the way concurrent agents would: racing `next --claim`,
// hammering reads, and running `graph gc` in the middle of it all.
//
// What in-process tests cannot catch and these can: torn cross-process
// reads, double-claims that slip past the advisory lock + CAS pairing, and
// gc reaping an allocation in flight (the race that forced claims to
// confirm-then-allocate).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

var (
	stressBinOnce sync.Once
	stressBinPath string
	stressBinErr  error
)

// stressBinary builds the sdd binary once per test run. The stress tests
// exercise PROCESSES, not packages: in-process claim calls would share one
// lock table and prove nothing about cross-process safety.
func stressBinary(t *testing.T) string {
	t.Helper()
	stressBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "sdd-stress-bin-")
		if err != nil {
			stressBinErr = err
			return
		}
		name := "sdd-stress"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		stressBinPath = filepath.Join(dir, name)
		repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			stressBinErr = err
			return
		}
		cmd := exec.Command("go", "build", "-o", stressBinPath, "./cmd/sdd")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			stressBinErr = fmt.Errorf("building sdd: %v: %s", err, out)
		}
	})
	if stressBinErr != nil {
		t.Fatalf("stress binary: %v", stressBinErr)
	}
	return stressBinPath
}

// runSdd executes one binary invocation in dir and returns combined stdout,
// stderr, and the raw error (nil on exit 0).
func runSdd(bin, dir string, args ...string) (string, string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

func gitStress(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// stressFixture builds a real git repo with a compiled graph of nodeCount
// independent READY nodes, driving setup through the binary itself.
func stressFixture(t *testing.T, nodeCount int) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	bin := stressBinary(t)
	root := t.TempDir()
	gitStress(t, root, "init", "-q")
	gitStress(t, root, "config", "user.email", "t@example.com")
	gitStress(t, root, "config", "user.name", "t")

	writeStress := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeStress("planning-config.json", `{"planningRoot": "."}`)
	writeStress("Specs/Sample/README.md", `---
title: "S"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# S

## Acceptance Criteria

- [ ] **AC-01**: The API answers.
`)
	writeStress("Plans/Demo/README.md", `---
title: "Demo"
type: plan
status: draft
created: 2026-08-01
updated: 2026-08-01
tags: []
related: [Specs/Sample]
phases: []
---

# Demo
`)

	var nodes, workIDs []string
	for i := 0; i < nodeCount; i++ {
		nodes = append(nodes, fmt.Sprintf(`{"id": "n%d", "contract": "does thing %d", "justifies": ["AC-01"], "gate": {"type": "tests", "tests": [{"id": "test_n%d", "file": "t.ext", "satisfies": []}]}, "hazards": [], "artifacts": ["src/n%d.ext"]}`, i, i, i, i))
		workIDs = append(workIDs, fmt.Sprintf("%q", fmt.Sprintf("n%d", i)))
	}
	// The terminal full review gate: compile refuses a graph whose nodes
	// have no completion-grade closure (DD-9's backstop).
	nodes = append(nodes, fmt.Sprintf(`{"id": "final-gate", "contract": "survives full review", "justifies": ["AC-01"], "deps": [%s], "gate": {"type": "review", "lanes": "full"}, "hazards": []}`, strings.Join(workIDs, ", ")))
	writeStress("payload.json", fmt.Sprintf(`{"version": 1, "nodes": [%s]}`, strings.Join(nodes, ", ")))

	for _, args := range [][]string{
		{"graph", "init", "--plan", "Demo"},
		{"graph", "propose", "--plan", "Demo", "--file", "payload.json"},
		{"compile", "--plan", "Demo"},
	} {
		if out, errb, err := runSdd(bin, root, args...); err != nil {
			t.Fatalf("setup %v: %v\nstdout: %s\nstderr: %s", args, err, out, errb)
		}
	}
	gitStress(t, root, "add", "-A")
	gitStress(t, root, "commit", "-q", "-m", "base")
	return root
}

// TestGraphConcurrentClaimStress races claim workers, a reader, and gc as
// separate processes. Invariants: no node is ever claimed twice, the final
// graph matches exactly the wins the workers report, every surviving
// claim's workspace exists (gc never reaped a live allocation), and no
// reader ever observes a torn graph.
func TestGraphConcurrentClaimStress(t *testing.T) {
	if testing.Short() {
		t.Skip("multi-process stress is not a -short test")
	}
	const nodeCount, workers, capacity = 10, 4, 8
	root := stressFixture(t, nodeCount)
	bin := stressBinary(t)

	type win struct{ worker, node string }
	winCh := make(chan win, nodeCount*2)
	errCh := make(chan error, workers+2)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker string) {
			defer wg.Done()
			for attempt := 0; attempt < 30; attempt++ {
				out, errb, err := runSdd(bin, root, "next", "Plans/Demo", "--claim", "--by", worker, "--json")
				if err == nil {
					var res struct {
						OK   bool `json:"ok"`
						Node struct {
							ID string `json:"id"`
						} `json:"node"`
					}
					if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil || !res.OK || res.Node.ID == "" {
						errCh <- fmt.Errorf("%s: claim success with unparseable output: %v\n%s", worker, jsonErr, out)
						return
					}
					winCh <- win{worker, res.Node.ID}
					continue
				}
				if strings.Contains(errb, "nothing claimable") {
					return // frontier drained or capacity reached: done
				}
				// Contended CAS ("retry") or a lost allocation race
				// ("claim rolled back") are transient under stress.
				if strings.Contains(errb, "retry") || strings.Contains(errb, "claim rolled back") {
					time.Sleep(20 * time.Millisecond)
					continue
				}
				errCh <- fmt.Errorf("%s: unexpected claim refusal: %v\nstderr: %s", worker, err, errb)
				return
			}
			errCh <- fmt.Errorf("worker retry budget exhausted without a terminal refusal")
		}(fmt.Sprintf("w%d", w))
	}

	// Reader: a concurrent writer must never be observable as a torn or
	// unparseable graph.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			out, errb, err := runSdd(bin, root, "next", "Plans/Demo", "--json")
			if err != nil {
				errCh <- fmt.Errorf("reader: %v\nstderr: %s", err, errb)
				return
			}
			if !json.Valid([]byte(out)) {
				errCh <- fmt.Errorf("reader: invalid JSON (torn read?): %s", out)
				return
			}
		}
	}()

	// gc racing live claims: with confirm-then-allocate it must never
	// reap an allocation in flight.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 4; i++ {
			if _, errb, err := runSdd(bin, root, "graph", "gc", "--plan", "Demo"); err != nil {
				errCh <- fmt.Errorf("gc: %v\nstderr: %s", err, errb)
				return
			}
			time.Sleep(30 * time.Millisecond)
		}
	}()

	wg.Wait()
	close(winCh)
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	wonBy := map[string]string{}
	for w := range winCh {
		if prior, dup := wonBy[w.node]; dup {
			t.Fatalf("DOUBLE CLAIM: %s won by both %s and %s", w.node, prior, w.worker)
		}
		wonBy[w.node] = w.worker
	}
	if len(wonBy) != capacity {
		t.Errorf("expected the frontier to fill provider capacity (%d claims), got %d: %v", capacity, len(wonBy), wonBy)
	}

	g, err := gstore.Load(gstore.PathFor(filepath.Join(root, "Plans", "Demo")))
	if err != nil {
		t.Fatalf("final graph must parse: %v", err)
	}
	claimed := map[string]string{}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Claim == nil {
			continue
		}
		claimed[n.ID] = n.Claim.By
		if n.Claim.Workspace == "" {
			t.Errorf("claim on %s carries no workspace handle", n.ID)
			continue
		}
		wsDir := filepath.Join(root, filepath.FromSlash(n.Claim.Workspace))
		if _, err := os.Stat(wsDir); err != nil {
			t.Errorf("claim on %s names workspace %s which does not exist (gc reaped a live allocation?)", n.ID, n.Claim.Workspace)
		}
	}
	for node, worker := range wonBy {
		if claimed[node] != worker {
			t.Errorf("%s: worker %s reported the win but the graph says %q", node, worker, claimed[node])
		}
	}
	for node, by := range claimed {
		if wonBy[node] == "" {
			t.Errorf("%s: graph carries a claim by %s that no worker reported winning", node, by)
		}
	}
}

// TestGraphLeaseTakeoverAcrossProcesses walks the crash story end to end
// through the binary: the claimant dies (lease lapses), the takeover
// attempt refuses on the post-mortem workspace, gc expires the claim and
// reaps it, the successor claims cleanly, and the dead claimant's late sync
// is refused by claim discipline.
func TestGraphLeaseTakeoverAcrossProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("multi-process stress is not a -short test")
	}
	root := stressFixture(t, 1)
	bin := stressBinary(t)
	planDir := filepath.Join(root, "Plans", "Demo")

	if out, errb, err := runSdd(bin, root, "next", "Plans/Demo", "--claim", "--by", "w1"); err != nil {
		t.Fatalf("w1 claim: %v\n%s\n%s", err, out, errb)
	}

	// The claimant crashes: its lease lapses without a release.
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("n0").Claim.LeaseExpires = "2001-01-01T00:00:00Z"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Takeover before gc: the expired claim is cleared, but the leftover
	// workspace is post-mortem evidence — allocation refuses loudly and
	// the takeover claim rolls back.
	_, errb, err := runSdd(bin, root, "next", "Plans/Demo", "--claim", "--by", "w2")
	if err == nil || !strings.Contains(errb, "already exists") {
		t.Fatalf("takeover over a leftover workspace must refuse naming it: %v\nstderr: %s", err, errb)
	}

	// gc: the crash story's cleanup half.
	if out, errb, err := runSdd(bin, root, "graph", "gc", "--plan", "Demo"); err != nil {
		t.Fatalf("gc: %v\n%s\n%s", err, out, errb)
	}
	if _, err := os.Stat(filepath.Join(planDir, gstore.GraphDirName, "ws-n0")); !os.IsNotExist(err) {
		t.Fatal("gc must reap the dead claimant's workspace")
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if g.NodeByID("n0").Claim != nil {
		t.Fatal("the lapsed claim must be gone after gc")
	}

	// The successor claims cleanly: fresh worktree, fresh branch (the
	// dead claim's surviving branch must not collide).
	if out, errb, err := runSdd(bin, root, "next", "Plans/Demo", "--claim", "--by", "w2"); err != nil {
		t.Fatalf("post-gc reclaim: %v\n%s\n%s", err, out, errb)
	}

	// The dead claimant reappears with a late report: claim discipline
	// refuses it — w2 holds the node now.
	if err := os.WriteFile(filepath.Join(root, "red.xml"),
		[]byte(`<testsuite><testcase name="test_n0"><failure/></testcase></testsuite>`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errb, err = runSdd(bin, root, "graph", "sync", "--plan", "Demo", "--node", "n0", "--by", "w1", "--report", "red.xml")
	if err == nil || !strings.Contains(errb, "claimed by") {
		t.Fatalf("the dead claimant's late sync must be refused by claim discipline: %v\nstderr: %s", err, errb)
	}
}
