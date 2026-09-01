// Package compile is the enforcement core of the plan graph (Designs/SddGraph
// DD-4, DD-9, DD-11): it takes the staged proposal, validates every semantic
// invariant in ONE batched pass, embeds the intent fingerprints that make
// spec edits ripple (INTENT-STALE), and appends the nodes to the committed
// graph under the store's compare-and-swap.
//
// Refusals are authoritative and complete: the repair loop is an edit to the
// payload file, and it should need exactly one round trip, so compile never
// stops at the first finding. Identifier resolution reuses the validator's
// own reachability and definition patterns (rules.RelatedIdentifierSources,
// rules.DefinedIdentifiers) — the recorded trap for this task is that a
// second resolution opinion would fight `sdd validate`, so there isn't one.
//
// Compile never inserts structure on the proposer's behalf: an uncovered
// node is a finding, not an auto-added review gate (the same
// no-silent-defaults rule conversion follows, DD-15).
package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/hazards"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/intent"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// deriveClosure builds the derive pass the render path projects: full
// three-axis states (digest from the shared tree, current intent
// fingerprints from the same sources compile embeds from) plus the D-0022
// closed predicate. One closure, applied to both the preflight preview and
// the written graph, so the dry-run and the render can never disagree.
func deriveClosure(repoRoot string, sources *sourceSet) func(*model.Graph) (map[string]states.NodeState, map[string]bool) {
	currentHashes := map[string]string{}
	for id, item := range sources.items {
		currentHashes[id] = item.Hash
	}
	digester := digest.New(repoRoot)
	return func(g *model.Graph) (map[string]states.NodeState, map[string]bool) {
		st := states.Derive(states.Inputs{
			Graph:               g,
			ArtifactDigest:      digester.Artifact,
			CurrentIntentHashes: currentHashes,
		})
		return st, review.Closed(g, st)
	}
}

// Finding is one semantic refusal: where (a node id, or `graph` for
// whole-graph findings) and what.
type Finding struct {
	Where string
	Msg   string
}

func (f Finding) String() string { return f.Where + ": " + f.Msg }

// Result reports a successful compile.
type Result struct {
	GraphPath string
	// Added is the appended node ids, in proposal order.
	Added []string
	// Hashes is node id -> cited id -> embedded intent hash.
	Hashes map[string]map[string]string
	// Consumed is the proposal (or single fragment) file that was consumed.
	Consumed string
	// Views is every rendered view file this compile wrote or refreshed.
	Views []string
}

// Run compiles the staged proposal for one plan. `root` is the planning
// root, `repoRoot` the target repository root (the pair every rules load
// takes), `plan` the plan directory name. A nil error with findings means
// the proposal was refused (exit 1 at the CLI); an error means compile could
// not run at all.
func Run(root, repoRoot, plan string) (*Result, []Finding, error) {
	planDir := filepath.Join(root, "Plans", plan)
	graphPath := gstore.PathFor(planDir)
	g, err := gstore.Load(graphPath)
	if err != nil {
		return nil, nil, fmt.Errorf("compile: %w (run `sdd graph init --plan %s` first)", err, plan)
	}
	payloadPath, payload, err := selectProposal(planDir)
	if err != nil {
		return nil, nil, err
	}
	p, err := model.DecodeProposal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("compile: %s is not a valid proposal:\n%w", payloadPath, err)
	}

	sources, err := identifierSources(root, repoRoot, plan)
	if err != nil {
		return nil, nil, err
	}

	findings := semanticFindings(g, p, sources)
	if len(findings) > 0 {
		return nil, findings, nil
	}

	// Embed fingerprints, then append under the store's compare-and-swap.
	hashes := map[string]map[string]string{}
	var added []string
	for i := range p.Nodes {
		n := &p.Nodes[i]
		for _, cited := range n.Justifies {
			item, ok := sources.items[cited]
			if !ok {
				continue // D-NNNN citations resolve but are not fingerprinted
			}
			if n.IntentHashes == nil {
				n.IntentHashes = map[string]string{}
			}
			n.IntentHashes[cited] = item.Hash
		}
		if len(n.IntentHashes) > 0 {
			hashes[n.ID] = n.IntentHashes
		}
		added = append(added, n.ID)
	}
	// Preflight the render targets BEFORE the graph write: a view refusal
	// (an existing hand-authored document in a target's place, or a frozen
	// view the new nodes would change) must leave the graph untouched and
	// the payload staged. The dry-run needs the same derived truth the real
	// render will project, so the derive pass runs on the preview graph.
	preview := &model.Graph{Version: g.Version, SeqCounter: g.SeqCounter, Retired: g.Retired}
	preview.Nodes = append(append(preview.Nodes, g.Nodes...), p.Nodes...)
	deriveFor := deriveClosure(repoRoot, sources)
	pst, pclosed := deriveFor(preview)
	if err := preflightViews(root, plan, preview, pst, pclosed); err != nil {
		return nil, nil, err
	}

	final, err := gstore.Update(graphPath, func(fresh *model.Graph) error {
		for _, n := range fresh.Nodes {
			for _, incoming := range p.Nodes {
				if n.ID == incoming.ID {
					return fmt.Errorf("compile: node %q landed in the graph while this compile ran; re-stage against the fresh graph", n.ID)
				}
			}
		}
		fresh.Nodes = append(fresh.Nodes, p.Nodes...)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	// Views render from the graph as written (DD-2: projections of the
	// source of truth, never of an in-memory draft).
	fst, fclosed := deriveFor(final)
	views, err := renderViews(root, plan, final, fst, fclosed)
	if err != nil {
		return nil, nil, fmt.Errorf("compile: graph written but view rendering failed (re-run `sdd compile` after fixing): %w", err)
	}
	// Consumed only after the graph write is durable.
	if err := os.Remove(payloadPath); err != nil {
		return nil, nil, fmt.Errorf("compile: graph written but %s could not be consumed: %w", payloadPath, err)
	}
	return &Result{GraphPath: graphPath, Added: added, Hashes: hashes, Consumed: payloadPath, Views: views}, nil, nil
}

// CurrentIntent returns every requirement fingerprint reachable from the
// plan's related graph right now: cited id -> its Item (normalized text +
// hash). The walk loop consumes it twice — states recheck embedded hashes
// against it (INTENT-STALE), and `next --claim` inlines the cited text into
// the context payload so an agent never re-reads specs wholesale.
func CurrentIntent(root, repoRoot, plan string) (map[string]intent.Item, error) {
	sources, err := identifierSources(root, repoRoot, plan)
	if err != nil {
		return nil, err
	}
	return sources.items, nil
}

// Validate runs the full semantic pass over a graph as it stands (an empty
// proposal against it) — the transition gate `graph split` and friends use
// to prove a mutation introduces no findings that compile would refuse.
func Validate(root, repoRoot, plan string, g *model.Graph) ([]Finding, error) {
	sources, err := identifierSources(root, repoRoot, plan)
	if err != nil {
		return nil, err
	}
	return semanticFindings(g, &model.Proposal{Version: model.SchemaVersion}, sources), nil
}

// selectProposal picks the compile input: the assembled proposal when it
// exists, else exactly one staged fragment (single-fragment flows skip
// assemble), else a helpful refusal.
func selectProposal(planDir string) (string, []byte, error) {
	assembled := proposal.AssembledPath(planDir)
	if raw, err := os.ReadFile(assembled); err == nil {
		return assembled, raw, nil
	}
	dir := proposal.FragmentsDir(planDir)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return "", nil, err
	}
	var fragments []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			fragments = append(fragments, e.Name())
		}
	}
	switch len(fragments) {
	case 0:
		return "", nil, fmt.Errorf("compile: nothing staged for this plan; author a payload from `sdd template graph-proposal` and stage it with `sdd graph propose`")
	case 1:
		path := filepath.Join(dir, fragments[0])
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", nil, err
		}
		return path, raw, nil
	default:
		return "", nil, fmt.Errorf("compile: %d fragments staged; merge them with `sdd graph assemble` first", len(fragments))
	}
}

// sourceSet is the resolution context: which ids exist (per the validator's
// own reachability), their fingerprints, and the decision ledger's statuses.
type sourceSet struct {
	items     map[string]intent.Item // FR/NFR/AC/DD -> fingerprint
	acIDs     []string               // every live AC across reachable specs, sorted
	decisions map[string]string      // D-NNNN -> status
}

// identifierSources loads the root and collects everything the plan's
// related graph lets its nodes cite.
func identifierSources(root, repoRoot, plan string) (*sourceSet, error) {
	loaded, err := rules.LoadRootRepo(root, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("compile: loading planning root: %w", err)
	}
	planRel := "Plans/" + plan + "/README.md"
	planArt, ok := loaded.ByPath[planRel]
	if !ok {
		return nil, fmt.Errorf("compile: %s does not exist; the plan's README carries the `related` graph citations resolve through", planRel)
	}
	out := &sourceSet{items: map[string]intent.Item{}, decisions: rules.DecisionStatuses(loaded)}
	// Coverage is an exit code over the plan's OWN requirement surface
	// (DD-4): only specs the plan's README directly relates put their ACs
	// on the coverage demand. Transitively reachable specs (a design's
	// background citations — often another plan's requirement surface)
	// stay citable below but demand nothing here.
	directSpec := map[string]bool{}
	for _, src := range rules.DirectRelatedSources(loaded, planArt) {
		if src.Kind() == "spec" {
			directSpec[src.Rel] = true
		}
	}
	for _, src := range rules.RelatedIdentifierSources(loaded, planArt) {
		kind := src.Kind()
		if kind != "spec" && kind != "design" {
			continue
		}
		body := rules.CommentStripped(src.Body)
		items := intent.Items(body)
		// Only ids the validator agrees are defined enter the set — Items
		// and DefinedIdentifiers use the same patterns, but the inventory is
		// the validator's call.
		for _, family := range rules.IdentifierFamilies() {
			for id := range rules.DefinedIdentifiers(src, family) {
				if item, ok := items[id]; ok {
					if _, dup := out.items[id]; !dup {
						out.items[id] = item
					}
				}
				if family == "AC" && directSpec[src.Rel] {
					out.acIDs = append(out.acIDs, id)
				}
			}
		}
	}
	sort.Strings(out.acIDs)
	return out, nil
}

// semanticFindings is the batched pass: every invariant, every violation,
// one report, deterministic order.
func semanticFindings(g *model.Graph, p *model.Proposal, sources *sourceSet) []Finding {
	var out []Finding
	add := func(where, format string, args ...any) {
		out = append(out, Finding{Where: where, Msg: fmt.Sprintf(format, args...)})
	}

	// Merged view: master nodes plus proposal nodes.
	merged := map[string]*model.Node{}
	for i := range g.Nodes {
		merged[g.Nodes[i].ID] = &g.Nodes[i]
	}
	// Duplicate ids: within the proposal, and against the master graph
	// (phase-1 review followup FU-01 — the model layer deliberately does not
	// check this; compile does). Retired ids are never reused (the same
	// stable-identifier discipline the markdown artifacts carry).
	retired := map[string]bool{}
	for _, id := range g.Retired {
		retired[id] = true
	}
	seen := map[string]bool{}
	for i := range p.Nodes {
		n := &p.Nodes[i]
		if seen[n.ID] {
			add(n.ID, "declared more than once in this proposal")
			continue
		}
		seen[n.ID] = true
		if retired[n.ID] {
			add(n.ID, "was retired (split or cut); retired ids are never reused — pick a fresh id")
			continue
		}
		if _, exists := merged[n.ID]; exists {
			add(n.ID, "already exists in the graph; proposals introduce nodes, mutations go through `sdd graph` verbs")
			continue
		}
		merged[n.ID] = n
	}

	adjacency := algorithms.Graph{}
	for id, n := range merged {
		adjacency[id] = n.Deps
	}

	ids := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	citedACs := map[string]bool{}
	for _, id := range ids {
		n := merged[id]

		// Dangling deps.
		for _, dep := range n.Deps {
			if _, ok := merged[dep]; !ok {
				add(id, "deps on %q, which no node declares", dep)
			}
		}

		// Hazards: triaged, known, and each discharged by a test that
		// claims it; a test may not claim an undeclared hazard.
		if n.Hazards == nil {
			add(id, "hazards are untriaged; triage against `sdd graph hazards` — an explicit empty list is a legitimate claim, an unmade judgment is not")
		} else {
			declared := map[string]bool{}
			for _, h := range n.Hazards {
				declared[h] = true
			}
			for _, err := range hazards.RequireKnownAll(n.Hazards, "node "+id) {
				add(id, "%v", err)
			}
			satisfied := map[string]bool{}
			for _, t := range n.Gate.Tests {
				for _, s := range t.Satisfies {
					if !declared[s] {
						add(id, "test %q satisfies %q, which the node does not declare", t.ID, s)
						continue
					}
					satisfied[s] = true
				}
			}
			for _, h := range n.Hazards {
				if hazards.Known(h) && !satisfied[h] {
					shape, _ := hazards.Lookup(h)
					add(id, "hazard %q is discharged by no test; one of the node's tests must declare `satisfies: [%q]` and take the required shape: %s", h, h, shape.RequiresTestThat)
				}
			}
		}

		// Review-gate lanes come from the closed four-lane vocabulary
		// (DD-9): a typo'd lane would silently review nothing, the same
		// failure class an unknown hazard is refused for.
		if n.Gate.Type == model.GateReview {
			for _, lane := range n.Gate.Lanes {
				if !model.KnownReviewLane(lane) {
					add(id, "names unknown review lane %q; the lanes are: %s (or \"full\" for all four — the only selection that carries completion-grade closure)", lane, strings.Join(model.ReviewLanes, ", "))
				}
			}
		}

		// Conversion sentinels (DD-15): a gate nobody specified and a
		// contract nobody wrote block compile per node, never default.
		if n.Gate.Type == model.GateUnspecified {
			add(id, "gate is unspecified (conversion sentinel); state how this node is verified — tests, command, or review")
		}
		if strings.HasPrefix(n.Contract, model.NeedsContractPrefix) {
			add(id, "contract is the conversion sentinel; replace it with a falsifiable sentence stating what is true when this node is done")
		}

		// Justifies: present, and every citation resolves.
		if len(n.Justifies) == 0 {
			add(id, "cites nothing; every node carries `justifies` naming the AC/FR/NFR/DD/D ids it exists for (an unsourced node is cut, not compiled)")
		}
		for _, cited := range n.Justifies {
			if _, ok := sources.items[cited]; ok {
				if strings.HasPrefix(cited, "AC-") {
					citedACs[cited] = true
				}
				continue
			}
			if status, ok := sources.decisions[cited]; ok {
				if status != "accepted" {
					add(id, "cites decision %s with status %q; live work cites accepted decisions", cited, status)
				}
				continue
			}
			add(id, "cites %q, which resolves in no related spec, design, or decision ledger", cited)
		}
	}

	// Cycles over the merged graph.
	for _, cycle := range algorithms.Cycles(adjacency) {
		add("graph", "dependency cycle: %s", strings.Join(append(append([]string{}, cycle...), cycle[0]), " -> "))
	}

	// AC coverage: every live AC across reachable specs has a covering node
	// (DD-4: coverage is an exit code, not a review judgment).
	for _, ac := range sources.acIDs {
		if !citedACs[ac] {
			add("graph", "%s has no covering node; cover it or retire it in the spec", ac)
		}
	}

	// Coverage invariant (DD-9): every node inside the dependency closure of
	// at least one full review gate; a full gate covers itself. Compile
	// never inserts a gate on the proposer's behalf.
	covered := map[string]bool{}
	for id, n := range merged {
		if n.Gate.Type == model.GateReview && n.Gate.Lanes == nil {
			covered[id] = true
			for dep := range algorithms.DependencyClosure(adjacency, id) {
				covered[dep] = true
			}
		}
	}
	for _, id := range ids {
		if !covered[id] {
			add(id, "covered by no full review gate; every node's completion-grade closure comes from a full gate's validation cycle (the template's terminal gate is the backstop)")
		}
	}

	// Claimed-artifact overlap: two claimed nodes writing one path is the
	// parallel-dispatch collision the artifact declarations exist to prevent.
	claimants := map[string][]string{}
	for _, id := range ids {
		n := merged[id]
		if n.Claim == nil {
			continue
		}
		for _, a := range n.Artifacts {
			claimants[a] = append(claimants[a], id)
		}
	}
	var overlapped []string
	for artifact, who := range claimants {
		if len(who) > 1 {
			sort.Strings(who)
			overlapped = append(overlapped, fmt.Sprintf("%s is claimed by %s", artifact, strings.Join(who, " and ")))
		}
	}
	sort.Strings(overlapped)
	for _, msg := range overlapped {
		add("graph", "claimed-artifact overlap: %s", msg)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Where != out[j].Where {
			return out[i].Where < out[j].Where
		}
		return out[i].Msg < out[j].Msg
	})
	return out
}
