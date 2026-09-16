package ops

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

func remapGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	argv := append([]string{"-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid", "-c", "core.hooksPath=" + filepath.Join(dir, "no-hooks")}, args...)
	cmd := exec.Command("git", argv...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

type remapFixture struct {
	root, graphPath            string
	base, old, main, rewritten string
	verificationJSON, nodeJSON []byte
}

func newRemapFixture(t *testing.T) remapFixture {
	t.Helper()
	root := t.TempDir()
	remapGit(t, root, "init", "-q", "-b", "main")
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
	write("base.txt", "base\n")
	remapGit(t, root, "add", "base.txt")
	remapGit(t, root, "commit", "-q", "-m", "base")
	base := remapGit(t, root, "rev-parse", "HEAD")
	remapGit(t, root, "checkout", "-q", "-b", "topic")
	write("topic.txt", "topic\n")
	remapGit(t, root, "add", "topic.txt")
	remapGit(t, root, "commit", "-q", "-m", "topic")
	old := remapGit(t, root, "rev-parse", "HEAD")
	remapGit(t, root, "checkout", "-q", "main")
	write("main.txt", "main\n")
	remapGit(t, root, "add", "main.txt")
	remapGit(t, root, "commit", "-q", "-m", "main")
	main := remapGit(t, root, "rev-parse", "HEAD")
	remapGit(t, root, "checkout", "-q", "topic")
	remapGit(t, root, "rebase", "main")
	rewritten := remapGit(t, root, "rev-parse", "HEAD")
	if old == rewritten {
		t.Fatal("fixture did not produce a rewritten commit")
	}

	write("planning-config.json", `{"planningRoot":"."}`)
	write("Plans/Demo/README.md", "---\ntitle: Demo\ntype: plan\nstatus: active\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags: []\nrelated: []\nphases: []\n---\n\n# Demo\n")
	n := model.Node{ID: "work", Contract: "works", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1,
		Claim:        &model.Claim{By: "worker", LeaseExpires: "2099-01-01T00:00:00Z", Workspace: "ws"},
		Verification: &model.Verification{Result: model.ResultPass, Seq: 7, ContractRev: 2, ReportDigest: "sha256:report", Isolation: model.IsolationClean, Provenance: &model.Provenance{Kind: "git", Revision: old, Worktree: "ws"}},
		RedSeqs:      map[string]int{"test_work": 4}, ContractRev: 2}
	g := &model.Graph{Version: model.SchemaVersion, SeqCounter: 9, Nodes: []model.Node{n}, Retired: []string{"retired"}, RetirementSources: map[string]model.RetirementRecord{"retired": {Source: model.RetirementSource{VCS: "git", Revision: base, Path: "old.md", SourceID: "1.1"}}}, Amendments: []model.AmendmentRecord{{Seq: 8, Review: "review", Artifact: "reviews/frozen.md", ReportDigest: "sha256:frozen"}}}
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", "Demo"))
	if err := gstore.Save(graphPath, g); err != nil {
		t.Fatal(err)
	}
	verificationJSON, _ := json.Marshal(g.Nodes[0].Verification)
	nodeJSON, _ := json.Marshal(g.Nodes[0])
	return remapFixture{root: root, graphPath: graphPath, base: base, old: old, main: main, rewritten: rewritten, verificationJSON: verificationJSON, nodeJSON: nodeJSON}
}

func graphDigest(t *testing.T, path string) string {
	t.Helper()
	art, err := istore.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return art.Digest
}

func TestRemapRevisionsRealRebaseDryRunApplyAndIdempotence(t *testing.T) {
	f := newRemapFixture(t)
	before, _ := os.ReadFile(f.graphPath)
	beforeGraph, _ := gstore.Load(f.graphPath)
	mapping := []byte(f.old + " " + f.rewritten + "\n" + f.old + " " + f.rewritten + "\n" + f.base + " " + f.main + "\n")
	dry, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: mapping, DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dry.ExpectDigest != graphDigest(t, f.graphPath) || len(dry.Outcomes) != 2 || dry.Outcomes[0].Status != RemapRecorded || dry.Outcomes[1].Status != RemapUnreferenced {
		t.Fatalf("dry result: %+v", dry)
	}
	if after, _ := os.ReadFile(f.graphPath); string(after) != string(before) {
		t.Fatal("dry run mutated graph")
	}

	res, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: mapping, ExpectDigest: dry.ExpectDigest})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Applied || res.NewDigest == dry.ExpectDigest {
		t.Fatalf("apply result: %+v", res)
	}
	g, err := gstore.Load(f.graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if g.RevisionLineage[f.old] != f.rewritten {
		t.Fatalf("lineage = %+v", g.RevisionLineage)
	}
	if _, stored := g.RevisionLineage[f.base]; stored {
		t.Fatal("unreferenced mapping was stored")
	}
	gotVerification, _ := json.Marshal(g.Nodes[0].Verification)
	gotNode, _ := json.Marshal(g.Nodes[0])
	if string(gotVerification) != string(f.verificationJSON) || string(gotNode) != string(f.nodeJSON) {
		t.Fatalf("remap changed node/proof bytes\nverification: %s\nnode: %s", gotVerification, gotNode)
	}
	if g.SeqCounter != 9 || states.Derive(states.Inputs{Graph: g})["work"].State != states.Green {
		t.Fatalf("remap changed sequence or derived state: seq=%d state=%s", g.SeqCounter, states.Derive(states.Inputs{Graph: g})["work"].State)
	}
	gotLineage := g.RevisionLineage
	g.RevisionLineage = nil
	beforeGraph.RevisionLineage = nil
	if !reflect.DeepEqual(g, beforeGraph) {
		t.Fatalf("remap changed graph fields other than lineage\nbefore: %+v\nafter: %+v", beforeGraph, g)
	}
	g.RevisionLineage = gotLineage

	stable, _ := os.ReadFile(f.graphPath)
	again, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: mapping, ExpectDigest: res.NewDigest})
	if err != nil || again.Applied || again.Outcomes[0].Status != RemapAlreadyRecorded {
		t.Fatalf("idempotent repeat: %+v %v", again, err)
	}
	if after, _ := os.ReadFile(f.graphPath); string(after) != string(stable) {
		t.Fatal("idempotent repeat changed bytes")
	}
	for _, tc := range []struct{ body, want string }{
		{f.old + " " + f.main, "changed binding"},
		{f.base + " " + f.rewritten, "many-to-one"},
		{f.rewritten + " " + f.old, "cycle"},
	} {
		if _, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: []byte(tc.body), DryRun: true}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("existing-lineage %s refusal: %v", tc.want, err)
		}
	}
}

func TestRemapRevisionsMultiStepChainAndNoOpRows(t *testing.T) {
	f := newRemapFixture(t)
	first, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: []byte(f.old + " " + f.rewritten), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: []byte(f.old + " " + f.rewritten), ExpectDigest: first.ExpectDigest}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "later.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	remapGit(t, f.root, "add", "later.txt")
	remapGit(t, f.root, "commit", "-q", "-m", "later rewrite")
	later := remapGit(t, f.root, "rev-parse", "HEAD")
	digest := graphDigest(t, f.graphPath)
	res, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: []byte(f.rewritten + " " + later + "\n" + later + " " + later + "\n"), ExpectDigest: digest})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outcomes) != 2 || res.Outcomes[1].Status != RemapUnchanged {
		t.Fatalf("outcomes: %+v", res.Outcomes)
	}
	g, _ := gstore.Load(f.graphPath)
	if got := g.RevisionChain(f.old); !reflect.DeepEqual(got, []string{f.old, f.rewritten, later}) {
		t.Fatalf("chain = %v", got)
	}
	if _, stored := g.RevisionLineage[later]; stored {
		t.Fatal("self mapping was stored")
	}
}

func TestRemapRevisionsRejectsBadMappingsAndObjects(t *testing.T) {
	f := newRemapFixture(t)
	blobPath := filepath.Join(f.root, "blob.txt")
	if err := os.WriteFile(blobPath, []byte("blob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	blob := remapGit(t, f.root, "hash-object", "-w", "blob.txt")
	missing := strings.Repeat("f", 40)
	cases := []struct{ name, body, want string }{
		{"malformed row", f.old, "exactly two columns"},
		{"short hash", "abc " + f.rewritten, "full 40-character"},
		{"conflicting duplicate", f.old + " " + f.rewritten + "\n" + f.old + " " + f.main, "conflicting mappings"},
		{"fan in", f.old + " " + f.rewritten + "\n" + f.main + " " + f.rewritten, "many-to-one"},
		{"cycle", f.old + " " + f.rewritten + "\n" + f.rewritten + " " + f.old, "cycle"},
		{"missing object", f.old + " " + missing, "not an available commit"},
		{"blob object", f.old + " " + blob, "not an available commit"},
		{"unreferenced", f.base + " " + f.main, "no relevant mappings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: []byte(tc.body), DryRun: true})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRemapRevisionsStaleDigestAndConcurrentWriterRefuse(t *testing.T) {
	f := newRemapFixture(t)
	mapping := []byte(f.old + " " + f.rewritten)
	if _, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: mapping, ExpectDigest: strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("stale digest: %v", err)
	}
	digest := graphDigest(t, f.graphPath)
	_, err := remapRevisionsWithWrite(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: mapping, ExpectDigest: digest}, func(path, content, expect string) error {
		if err := os.WriteFile(path, []byte(content+" "), 0o644); err != nil {
			return err
		}
		return istore.WriteAtomicExpecting(path, content, expect)
	})
	if err == nil || !strings.Contains(err.Error(), "another writer landed") {
		t.Fatalf("concurrency fence: %v", err)
	}
}
