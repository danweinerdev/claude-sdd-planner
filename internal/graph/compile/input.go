package compile

// The input-resolution half of the shared resolution surface. Citations
// resolve through Sources (the validator's own reachability); declared
// read-only inputs resolve through InputResolver — the single opinion for
// compile embed, validation, audit, `next --claim` context, `set-inputs`, and
// validation. Both halves share one resolution opinion.

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

// describeInputSpec renders one declared input for findings and views:
// `root:path` or `root:path#Heading / Path`.
func describeInputSpec(spec model.Input) string {
	s := spec.Root + ":" + spec.Path
	if spec.Section != nil {
		s += "#" + strings.Join(spec.Section.HeadingPath, " / ")
	}
	return s
}
