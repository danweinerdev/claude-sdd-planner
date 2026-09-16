package compile

import (
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/intent"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

type Sources struct {
	set   *sourceSet
	inRes *InputResolver
}

func NewSources(root, repoRoot, plan string) (*Sources, error) {
	set, err := identifierSources(root, repoRoot, plan)
	if err != nil {
		return nil, err
	}
	return &Sources{set: set, inRes: NewInputResolver(root, set.inputRepoRoot)}, nil
}
func (s *Sources) InputResolver() *InputResolver { return s.inRes }
func (s *Sources) RepositoryRoot() string        { return s.set.inputRepoRoot }
func (s *Sources) Validate(g *model.Graph) ([]Finding, error) {
	return semanticFindings(g, &model.Proposal{Version: model.SchemaVersion}, s.set, s.inRes)
}

type CitationKind int

const (
	CitationFingerprintable CitationKind = iota
	CitationDecision
	CitationAmbiguous
	CitationUnresolved
)

type CitationDisposition struct {
	Kind        CitationKind
	Hit         rules.CitationHit
	Item        intent.Item
	Suggestions []string
	Hint        string
}

func (s *Sources) ClassifyCitation(cited string) CitationDisposition {
	if hit, item, ok := s.set.resolveItem(cited); ok {
		return CitationDisposition{Kind: CitationFingerprintable, Hit: hit, Item: item}
	}
	if suggestions := s.set.index.Ambiguous(cited); len(suggestions) > 0 {
		return CitationDisposition{Kind: CitationAmbiguous, Suggestions: suggestions}
	}
	if _, _, ok := decisions.ParseRef(cited); ok {
		return CitationDisposition{Kind: CitationDecision}
	}
	return CitationDisposition{Kind: CitationUnresolved, Hint: s.set.unrelatedHint(cited)}
}

type IntentSnapshot struct{ Items map[string]intent.Item }

func (s IntentSnapshot) Hashes() map[string]string {
	out := map[string]string{}
	for id, item := range s.Items {
		out[id] = item.Hash
	}
	return out
}
func (s *Sources) IntentSnapshot() IntentSnapshot { return s.set.intentSnapshot() }
func LoadIntentSnapshot(root, repoRoot, plan string) (IntentSnapshot, error) {
	s, err := NewSources(root, repoRoot, plan)
	if err != nil {
		return IntentSnapshot{}, err
	}
	return s.IntentSnapshot(), nil
}
