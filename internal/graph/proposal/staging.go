package proposal

// Fragment staging and assembly (Designs/SddGraph DD-11): construction is
// declarative and batched. A session authors a payload file, `Stage`
// validates it wholesale and parks it under the plan's gitignored workspace,
// and `Assemble` merges every staged fragment into one proposal set for
// compile. Parallel decomposition is N sessions staging N fragments; the
// merge is where their node ids meet.
//
// Refusals are atomic: nothing is staged when validation fails, and a
// collision leaves every fragment untouched — the fix is an edit to a
// fragment file, never a cleanup of half-consumed state.

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// FragmentsDirName is where staged fragments live, inside the plan's
// gitignored .graph/ workspace.
const FragmentsDirName = "fragments"

// AssembledName is the merged proposal Assemble writes, consumed by compile.
const AssembledName = "proposal.json"

// FragmentsDir returns the staging directory for a plan directory.
func FragmentsDir(planDir string) string {
	return filepath.Join(planDir, gstore.GraphDirName, FragmentsDirName)
}

// AssembledPath returns the merged-proposal path for a plan directory.
func AssembledPath(planDir string) string {
	return filepath.Join(planDir, gstore.GraphDirName, AssembledName)
}

// Stage validates a payload wholesale and parks it as a fragment. Refused
// payloads stage nothing. The plan's graph must already exist: a proposal is
// a proposal AGAINST a graph, and the master node-id check below needs one
// to check against.
func Stage(planDir string, payload []byte) (string, error) {
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		return "", fmt.Errorf("graph propose: %w (run `sdd graph init --plan <name>` first)", err)
	}
	p, err := model.DecodeProposal(payload)
	if err != nil {
		return "", fmt.Errorf("graph propose: payload refused, nothing staged:\n%w", err)
	}
	// A proposal introduces nodes; it never redefines one. Mutating an
	// existing node goes through the locked single-node verbs (DD-11), not
	// through a payload that would silently replace committed structure.
	existing := map[string]bool{}
	for _, id := range g.NodeIDs() {
		existing[id] = true
	}
	for _, n := range p.Nodes {
		if existing[n.ID] {
			return "", fmt.Errorf("graph propose: node %q already exists in the graph; proposals introduce nodes, mutations go through `sdd graph` verbs", n.ID)
		}
	}
	dir := FragmentsDir(planDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, newFragmentID()+".json")
	if err := istore.WriteAtomic(path, string(payload)); err != nil {
		return "", err
	}
	return path, nil
}

// Assemble merges every staged fragment into one proposal set, in staging
// order (fragment ids are time-ordered, so lexical order is arrival order).
// A node id declared by two fragments is a collision naming both files; any
// refusal leaves every fragment untouched. On success the merged proposal is
// written to AssembledPath and the consumed fragments are removed.
func Assemble(planDir string) (string, *model.Proposal, error) {
	dir := FragmentsDir(planDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("graph assemble: no staged fragments under %s; stage one with `sdd graph propose`", dir)
		}
		return "", nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return "", nil, fmt.Errorf("graph assemble: no staged fragments under %s; stage one with `sdd graph propose`", dir)
	}
	sort.Strings(names)

	merged := &model.Proposal{Version: model.SchemaVersion}
	declaredIn := map[string]string{}
	for _, name := range names {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", nil, err
		}
		frag, err := model.DecodeProposal(raw)
		if err != nil {
			return "", nil, fmt.Errorf("graph assemble: fragment %s is not valid, nothing merged:\n%w", name, err)
		}
		for _, n := range frag.Nodes {
			if prev, dup := declaredIn[n.ID]; dup {
				return "", nil, fmt.Errorf("graph assemble: node %q is declared in both %s and %s; nothing merged — remove one declaration", n.ID, prev, name)
			}
			declaredIn[n.ID] = name
			merged.Nodes = append(merged.Nodes, n)
		}
	}

	out, err := encodeProposal(merged)
	if err != nil {
		return "", nil, err
	}
	assembled := AssembledPath(planDir)
	if err := istore.WriteAtomic(assembled, string(out)); err != nil {
		return "", nil, err
	}
	// Consumed only after the merged set is durably written.
	for _, name := range names {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return "", nil, fmt.Errorf("graph assemble: merged set written but fragment %s could not be consumed: %w", name, err)
		}
	}
	return assembled, merged, nil
}

// Encode renders a proposal with the same conventions as the graph:
// two-space indent, LF, trailing newline, deterministic order. Exported for
// producers that build proposals programmatically (`sdd graph convert`).
func Encode(p *model.Proposal) ([]byte, error) {
	out := *p
	if out.Nodes == nil {
		out.Nodes = []model.Node{}
	}
	return encodeJSON(&out)
}

// encodeProposal is the internal spelling Assemble uses.
func encodeProposal(p *model.Proposal) ([]byte, error) { return Encode(p) }

// newFragmentID returns a UUIDv7: time-ordered, so staging order and lexical
// order agree and assembly is deterministic without a manifest.
func newFragmentID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	binary.BigEndian.PutUint64(b[:8], ms<<16)
	if _, err := rand.Read(b[6:]); err != nil {
		// crypto/rand failing is a broken platform; a timestamp-only id
		// still stages correctly, it just loses collision headroom within
		// one millisecond.
		binary.BigEndian.PutUint64(b[8:], uint64(time.Now().UnixNano()))
	}
	b[6] = 0x70 | (b[6] & 0x0f) // version 7
	b[8] = 0x80 | (b[8] & 0x3f) // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
