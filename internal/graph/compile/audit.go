package compile

// Formal graph audit (read-only): a single structured report over a committed
// plan graph that reuses the SAME resolver and validation compile enforces —
// coverage by source and identifier family, per-node derived staleness,
// duplicate and cross-node shared tests, and unresolved inputs — so a human or
// a review lane reads one document instead of re-deriving any of it. The only
// thing that sets the exit code is the mandatory compile validation (AC
// coverage, dangling deps, unsourced nodes, …): FR/NFR/DD coverage and the
// other diagnostics are informative, reported alongside, never gating.

import (
	"path/filepath"
	"sort"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// AuditReport is the structured audit result. OK is false when the mandatory
// compile validation produced findings; everything else is informative.
type AuditReport struct {
	OK                bool                              `json:"ok"`
	Plan              string                            `json:"plan"`
	Graph             string                            `json:"graph"`
	Schema            int                               `json:"schema_version"`
	Counts            AuditCounts                       `json:"counts"`
	Coverage          []SourceCoverage                  `json:"coverage,omitempty"`
	Findings          []Finding                         `json:"findings,omitempty"`
	Stale             []StaleNode                       `json:"stale,omitempty"`
	DuplicateTests    []DuplicateTest                   `json:"duplicate_tests,omitempty"`
	SharedTests       []SharedTest                      `json:"shared_tests,omitempty"`
	UnresolvedInputs  []UnresolvedInput                 `json:"unresolved_inputs,omitempty"`
	RetirementSources map[string]model.RetirementRecord `json:"retirement_sources,omitempty"`
}

// AuditCounts is the structural census.
type AuditCounts struct {
	Nodes      int            `json:"nodes"`
	Tests      int            `json:"tests"`
	Gates      map[string]int `json:"gates"`
	Hazards    map[string]int `json:"hazards"`
	Inputs     int            `json:"inputs"`
	Retired    int            `json:"retired"`
	RetiredIDs []string       `json:"retired_ids,omitempty"`
}

// SourceCoverage is one reachable identifier source's coverage, broken down
// by family.
type SourceCoverage struct {
	Source   string           `json:"source"`
	Kind     string           `json:"kind"`
	Families []FamilyCoverage `json:"families"`
}

// FamilyCoverage is one family's defined-vs-covered for a source. Mandatory
// is true only for AC criteria of a directly related spec (the compile exit
// code); every other row is informative.
type FamilyCoverage struct {
	Family    string   `json:"family"`
	Defined   int      `json:"defined"`
	Covered   int      `json:"covered"`
	Uncovered []string `json:"uncovered,omitempty"`
	Mandatory bool     `json:"mandatory,omitempty"`
}

// StaleNode is one node's derived staleness reasons.
type StaleNode struct {
	ID              string   `json:"id"`
	SeqStale        bool     `json:"seq_stale,omitempty"`
	DependencyStale []string `json:"dependency_stale,omitempty"`
	DigestStale     []string `json:"digest_stale,omitempty"`
	IntentStale     []string `json:"intent_stale,omitempty"`
	InputStale      []string `json:"input_stale,omitempty"`
	ReviewStale     []string `json:"review_stale,omitempty"`
	AnchorAdvisory  []string `json:"anchor_advisory,omitempty"`
}

// DuplicateTest is a test id declared twice within one node.
type DuplicateTest struct {
	Node   string `json:"node"`
	TestID string `json:"test_id"`
	File   string `json:"file"`
}

// SharedTest is a test id declared by more than one node (informational:
// shared coverage is legitimate, not a defect).
type SharedTest struct {
	TestID string   `json:"test_id"`
	File   string   `json:"file"`
	Nodes  []string `json:"nodes"`
}

// UnresolvedInput is a declared input that does not resolve.
type UnresolvedInput struct {
	Node  string `json:"node"`
	Input string `json:"input"`
	Error string `json:"error"`
}

// Audit runs the formal audit for one plan.
func Audit(root, repoRoot, plan string) (*AuditReport, error) {
	planDir := filepath.Join(root, "Plans", plan)
	graphPath := gstore.PathFor(planDir)
	g, err := gstore.Load(graphPath)
	if err != nil {
		return nil, err
	}
	sources, err := NewSources(root, repoRoot, plan)
	if err != nil {
		return nil, err
	}

	rep := &AuditReport{
		OK:     true,
		Plan:   plan,
		Graph:  plan + "-Graph.json",
		Schema: g.Version,
		Counts: AuditCounts{Gates: map[string]int{}, Hazards: map[string]int{}},
	}
	rep.Findings = sources.Validate(g)
	if len(rep.Findings) > 0 {
		rep.OK = false
	}

	// Derived states with every axis wired (digest, intent, input).
	snap := sources.IntentSnapshot()
	inRes := sources.InputResolver()
	digester := digest.New(repoRoot)
	st := states.Derive(states.Inputs{
		Graph:               g,
		ArtifactDigest:      digester.Artifact,
		CurrentIntentHashes: snap.Hashes(),
		CurrentInputHashes:  inRes.GraphHashes(g),
	})

	rep.Counts.Nodes = len(g.Nodes)
	rep.Counts.Retired = len(g.Retired)
	rep.Counts.RetiredIDs = append([]string(nil), g.Retired...)
	rep.RetirementSources = g.RetirementSources

	// Per-node census: gate type, hazard triage, tests, inputs, staleness,
	// duplicate and shared tests.
	ownerByTest := map[[2]string][]string{}
	seenTest := map[[2]string]bool{}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		rep.Counts.Gates[n.Gate.Type]++
		switch {
		case n.Hazards == nil:
			rep.Counts.Hazards["untriaged"]++
		default:
			rep.Counts.Hazards["triaged"]++
		}
		rep.Counts.Tests += len(n.Gate.Tests)
		rep.Counts.Inputs += len(n.Inputs)

		ns := st[n.ID]
		if ns.State == states.Stale {
			rep.Stale = append(rep.Stale, StaleNode{
				ID: n.ID, SeqStale: ns.SeqStale, DependencyStale: ns.DependencyStale,
				DigestStale: ns.DigestStale, IntentStale: ns.IntentStale, InputStale: ns.InputStale,
				ReviewStale: ns.ReviewStale, AnchorAdvisory: ns.AnchorAdvisory,
			})
		}

		// Duplicate tests within this node.
		within := map[[2]string]int{}
		for _, t := range n.Gate.Tests {
			key := [2]string{t.ID, t.File}
			within[key]++
			if !seenTest[key] {
				seenTest[key] = true
				ownerByTest[key] = []string{}
			}
		}
		for id, c := range within {
			if c > 1 {
				rep.DuplicateTests = append(rep.DuplicateTests, DuplicateTest{Node: n.ID, TestID: id[0], File: id[1]})
			}
		}
		// Record ownership for shared-test detection.
		for _, t := range n.Gate.Tests {
			key := [2]string{t.ID, t.File}
			if !containsString(ownerByTest[key], n.ID) {
				ownerByTest[key] = append(ownerByTest[key], n.ID)
			}
		}

		// Unresolved inputs (distinct from the finding, for clean reporting).
		for _, spec := range n.Inputs {
			if _, rerr := inRes.Resolve(spec); rerr != nil {
				rep.UnresolvedInputs = append(rep.UnresolvedInputs, UnresolvedInput{
					Node: n.ID, Input: describeInputSpec(spec), Error: rerr.Error(),
				})
			}
		}
	}

	// Cross-node shared tests (informational).
	for id, owners := range ownerByTest {
		if len(owners) > 1 {
			sort.Strings(owners)
			rep.SharedTests = append(rep.SharedTests, SharedTest{TestID: id[0], File: id[1], Nodes: owners})
		}
	}

	rep.Coverage = coverageReport(sources, g)

	// Deterministic ordering.
	sort.Slice(rep.Stale, func(i, j int) bool { return rep.Stale[i].ID < rep.Stale[j].ID })
	sort.Slice(rep.DuplicateTests, func(i, j int) bool {
		if rep.DuplicateTests[i].Node != rep.DuplicateTests[j].Node {
			return rep.DuplicateTests[i].Node < rep.DuplicateTests[j].Node
		}
		if rep.DuplicateTests[i].TestID != rep.DuplicateTests[j].TestID {
			return rep.DuplicateTests[i].TestID < rep.DuplicateTests[j].TestID
		}
		return rep.DuplicateTests[i].File < rep.DuplicateTests[j].File
	})
	sort.Slice(rep.SharedTests, func(i, j int) bool {
		if rep.SharedTests[i].TestID != rep.SharedTests[j].TestID {
			return rep.SharedTests[i].TestID < rep.SharedTests[j].TestID
		}
		return rep.SharedTests[i].File < rep.SharedTests[j].File
	})
	sort.Slice(rep.UnresolvedInputs, func(i, j int) bool {
		if rep.UnresolvedInputs[i].Node != rep.UnresolvedInputs[j].Node {
			return rep.UnresolvedInputs[i].Node < rep.UnresolvedInputs[j].Node
		}
		return rep.UnresolvedInputs[i].Input < rep.UnresolvedInputs[j].Input
	})
	return rep, nil
}

// coverageReport computes per-source, per-family coverage: every id the
// validator agrees is defined in a reachable source, marked covered when a
// node cites it. The mandatory column reflects only the AC demand of a
// directly related spec (compile's exit code); FR/NFR/DD and transitive AC
// rows are informative.
func coverageReport(sources *Sources, g *model.Graph) []SourceCoverage {
	directSpecs := map[string]bool{}
	for _, p := range sources.set.acPairs {
		directSpecs[p.SourceRel] = true
	}
	covered := map[string]bool{}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		for _, cited := range n.Justifies {
			if hit, item, ok := sources.set.resolveItem(cited); ok {
				covered[hit.SourceRel+"\x00"+item.Family+"\x00"+item.ID] = true
			}
		}
	}

	var out []SourceCoverage
	for _, src := range sources.set.index.Sources() {
		sc := SourceCoverage{Source: src.Rel, Kind: src.Kind()}
		for _, family := range rules.IdentifierFamilies() {
			defined := sources.set.index.DefinedBy(src.Rel, family)
			fc := FamilyCoverage{Family: family, Defined: len(defined),
				Mandatory: family == "AC" && directSpecs[src.Rel]}
			for _, id := range defined {
				if covered[src.Rel+"\x00"+family+"\x00"+id] {
					fc.Covered++
				} else {
					fc.Uncovered = append(fc.Uncovered, id)
				}
			}
			sort.Strings(fc.Uncovered)
			sc.Families = append(sc.Families, fc)
		}
		out = append(out, sc)
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
