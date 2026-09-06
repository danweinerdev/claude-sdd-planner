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
//   - The D-NNNN exemption is not a name-prefix heuristic: decisions resolve
//     to no fingerprintable item, so the resolver reports ok=false and Anchor
//     skips them. There is deliberately no `strings.HasPrefix(cited, "D-")`
//     anywhere in this package.
//   - Sources is one plan's citation-resolution snapshot. Split builds it once
//     and shares it across the anchor and validate paths, so a spec edit
//     landing mid-split cannot re-anchor children against text the gate did
//     not validate.

import (
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
// hash under the citation as written. Decisions (D-NNNN) resolve to no
// fingerprintable item and are never hashed — their supersession is the
// ledger's own machinery, not a content fingerprint.
func Anchor(n *model.Node, resolve Resolver) {
	for _, cited := range n.Justifies {
		_, item, ok := resolve(cited)
		if !ok {
			continue // decision (D-NNNN) or otherwise not fingerprintable
		}
		if n.IntentHashes == nil {
			n.IntentHashes = map[string]string{}
		}
		n.IntentHashes[cited] = item.Hash
	}
}

// Sources is one plan's citation-resolution snapshot: which ids exist (per
// the validator's own reachability), their fingerprints, and the decision
// ledger's statuses. It carries the same resolution opinion every consumer
// needs, built once, so embed, validate, and repair can never disagree about
// what a citation means.
type Sources struct {
	set *sourceSet
}

// NewSources loads one plan's citation-resolution snapshot.
func NewSources(root, repoRoot, plan string) (*Sources, error) {
	set, err := identifierSources(root, repoRoot, plan)
	if err != nil {
		return nil, err
	}
	return &Sources{set: set}, nil
}

// Anchor embeds fingerprints for the node's own justifications against this
// snapshot.
func (s *Sources) Anchor(n *model.Node) {
	Anchor(n, s.set.resolveItem)
}

// Validate runs the semantic pass over g, treating every node as stored (an
// empty proposal against it) — the same gate compile applies, including the
// missing-fingerprint guard, against this snapshot.
func (s *Sources) Validate(g *model.Graph) []Finding {
	return semanticFindings(g, &model.Proposal{Version: model.SchemaVersion}, s.set)
}

// CitationKind classifies one justification against the snapshot: how it
// resolves, which is the input to repair's conservative backfill eligibility.
type CitationKind int

const (
	// CitationFingerprintable resolves to a spec/design requirement with
	// extractable text — a hash can and must be embedded.
	CitationFingerprintable CitationKind = iota
	// CitationDecision is a decision-ledger id (D-NNNN): exempt from
	// fingerprinting by design, never a refusal on its own.
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
	if _, ok := s.set.decisions[cited]; ok {
		return CitationDisposition{Kind: CitationDecision}
	}
	return CitationDisposition{Kind: CitationUnresolved}
}

// IntentSnapshot is one plan's citation-disposition snapshot, resolved once:
// the current fingerprint of every resolvable fingerprintable citation (keyed
// by the citation AS WRITTEN) plus the set of legitimate exempt decisions.
// Both halves come from the SAME source-resolution snapshot, so derive can
// never disagree with what compile/split embedded — a citation that vanishes,
// unlinks, or turns ambiguous lands in neither half and derives stale, while
// an accepted decision lands in Exemptions and stays valid without a hash.
type IntentSnapshot struct {
	// Items maps cited id (as written) -> the resolved requirement item
	// (normalized text + hash). The hash half feeds INTENT-STALE matching;
	// the text half feeds `next --claim` inlining.
	Items map[string]intent.Item
	// Exemptions is the set of cited ids that are accepted decisions —
	// legitimate exempt citations that carry no fingerprint by design.
	Exemptions map[string]bool
}

// Hashes returns the hash half of the snapshot: cited id -> current
// fingerprint. A cited id absent from the result does not currently resolve
// to a fingerprintable requirement (deleted, unlinked, ambiguous, or a
// decision).
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
