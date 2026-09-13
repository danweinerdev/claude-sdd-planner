package compile

// The shared intent-anchor surface (Designs/SddGraph DD-4). compile embeds
// each cited requirement's fingerprint and split must do the same for every
// child it retires a node into; a second opinion on what a citation resolves
// to, or a second hasher, would let the two embed different fingerprints for
// the same citation. This file is that one opinion:
//
//   - Anchor is the single resolver/embedder. It keys every embedded hash by
//     the citation AS WRITTEN — qualified spellings included — so states'
//     staleness lookups match what IntentSnapshot serves under the same keys.
//   - Plan decisions are fingerprintable like other citations. There is no
//     decision-shaped exemption from intent anchoring or staleness checks.
//   - Sources is one plan's citation-resolution snapshot. Split builds it once
//     and shares it across the anchor and validate paths, so a spec edit
//     landing mid-split cannot re-anchor children against text the gate did
//     not validate.

import (
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/intent"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// Resolver resolves one citation spelling to its defining source's
// fingerprintable item: unambiguous resolution AND extractable text, the
// same bar the flat lookup set before qualified spellings existed.
type Resolver func(cited string) (rules.CitationHit, intent.Item, bool)

// Anchor is the single intent-anchor resolver/embedder shared by compile and
// split. For each of the node's own justifications it resolves the citation
// and, when the citation is fingerprintable, embeds the requirement's current
// hash under the citation as written. A plan-decision citation (`pd-…`,
// Designs/PlanDecisions) is fingerprintable like any other requirement — its
// item text is the entry's normalized statement — and entries are immutable,
// so the hash never goes stale.
func Anchor(n *model.Node, resolve Resolver) {
	for _, cited := range n.Justifies {
		_, item, ok := resolve(cited)
		if !ok {
			continue // ambiguous or unresolved citation
		}
		if n.IntentHashes == nil {
			n.IntentHashes = map[string]string{}
		}
		n.IntentHashes[cited] = item.Hash
	}
}

// Sources is one plan's citation-resolution snapshot: which ids exist (per
// the validator's own reachability), their fingerprints, and the per-plan
// decisions. It carries the same resolution opinion every consumer
// needs, built once, so embed, validate, and repair can never disagree about
// what a citation means. It also carries the input resolver, so validation
// and split anchor both citations and declared inputs from one snapshot.
type Sources struct {
	set   *sourceSet
	inRes *InputResolver
}

// NewSources loads one plan's citation-resolution snapshot.
func NewSources(root, repoRoot, plan string) (*Sources, error) {
	set, err := identifierSources(root, repoRoot, plan)
	if err != nil {
		return nil, err
	}
	return &Sources{set: set, inRes: NewInputResolver(root, set.inputRepoRoot)}, nil
}

// Anchor embeds fingerprints for the node's own justifications against this
// snapshot.
func (s *Sources) Anchor(n *model.Node) {
	Anchor(n, s.set.resolveItem)
}

// AnchorInputs resolves and embeds fingerprints for the node's declared
// inputs against this snapshot's root pair.
func (s *Sources) AnchorInputs(n *model.Node) error {
	return s.inRes.AnchorInputs(n)
}

// InputResolver returns this snapshot's input resolver (the shared input
// opinion), for callers that need input hashes or resolved text.
func (s *Sources) InputResolver() *InputResolver {
	return s.inRes
}

// Validate runs the semantic pass over g, treating every node as stored (an
// empty proposal against it) — the same gate compile applies, including the
// missing-fingerprint guard, against this snapshot. The returned error is
// non-nil only when a retirement source could not be verified operationally
// (network, filesystem or process trouble, not a genuine finding); every
// caller must check it before trusting the finding list, never read an
// unanswered probe as a clean or ordinary result.
func (s *Sources) Validate(g *model.Graph) ([]Finding, error) {
	return semanticFindings(g, &model.Proposal{Version: model.SchemaVersion}, s.set, s.inRes)
}

// CitationKind classifies one justification against the snapshot: how it
// resolves, which is the input to repair's conservative backfill eligibility.
type CitationKind int

const (
	// CitationFingerprintable resolves to a spec/design requirement with
	// extractable text — a hash can and must be embedded.
	CitationFingerprintable CitationKind = iota
	// CitationDecision is a plan-decision-shaped id (`pd-…`) that no plan
	// under the planning root records — refusable, like CitationUnresolved,
	// but with its own repair-path message.
	CitationDecision
	// CitationAmbiguous is a bare id defined by more than one related source.
	CitationAmbiguous
	// CitationUnresolved resolves to no related spec, design, or decision.
	CitationUnresolved
)

// CitationDisposition is one justification's classification and, when
// fingerprintable, its defining source and item.
type CitationDisposition struct {
	Kind CitationKind
	Hit  rules.CitationHit
	Item intent.Item
	// Suggestions lists the qualified spellings competing for an ambiguous
	// bare citation.
	Suggestions []string
	// Hint, for an unresolved citation, is the compiler's explanation suffix
	// (the qualifier names a real source the plan's related graph never
	// reaches), or "".
	Hint string
}

// ClassifyCitation classifies one citation spelling with the SAME resolution
// opinion the anchor and validate paths use — no caller re-derives
// resolution, so a citation can never mean different things in embed,
// validate, and repair.
func (s *Sources) ClassifyCitation(cited string) CitationDisposition {
	if hit, item, ok := s.set.resolveItem(cited); ok {
		return CitationDisposition{Kind: CitationFingerprintable, Hit: hit, Item: item}
	}
	if sugg := s.set.index.Ambiguous(cited); len(sugg) > 0 {
		return CitationDisposition{Kind: CitationAmbiguous, Suggestions: sugg}
	}
	if _, _, isRef := decisions.ParseRef(cited); isRef {
		return CitationDisposition{Kind: CitationDecision}
	}
	return CitationDisposition{Kind: CitationUnresolved, Hint: s.set.unrelatedHint(cited)}
}

// IntentSnapshot is one plan's citation-disposition snapshot, resolved once:
// the current fingerprint of every resolvable fingerprintable citation (keyed
// by the citation AS WRITTEN). All entries come from the same snapshot, so derive can
// never disagree with what compile/split embedded — a citation that vanishes,
// unlinks, or turns ambiguous is absent and derives stale, while
// every citation is fingerprintable, so an unhashed one is stale.
type IntentSnapshot struct {
	// Items maps cited id (as written) -> the resolved requirement item
	// (normalized text + hash). The hash half feeds INTENT-STALE matching;
	// the text half feeds `next --claim` inlining.
	Items map[string]intent.Item
}

// Hashes returns the hash half of the snapshot: cited id -> current
// fingerprint. A cited id absent from the result does not currently resolve
// to a fingerprintable requirement (deleted, unlinked, or ambiguous).
func (s IntentSnapshot) Hashes() map[string]string {
	out := make(map[string]string, len(s.Items))
	for id, item := range s.Items {
		out[id] = item.Hash
	}
	return out
}

// IntentSnapshot resolves the plan's citation dispositions from this
// already-loaded source-resolution snapshot.
func (s *Sources) IntentSnapshot() IntentSnapshot {
	return s.set.intentSnapshot()
}

// LoadIntentSnapshot loads one plan's source-resolution snapshot and returns
// its citation dispositions.
func LoadIntentSnapshot(root, repoRoot, plan string) (IntentSnapshot, error) {
	sources, err := NewSources(root, repoRoot, plan)
	if err != nil {
		return IntentSnapshot{}, err
	}
	return sources.IntentSnapshot(), nil
}
