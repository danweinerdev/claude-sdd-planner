package compile

// The input-resolution half of the shared resolution surface. Citations
// resolve through Sources (the validator's own reachability); declared
// read-only inputs resolve through InputResolver — the single opinion for
// compile embed, validation, audit, `next --claim` context, `set-inputs`, and
// state drift. Both halves share the same one-opinion discipline as intent
// anchoring: a second resolver would let embed and recheck disagree.

import (
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/inputs"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

// InputResolver resolves declared read-only inputs against the explicit root
// pair (repository + planning), memoizing by input key so a derive pass that
// consults the same input from many nodes reads it once.
type InputResolver struct {
	res *inputs.Resolver
}

// NewInputResolver returns a resolver for the (root, repoRoot) pair. `root`
// is the planning root, `repoRoot` the target repository root — the same pair
// every rules load takes, never the process working directory.
func NewInputResolver(root, repoRoot string) *InputResolver {
	return &InputResolver{res: inputs.NewResolver(inputs.Roots{Repository: repoRoot, Planning: root})}
}

// Resolve resolves one declared input, memoized by input key.
func (r *InputResolver) Resolve(spec model.Input) (inputs.Resolved, error) {
	return r.res.Resolve(spec)
}

// GraphHashes resolves every input declared across g's nodes and returns
// input key -> current digest for the resolvable ones. An input that does not
// resolve is absent (fail-closed: a recorded hash cannot match an absent
// entry, so it derives stale).
func (r *InputResolver) GraphHashes(g *model.Graph) map[string]string {
	out := map[string]string{}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		for _, spec := range n.Inputs {
			key := model.InputKey(spec)
			if _, seen := out[key]; seen {
				continue
			}
			if resolved, err := r.Resolve(spec); err == nil {
				out[key] = resolved.Digest
			}
		}
	}
	return out
}

// AnchorInputs resolves each of the node's declared inputs and embeds its
// digest under its key. It returns the first unresolvable input; callers
// anchor inputs BEFORE semantic validation runs, so an unresolvable input is
// already a finding by the time the graph is written.
func (r *InputResolver) AnchorInputs(n *model.Node) error {
	for _, spec := range n.Inputs {
		resolved, err := r.Resolve(spec)
		if err != nil {
			return err
		}
		if n.InputHashes == nil {
			n.InputHashes = map[string]string{}
		}
		n.InputHashes[model.InputKey(spec)] = resolved.Digest
	}
	return nil
}

// describeInputSpec renders one declared input for findings and views:
// `root:path` or `root:path#Heading / Path`.
func describeInputSpec(spec model.Input) string {
	s := spec.Root + ":" + spec.Path
	if spec.Section != nil {
		s += "#" + strings.Join(spec.Section.HeadingPath, " / ")
	}
	return s
}
