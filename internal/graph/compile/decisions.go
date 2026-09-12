package compile

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// DecisionSync reports what SyncDesignDecisions appended.
type DecisionSync struct {
	Path    string            `json:"path"`
	Added   []decisions.Entry `json:"added"`
	Skipped []string          `json:"skipped,omitempty"` // design DDs already recorded
}

// SyncDesignDecisions copies every Design Decision of every design the
// plan's README directly relates into the plan's decisions file, verbatim,
// with `source: Designs/<X>:DD-N` (Designs/PlanDecisions DD-4). Compile is
// the handoff: from here on the plan's file is the mutable record and the
// design is provenance. Idempotent — an entry whose statement digest is
// already present is skipped, so recompiling after a design gains a DD adds
// exactly that DD and touches nothing else.
func SyncDesignDecisions(root, repoRoot, plan, today string) (*DecisionSync, error) {
	loaded, err := rules.LoadRootRepo(root, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("compile: loading planning root: %w", err)
	}
	planRel := "Plans/" + plan + "/README.md"
	planArt, ok := loaded.ByPath[planRel]
	if !ok {
		return nil, fmt.Errorf("compile: %s does not exist", planRel)
	}
	path := decisions.PathFor(filepath.Join(root, "Plans", plan))
	out := &DecisionSync{Path: path}
	designs := rules.DirectRelatedSources(loaded, planArt)
	sort.Slice(designs, func(i, j int) bool { return designs[i].Rel < designs[j].Rel })
	for _, d := range designs {
		if d.Kind() != "design" {
			continue
		}
		qualifier := rules.SourceQualifier(d.Rel)
		for _, dd := range decisions.ExtractDesignDecisions(rules.CommentStripped(d.Body)) {
			source := qualifier + ":" + dd.ID
			supersedes := ""
			if dd.Supersedes != "" {
				// `Supersedes DD-3` names a decision by its design id; the
				// edge is recorded against the entry compiled from it.
				target := dd.Supersedes
				if !strings.Contains(target, ":") {
					target = qualifier + ":" + target
				}
				hits := loaded.DecisionIndex.BySource(target, plan)
				if len(hits) == 0 {
					return out, fmt.Errorf("compile: %s supersedes %s, which is not recorded in any plan's decisions file; compile the design that defines it first", source, target)
				}
				supersedes = hits[0].Plan + ":" + hits[0].Entry.ID
				if hits[0].Plan == plan {
					supersedes = hits[0].Entry.ID
				}
			}
			res, err := decisions.Append(path, dd.Text, supersedes, source, today, plan, loaded.DecisionIndex)
			if err != nil {
				return out, fmt.Errorf("compile: recording %s: %w", source, err)
			}
			if res.Created {
				out.Added = append(out.Added, res.Entry)
				// Later DDs in this run may supersede this one; refresh the
				// index so BySource sees the entry just written.
				files, _ := decisions.LoadRoot(root)
				loaded.DecisionIndex = decisions.NewIndex(files)
			} else {
				out.Skipped = append(out.Skipped, source)
			}
		}
	}
	return out, nil
}
