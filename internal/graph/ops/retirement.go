package ops

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// RetireWithSource attaches immutable historical evidence, including to an
// ID already retired by conversion. It never removes a live graph node.
func RetireWithSource(root, plan, id string, source model.RetirementSource, replacements []string, dryRun bool) (model.RetirementRecord, error) {
	dir := filepath.Join(root, "Plans", plan)
	canonical, err := rules.VerifyRetirementSource(dir, source)
	if err != nil {
		if errors.Is(err, vcs.ErrOperational) {
			return model.RetirementRecord{}, err
		}
		return model.RetirementRecord{}, &RefusedError{Reasons: []string{err.Error()}}
	}
	unique := map[string]bool{}
	for _, replacement := range replacements {
		unique[replacement] = true
	}
	replacements = nil
	for replacement := range unique {
		replacements = append(replacements, replacement)
	}
	sort.Strings(replacements)
	record := model.RetirementRecord{Source: canonical, ReplacedBy: replacements}
	apply := func(g *model.Graph) error {
		if id == "" || g.NodeByID(id) != nil {
			return &RefusedError{Reasons: []string{fmt.Sprintf("cannot retire live or empty id %q; use split for live work", id)}}
		}
		if prior, ok := g.RetirementSources[id]; ok && !reflect.DeepEqual(prior, record) {
			return &RefusedError{Reasons: []string{"historical retirement provenance is immutable; conflicting rewrite refused"}}
		}
		found := false
		for _, old := range g.Retired {
			if old == id {
				found = true
			}
		}
		if !found {
			g.Retired = append(g.Retired, id)
			sort.Strings(g.Retired)
		}
		if g.RetirementSources == nil {
			g.RetirementSources = map[string]model.RetirementRecord{}
		}
		g.RetirementSources[id] = record
		problems, err := rules.RetirementProblemsChecked(dir, g)
		if err != nil {
			return err
		}
		if len(problems) != 0 {
			return &RefusedError{Reasons: problems}
		}
		return nil
	}
	if dryRun {
		g, err := gstore.Load(gstore.PathFor(dir))
		if err != nil {
			return record, err
		}
		return record, apply(g)
	}
	_, err = gstore.Update(gstore.PathFor(dir), apply)
	return record, err
}
