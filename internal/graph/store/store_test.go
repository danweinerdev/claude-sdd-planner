package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func planDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Plans", "SamplePlan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInitAndPathFor(t *testing.T) {
	dir := planDir(t)
	path, err := Init(dir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if filepath.Base(path) != "SamplePlan-Graph.json" {
		t.Fatalf("graph name must derive from the plan directory, got %s", path)
	}
	g, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if g.Version != model.SchemaVersion || g.SeqCounter != 0 || len(g.Nodes) != 0 {
		t.Fatalf("init must write an empty v1 graph, got %+v", g)
	}

	ignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("init must write the plan .gitignore: %v", err)
	}
	for _, line := range []string{"*.sdd-lock", "*.lock", ".graph/"} {
		if !strings.Contains(string(ignore), line+"\n") {
			t.Fatalf(".gitignore must cover %q; got:\n%s", line, ignore)
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, GraphDirName)); err != nil || !fi.IsDir() {
		t.Fatalf("init must create the %s workspace dir: %v", GraphDirName, err)
	}

	if _, err := Init(dir); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("re-init must refuse: %v", err)
	}
}

func TestInitPreservesExistingIgnores(t *testing.T) {
	dir := planDir(t)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("notes/scratch/\n*.lock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	s := string(got)
	if !strings.Contains(s, "notes/scratch/\n") {
		t.Fatalf("existing ignore lines must be preserved; got:\n%s", s)
	}
	if strings.Count(s, "*.lock\n") != 1 {
		t.Fatalf("already-present lines must not duplicate; got:\n%s", s)
	}
	if !strings.Contains(s, "*.sdd-lock\n") || !strings.Contains(s, ".graph/\n") {
		t.Fatalf("missing required lines; got:\n%s", s)
	}
}

func TestFindWalksUpward(t *testing.T) {
	dir := planDir(t)
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, GraphDirName, "ws-0192", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := Find(nested)
	if err != nil {
		t.Fatalf("find from nested dir: %v", err)
	}
	if filepath.Base(found) != "SamplePlan-Graph.json" {
		t.Fatalf("found the wrong graph: %s", found)
	}

	_, err = Find(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "sdd graph init") {
		t.Fatalf("a miss must error helpfully, naming the init verb: %v", err)
	}
}

func TestSaveLoadRoundTripIsDeterministic(t *testing.T) {
	dir := planDir(t)
	path, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Load(path)
	if err != nil {
		t.Fatalf("load fresh graph: %v", err)
	}
	g.Nodes = append(g.Nodes, model.Node{
		ID: "a", Contract: "does a", Gate: model.Gate{Type: model.GateTests},
		Hazards: model.Hazards{}, Estimate: 1,
	})
	if err := Save(path, g); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, reloaded); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatalf("load->save must be byte-identical (the graph is committed and diffed):\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if strings.Contains(string(first), "\r") {
		t.Fatal("graph bytes must be LF-only")
	}
}

// TestUpdateLosesNoIncrement is the DD-10 contention property at the store
// level: N concurrent read-modify-write cycles land N times, never fewer —
// a lost update here would be a double-claim in phase 3.
func TestUpdateLosesNoIncrement(t *testing.T) {
	dir := planDir(t)
	path, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	const writers, perWriter = 8, 5
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if _, err := Update(path, func(g *model.Graph) error {
					g.SeqCounter++
					return nil
				}); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("update: %v", err)
	}
	g, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if g.SeqCounter != writers*perWriter {
		t.Fatalf("lost updates: seq_counter = %d, want %d", g.SeqCounter, writers*perWriter)
	}
}

// TestReadersNeverSeeATornGraph pounds reads during a write storm: every
// read must parse — the atomic rename means a reader sees a complete old or
// complete new graph, never bytes in between.
func TestReadersNeverSeeATornGraph(t *testing.T) {
	dir := planDir(t)
	path, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var writerErr error
	go func() {
		defer close(done)
		for i := 0; i < 40; i++ {
			if _, err := Update(path, func(g *model.Graph) error {
				g.SeqCounter++
				return nil
			}); err != nil {
				writerErr = err
				return
			}
		}
	}()
	for {
		select {
		case <-done:
			if writerErr != nil {
				t.Fatalf("writer: %v", writerErr)
			}
			return
		default:
			if _, err := Load(path); err != nil {
				t.Fatalf("reader saw a torn or invalid graph: %v", err)
			}
		}
	}
}

func TestUpdateSurfacesFnAndDecodeErrors(t *testing.T) {
	dir := planDir(t)
	path, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Update(path, func(*model.Graph) error {
		return os.ErrPermission
	}); err == nil {
		t.Fatal("fn errors must propagate")
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(path, func(*model.Graph) error { return nil }); err == nil ||
		!strings.Contains(err.Error(), "is not valid") {
		t.Fatalf("a corrupt graph must be reported, not overwritten: %v", err)
	}
	if _, err := Update(filepath.Join(dir, "Missing-Graph.json"), func(*model.Graph) error { return nil }); err == nil {
		t.Fatal("a missing graph must be reported")
	}
}
