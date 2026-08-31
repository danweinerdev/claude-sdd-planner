// Package store owns the plan graph's presence on disk: discovery, loading,
// atomic saving, and the read-modify-write cycle every mutation flows
// through (Designs/SddGraph DD-3, DD-10).
//
// It deliberately reuses internal/store's write machinery rather than
// growing a second implementation: WriteAtomicExpecting already spans the
// sidecar advisory lock, the digest re-check, the temp write, and the rename
// in one compare-and-swap, on both Windows and POSIX. Update builds the
// graph's RMW discipline on that contract — read (capture digest), mutate,
// CAS, and on ErrConcurrentWrite re-read and re-apply — which is what makes
// claim recording safe under parallel agents: a competing claim changes the
// digest, forcing the loser back to a fresh read that sees it (DD-10:
// double-claim prevention lives in the store, not in dispatch discipline).
//
// Nothing here interprets the graph. States are derived elsewhere and never
// stored; this package moves validated bytes.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// GraphDirName is the per-plan workspace area: fragments, per-claim working
// files, logs. Ignored at init — it is working state, never record.
const GraphDirName = ".graph"

// updateAttempts bounds the CAS retry loop. Contention on a planning root is
// a handful of agents, not a thundering herd; failing after this many
// collisions indicates a livelock bug or a pathological writer, and an error
// names it rather than spinning forever.
const updateAttempts = 16

// PathFor returns the graph path for a plan directory:
// <planDir>/<PlanName>-Graph.json, the committed master graph.
func PathFor(planDir string) string {
	return filepath.Join(planDir, filepath.Base(planDir)+"-Graph.json")
}

// Find walks upward from start looking for a directory whose
// <base>-Graph.json exists, and returns that graph path. It is the "which
// graph governs here?" question for a session working somewhere inside a
// plan directory.
func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := PathFor(dir)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no <Plan>-Graph.json found at or above %s; run `sdd graph init --plan <name>` to create one", start)
		}
		dir = parent
	}
}

// Load reads and strictly decodes the graph at path. Reads go through
// internal/store.Read, whose shared lock keeps a concurrent writer's rename
// from landing mid-read — bare os.ReadFile races the rename on Windows and
// surfaces as a transient sharing violation.
func Load(path string) (*model.Graph, error) {
	art, err := istore.Read(path)
	if err != nil {
		return nil, fmt.Errorf("reading graph: %w", err)
	}
	if !art.Exists {
		return nil, fmt.Errorf("reading graph %s: file does not exist", path)
	}
	g, err := model.DecodeGraph([]byte(art.Source))
	if err != nil {
		return nil, fmt.Errorf("graph %s is not valid:\n%w", art.Path, err)
	}
	return g, nil
}

// Save encodes and atomically writes the graph, unconditionally. Use Update
// for read-modify-write cycles; Save is for writers that own the whole
// content (init, compile's full rewrite).
func Save(path string, g *model.Graph) error {
	out, err := g.Encode()
	if err != nil {
		return err
	}
	return istore.WriteAtomic(path, string(out))
}

// Update runs one read-modify-write cycle against the graph: load, apply fn,
// and write back only if no other writer landed in between (digest
// compare-and-swap). On contention it re-reads and re-applies fn against the
// fresh state, so fn must be idempotent-by-derivation: compute the mutation
// from the graph it is handed, never from state captured outside the call.
// The final, written graph is returned.
func Update(path string, fn func(*model.Graph) error) (*model.Graph, error) {
	for attempt := 0; attempt < updateAttempts; attempt++ {
		art, err := istore.Read(path)
		if err != nil {
			return nil, fmt.Errorf("reading graph: %w", err)
		}
		if !art.Exists {
			return nil, fmt.Errorf("reading graph %s: file does not exist", path)
		}
		digest := art.Digest
		g, err := model.DecodeGraph([]byte(art.Source))
		if err != nil {
			return nil, fmt.Errorf("graph %s is not valid:\n%w", art.Path, err)
		}
		if err := fn(g); err != nil {
			return nil, err
		}
		out, err := g.Encode()
		if err != nil {
			return nil, err
		}
		writeErr := istore.WriteAtomicExpecting(path, string(out), digest)
		if writeErr == nil {
			return g, nil
		}
		var concurrent *istore.ErrConcurrentWrite
		if !errors.As(writeErr, &concurrent) {
			return nil, writeErr
		}
		// Another writer landed first; their claim/observation is now part
		// of the state fn must be re-derived from.
	}
	return nil, fmt.Errorf("updating graph %s: gave up after %d concurrent-write collisions", path, updateAttempts)
}

// ignoreLines is what Init guarantees a plan's .gitignore covers: the
// advisory-lock sidecars (never committed — a leftover lock is machine
// state, not record) and the .graph/ workspace area (fragments, per-claim
// files, logs — working state by design, DD-3).
var ignoreLines = []string{"*.sdd-lock", "*.lock", GraphDirName + "/"}

// Init creates an empty v1 graph for the plan directory, plus the
// .gitignore entries that keep lock sidecars and the workspace area out of
// version control. It refuses if the graph already exists — re-initializing
// a live graph would be data loss, and there is nothing idempotent about it.
func Init(planDir string) (string, error) {
	info, err := os.Stat(planDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("graph init: %s is not a plan directory", planDir)
	}
	path := PathFor(planDir)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("graph init: %s already exists; a live graph is never re-initialized", path)
	}
	if err := ensureIgnores(filepath.Join(planDir, ".gitignore")); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(planDir, GraphDirName), 0o755); err != nil {
		return "", err
	}
	if err := Save(path, &model.Graph{Version: model.SchemaVersion}); err != nil {
		return "", err
	}
	return path, nil
}

// ensureIgnores merges the required ignore lines into an existing .gitignore
// (or creates it), preserving whatever else it carries.
func ensureIgnores(path string) error {
	existing := ""
	if b, err := os.ReadFile(path); err == nil {
		existing = string(b)
	} else if !os.IsNotExist(err) {
		return err
	}
	present := map[string]bool{}
	for _, line := range strings.Split(existing, "\n") {
		present[strings.TrimSpace(line)] = true
	}
	var add []string
	for _, line := range ignoreLines {
		if !present[line] {
			add = append(add, line)
		}
	}
	if len(add) == 0 {
		return nil
	}
	out := existing
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += strings.Join(add, "\n") + "\n"
	return istore.WriteAtomic(path, out)
}
