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

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/hazards"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/intent"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// deriveClosure builds the derive pass the render path projects: full
// three-axis states (digest from the shared tree, current intent
// fingerprints from the same sources compile embeds from, current input
// fingerprints from the shared input resolver) plus the D-0022
// closed predicate. One closure, applied to both the preflight preview and
// the written graph, so the dry-run and the render can never disagree.
func deriveClosure(repoRoot string, sources *sourceSet, inRes *InputResolver) func(*model.Graph) (map[string]states.NodeState, map[string]bool) {
	snap := sources.intentSnapshot()
	digester := digest.New(repoRoot)
	return func(g *model.Graph) (map[string]states.NodeState, map[string]bool) {
		st := states.Derive(states.Inputs{
			Graph:               g,
			ArtifactDigest:      digester.Artifact,
			CurrentIntentHashes: snap.Hashes(),
			CurrentInputHashes:  inRes.GraphHashes(g),
		})
		return st, states.Closed(g, st)
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
	inRes := NewInputResolver(root, sources.inputRepoRoot)

	findings, err := semanticFindings(g, p, sources, inRes)
	if err != nil {
		return nil, nil, err
	}
	if len(findings) > 0 {
		return nil, findings, nil
	}

	// Embed fingerprints, then append under the store's compare-and-swap.
	hashes := map[string]map[string]string{}
	var added []string
	for i := range p.Nodes {
		n := &p.Nodes[i]
		Anchor(n, sources.resolveItem)
		if err := inRes.AnchorInputs(n); err != nil {
			return nil, nil, fmt.Errorf("compile: %w", err)
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
	preview := *g
	preview.Nodes = append(append([]model.Node(nil), g.Nodes...), p.Nodes...)
	deriveFor := deriveClosure(repoRoot, sources, inRes)
	pst, pclosed := deriveFor(&preview)
	if err := preflightViews(root, plan, &preview, pst, pclosed); err != nil {
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

// Validate runs the full semantic pass over a graph as it stands (an empty
// proposal against it) — the transition gate `graph split` and friends use
// to prove a mutation introduces no findings that compile would refuse. When
// a caller already holds a Sources snapshot, use Sources.Validate instead so
// the before/after comparison shares that snapshot rather than re-resolving.
func Validate(root, repoRoot, plan string, g *model.Graph) ([]Finding, error) {
	sources, err := NewSources(root, repoRoot, plan)
	if err != nil {
		return nil, err
	}
	return sources.Validate(g)
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
// own reachability) and their fingerprints.
// acPair is one direct spec's acceptance criterion — the unit of the
// per-spec coverage demand (DD-4: the plan's OWN requirement surface, per
// spec; a citation resolving to one spec never satisfies another spec's
// same-numbered criterion).
type acPair struct {
	SourceRel string
	Qualifier string
	ID        string
}

type sourceSet struct {
	inputRepoRoot string
	planDir       string
	// index is the validator's citation-resolution opinion, shared verbatim
	// (bare and qualified spellings, ambiguity marked, never first-wins).
	index *rules.CitationIndex
	// items carries each reachable source's fingerprintable requirement
	// text: sourceRel -> bare id -> item.
	items   map[string]map[string]intent.Item
	acPairs []acPair // direct specs x defined ACs, sorted
	// loaded and planRel let refusals explain a qualified citation whose
	// source exists but is not on the plan's related graph.
	loaded  *rules.Root
	planRel string
	// directByID maps a bare id (e.g. "FR-01") to the reachable source(s),
	// among the plan README's own DIRECTLY related sources, that define it.
	// A bare citation prefers a direct source over a transitive one: the
	// plan's own `related` list is a stronger claim of intent than a source
	// reached only through a related design's own `related` graph.
	directByID map[string][]string
}

// resolveItem resolves one citation spelling to its defining source's
// fingerprintable item: unambiguous resolution AND extractable text, the
// same bar the flat lookup set before qualified spellings existed.
//
// A bare (unqualified) id first checks the plan's DIRECTLY related sources:
// when exactly one of them defines it, that source wins even if a
// transitively related source (reached only through a related design's own
// `related` list) also defines the name — the transitive source never
// contributes to the ambiguity check in that case. When no direct source
// defines the bare id, resolution falls back to the full transitive index,
// which still refuses a genuine tie among direct sources (or among
// transitive sources when none is direct) via the existing did-you-mean
// path. Qualified spellings (`Specs/X:FR-01`, `X:FR-01`) always resolve
// through the transitive index, unchanged.
func (s *sourceSet) resolveItem(cited string) (rules.CitationHit, intent.Item, bool) {
	if !strings.Contains(cited, ":") {
		if direct := s.directByID[cited]; len(direct) == 1 {
			sourceRel := direct[0]
			if item, ok := s.items[sourceRel][cited]; ok {
				return rules.CitationHit{
					SourceRel: sourceRel,
					Qualifier: rules.SourceQualifier(sourceRel),
					ID:        cited,
					Kind:      s.loaded.ByPath[sourceRel].Kind(),
				}, item, true
			}
		}
	}
	hit, ok := s.index.Resolve(cited)
	if !ok {
		return rules.CitationHit{}, intent.Item{}, false
	}
	item, ok := s.items[hit.SourceRel][hit.ID]
	return hit, item, ok
}

// unrelatedHint explains an unresolved qualified citation whose qualifier
// names a real spec or design the plan's `related` graph never reaches:
// designs are discovered only through the citing artifact's own `related`
// chain (a spec's back-link to its realizing design is not a hop), so the
// repair is to relate the source directly from the plan README. Empty when
// the citation is bare or names nothing.
func (s *sourceSet) unrelatedHint(cited string) string {
	if s.loaded == nil || s.index == nil {
		return ""
	}
	src := s.index.UnrelatedSource(s.loaded, cited)
	if src == "" {
		return ""
	}
	return fmt.Sprintf("; %s defines it but is not reachable through the plan's `related` graph — relate it directly from %s (a spec's back-link to its design is not a discovery hop)", src, s.planRel)
}

// intentSnapshot resolves the plan's citation dispositions from this source
// set: every unambiguous citation spelling that resolves to a fingerprintable
// item. A citation that is deleted, unlinked, or ambiguous lands in neither
// half — the fail-closed signal states.Derive reads.
func (s *sourceSet) intentSnapshot() IntentSnapshot {
	snap := IntentSnapshot{Items: map[string]intent.Item{}}
	for _, key := range s.index.Keys() {
		if _, item, ok := s.resolveItem(key); ok {
			snap.Items[key] = item
		}
	}
	// A bare id resolved against a direct source (resolveItem's own
	// preference) may be transitively ambiguous and so absent from
	// s.index.Keys(), which only enumerates the full transitive index's
	// unambiguous keys. Walk directByID too, so the snapshot agrees with
	// what compile actually embedded.
	for key := range s.directByID {
		if _, item, ok := s.resolveItem(key); ok {
			snap.Items[key] = item
		}
	}
	// Plan-decision ids (Designs/PlanDecisions) are citable and
	// fingerprintable like any requirement, but Keys() only enumerates the
	// spec/design index — the bare and plan-qualified decision spellings
	// resolve dynamically through the decisions index instead, so they are
	// walked here from the loaded files.
	for _, file := range s.index.PlanDecisions() {
		if file.Err != nil {
			continue
		}
		for _, e := range file.Entries {
			for _, key := range []string{e.ID, file.Plan + ":" + e.ID} {
				if _, item, ok := s.resolveItem(key); ok {
					snap.Items[key] = item
				}
			}
		}
	}
	return snap
}

// acKey keys per-spec AC coverage — the validator's own key, so the two
// coverage opinions share one identity.
func acKey(sourceRel, id string) string { return rules.CitationKey(sourceRel, id) }

// identifierSources loads the root and collects everything the plan's
// related graph lets its nodes cite.
func identifierSources(root, repoRoot, plan string) (*sourceSet, error) {
	loaded, err := rules.LoadRootRepo(root, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("compile: loading planning root: %w", err)
	}
	index, err := loaded.ValidatedDecisionIndex()
	if err != nil {
		return nil, fmt.Errorf("compile: refusing incomplete per-plan decisions snapshot: %w", err)
	}
	loaded.DecisionIndex = index
	planRel := "Plans/" + plan + "/README.md"
	planArt, ok := loaded.ByPath[planRel]
	if !ok {
		return nil, fmt.Errorf("compile: %s does not exist; the plan's README carries the `related` graph citations resolve through", planRel)
	}
	out := &sourceSet{items: map[string]map[string]intent.Item{}, loaded: loaded, planRel: planRel}
	out.inputRepoRoot = loaded.RepoForArtifact(planArt.Rel)
	out.planDir = filepath.Dir(planArt.AbsPath)
	out.index = rules.BuildCitationIndex(loaded, planArt)
	// directByID: bare id -> the plan README's own DIRECTLY related
	// source(s) defining it. Built from the same index (so it never
	// disagrees about what a source defines), scoped to the plan's own
	// `related` list rather than the full transitive walk.
	out.directByID = map[string][]string{}
	for _, src := range rules.DirectRelatedSources(loaded, planArt) {
		for _, family := range rules.IdentifierFamilies() {
			for _, id := range out.index.DefinedBy(src.Rel, family) {
				if !containsString(out.directByID[id], src.Rel) {
					out.directByID[id] = append(out.directByID[id], src.Rel)
				}
			}
		}
	}
	for _, src := range out.index.Sources() {
		body := rules.CommentStripped(src.Body)
		items := intent.Items(body)
		// Only ids the validator agrees are defined enter the set — Items
		// and DefinedIdentifiers use the same patterns, but the inventory is
		// the validator's call (the index carries that inventory).
		per := map[string]intent.Item{}
		for _, family := range rules.IdentifierFamilies() {
			for _, id := range out.index.DefinedBy(src.Rel, family) {
				if item, ok := items[id]; ok {
					per[id] = item
				}
			}
		}
		out.items[src.Rel] = per
	}
	// Frozen review findings (ReviewDrivenAmendment DD-6): an extend node
	// cites the finding that demanded it; the title is the fingerprint.
	for rel, per := range out.index.ReviewFindings() {
		items := map[string]intent.Item{}
		for id, title := range per {
			normalized := intent.Normalize(title)
			items[id] = intent.Item{ID: id, Family: "F", Normalized: normalized, Hash: intent.Hash(normalized)}
		}
		out.items[rel] = items
	}
	// Plan decisions (Designs/PlanDecisions) are citable and fingerprintable
	// like any requirement: the item text is the normalized statement, so a
	// node citing `pd-…` carries an intent hash that goes stale only if the
	// entry changes — which, entries being immutable, means never.
	for _, file := range out.index.PlanDecisions() {
		if file.Err != nil {
			continue
		}
		per := map[string]intent.Item{}
		for _, e := range file.Entries {
			normalized := intent.Normalize(e.Statement)
			per[e.ID] = intent.Item{ID: e.ID, Family: "PD", Normalized: normalized, Hash: intent.Hash(normalized)}
		}
		out.items[file.Rel] = per
	}
	// Coverage is an exit code over the plan's OWN requirement surface
	// (DD-4): only specs the plan's README directly relates put their ACs
	// on the coverage demand — and the demand is PER SPEC. Transitively
	// reachable specs stay citable but demand nothing here.
	for _, src := range rules.DirectRelatedSources(loaded, planArt) {
		if src.Kind() != "spec" {
			continue
		}
		for _, id := range out.index.DefinedBy(src.Rel, "AC") {
			out.acPairs = append(out.acPairs, acPair{
				SourceRel: src.Rel, Qualifier: rules.SourceQualifier(src.Rel), ID: id,
			})
		}
	}
	sort.Slice(out.acPairs, func(i, j int) bool {
		if out.acPairs[i].Qualifier != out.acPairs[j].Qualifier {
			return out.acPairs[i].Qualifier < out.acPairs[j].Qualifier
		}
		return out.acPairs[i].ID < out.acPairs[j].ID
	})
	return out, nil
}

// semanticFindings is the batched pass: every invariant, every violation,
// one report, deterministic order. The returned error is non-nil only when
// a retirement source could not be verified operationally (network,
// filesystem or process trouble, not a genuine finding) — callers that
// need to fail closed rather than report an unanswered probe as an
// ordinary finding must check it before trusting an empty finding list.
func semanticFindings(g *model.Graph, p *model.Proposal, sources *sourceSet, inRes *InputResolver) ([]Finding, error) {
	var out []Finding
	add := func(where, format string, args ...any) {
		out = append(out, Finding{Where: where, Msg: fmt.Sprintf(format, args...)})
	}
	problems, err := rules.RetirementProblemsChecked(sources.planDir, g)
	if err != nil {
		return nil, err
	}
	for _, problem := range problems {
		add("graph", "%s", problem)
	}

	// Merged view: master nodes plus proposal nodes. stored marks the master
	// nodes — the missing-fingerprint guard applies to them (a committed node
	// must carry a hash for every fingerprintable citation), while proposal
	// nodes are construction input compile anchors AFTER this pass.
	merged := map[string]*model.Node{}
	stored := map[string]bool{}
	for i := range g.Nodes {
		merged[g.Nodes[i].ID] = &g.Nodes[i]
		stored[g.Nodes[i].ID] = true
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

		// Roles (ReviewDrivenAmendment DD-1): a review node is exactly a
		// node with a review gate, and it reviews its deps, so it must have
		// some. An acceptance node depends on review nodes, never on raw
		// work, so scrutiny is on the path to closure (DD-8).
		switch role := n.EffectiveRole(); {
		case role == model.RoleReview && n.Gate.Type != model.GateReview:
			add(id, "has role review but gate type %q; a review node's gate is `review`", n.Gate.Type)
		case role != model.RoleReview && n.Gate.Type == model.GateReview:
			add(id, "has a review gate but role %q; only a review node carries a review gate", role)
		case role == model.RoleReview && len(n.Deps) == 0:
			add(id, "is a review node with no deps; a review node reviews the contract nodes it depends on")
		case role == model.RoleIntegrationAcceptance:
			for _, dep := range n.Deps {
				if d, ok := merged[dep]; ok && d.EffectiveRole() != model.RoleReview && d.EffectiveRole() != model.RoleIntegrationAcceptance {
					add(id, "is an acceptance node depending on %q (role %s); acceptance depends on review nodes, not raw work", dep, d.EffectiveRole())
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
			if hit, _, ok := sources.resolveItem(cited); ok {
				if strings.HasPrefix(hit.ID, "AC-") {
					citedACs[acKey(hit.SourceRel, hit.ID)] = true
				}
				// The fail-closed half of INTENT-STALE: a committed node
				// citing a currently fingerprintable requirement with no
				// embedded hash (missing, or present but empty) cannot be
				// verified against the text it claims to satisfy. Proposal
				// nodes are exempt — compile anchors them after this pass.
				if stored[id] && n.IntentHashes[cited] == "" {
					add(id, "cites %q (defined in %s) with no embedded intent fingerprint; repair with `sdd graph repair-intent --plan <plan> --node %s`", cited, hit.SourceRel, id)
				}
				continue
			}
			if suggestions := sources.index.Ambiguous(cited); len(suggestions) > 0 {
				add(id, "cites %q, which is defined by more than one related source; qualify it (%s)", cited, strings.Join(suggestions, ", "))
				continue
			}
			if _, _, isRef := decisions.ParseRef(cited); isRef {
				add(id, "cites %q, which no plan under the planning root records; record it with `sdd decide add --plan <plan> --statement ...` or cite the recorded id", cited)
				continue
			}
			add(id, "cites %q, which resolves in no related spec, design%s", cited, sources.unrelatedHint(cited))
		}

		seenTests := map[[2]string]bool{}
		for _, test := range n.Gate.Tests {
			key := [2]string{test.File, test.ID}
			if seenTests[key] {
				add(id, "declares test %q in %q more than once", test.ID, test.File)
			}
			seenTests[key] = true
		}

		// Declared inputs: every one must resolve (missing/ambiguous
		// headings, escapes, directories, binary/non-Markdown sections all
		// refuse — never fall back), and a committed node must carry the
		// embedded fingerprint for each. Proposal nodes are anchored after
		// this pass, so the missing-fingerprint guard applies only to stored
		// nodes — the same split compile makes for citations.
		for _, spec := range n.Inputs {
			key := model.InputKey(spec)
			if _, err := inRes.Resolve(spec); err != nil {
				add(id, "declared input %q does not resolve: %v", describeInputSpec(spec), err)
				continue
			}
			if stored[id] && n.InputHashes[key] == "" {
				add(id, "declares input %q with no embedded input fingerprint; re-set it with `sdd graph set-inputs --plan <plan> --node %s`", describeInputSpec(spec), id)
			}
		}
	}

	// Cycles over the merged graph.
	for _, cycle := range algorithms.Cycles(adjacency) {
		add("graph", "dependency cycle: %s", strings.Join(append(append([]string{}, cycle...), cycle[0]), " -> "))
	}

	// Acceptance may confer completion closure only over work with a full
	// review upstream. A terminal full gate downstream remains a plan-wide
	// backstop, but cannot authorize an earlier acceptance node.
	for _, id := range ids {
		acceptance := merged[id]
		if acceptance.EffectiveRole() != model.RoleIntegrationAcceptance {
			continue
		}
		upstream := algorithms.DependencyClosure(adjacency, id)
		fullCovered := map[string]bool{}
		for candidateID := range upstream {
			candidate, present := merged[candidateID]
			if !present || candidate.Gate.Type != model.GateReview || candidate.Gate.Lanes != nil {
				continue
			}
			fullCovered[candidateID] = true
			for dep := range algorithms.DependencyClosure(adjacency, candidateID) {
				fullCovered[dep] = true
			}
		}
		var uncovered []string
		for upstreamID := range upstream {
			upstreamNode, present := merged[upstreamID]
			if !present {
				continue // the missing-dependency finding already names this
			}
			if upstreamNode.EffectiveRole() == model.RoleIntegrationAcceptance {
				continue // checked independently; supports acceptance chains
			}
			if !fullCovered[upstreamID] {
				uncovered = append(uncovered, upstreamID)
			}
		}
		if len(uncovered) > 0 {
			sort.Strings(uncovered)
			add(id, "cannot confer closure: no full review upstream covers %s; subset reviews and downstream full gates do not authorize acceptance", strings.Join(uncovered, ", "))
		}
	}

	// AC coverage: every AC of every DIRECTLY related spec has a covering
	// node, per spec (DD-4: coverage is an exit code, not a review
	// judgment; one spec's citation never covers another spec's
	// same-numbered criterion). The finding names the bare id when it is
	// unique across the demand, the qualified spelling when it is not.
	bareCount := map[string]int{}
	for _, p := range sources.acPairs {
		bareCount[p.ID]++
	}
	for _, p := range sources.acPairs {
		if citedACs[acKey(p.SourceRel, p.ID)] {
			continue
		}
		label := p.ID
		if bareCount[p.ID] > 1 {
			label = p.Qualifier + ":" + p.ID
		}
		add("graph", "%s has no covering node; cover it or retire it in the spec", label)
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
	return out, nil
}
