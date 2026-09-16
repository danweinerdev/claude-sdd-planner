package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/inputs"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

// Input files need not be SDD artifacts. Resolve them using the same root and
// Markdown selector opinion as graph compile, including during draft validation.
func graphInputFindings(r *Root, emit func(Diagnostic)) {
	for _, plan := range r.Artifacts {
		if plan.Kind() != "plan" {
			continue
		}
		dir := filepath.Dir(plan.AbsPath)
		raw, err := os.ReadFile(filepath.Join(dir, filepath.Base(dir)+"-Graph.json"))
		if os.IsNotExist(err) {
			continue
		}
		finding := func(message string) {
			emit(Diagnostic{Code: "SDD180", Severity: Error, Path: plan.Rel, Line: 1,
				Message: message, Correction: "Resolve the declared root-relative input/heading and compile or explicitly set inputs through sdd; never edit input hashes."})
		}
		if err != nil {
			finding(fmt.Sprintf("Cannot read plan graph: %v", err))
			continue
		}
		graph, err := model.DecodeGraph(raw)
		if err != nil {
			finding(fmt.Sprintf("Cannot decode plan graph inputs: %v", err))
			continue
		}
		resolver := inputs.NewResolver(inputs.Roots{Repository: r.RepoForArtifact(plan.Rel), Planning: r.Dir})
		for _, n := range graph.Nodes {
			for _, spec := range n.Inputs {
				_, err := resolver.Resolve(spec)
				if err != nil {
					finding(fmt.Sprintf("Node %q input %s: %v", n.ID, model.InputKey(spec), err))
					continue
				}
			}
		}
	}
}

func inputRuleFixture(missing bool) map[string]string {
	content := "# Design\n\n## Storage\nSelected behavior.\n"
	decl := model.Input{Root: model.InputRootRepository, Path: "docs/PRDs/probe.md", Section: &model.InputSection{HeadingPath: []string{"Storage"}}}
	if missing {
		decl.Section.HeadingPath = []string{"Missing"}
	}
	g := model.Graph{Version: 1, Nodes: []model.Node{{ID: "probe", Contract: "c", Justifies: []string{},
		Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{},
		Inputs: []model.Input{decl}, Estimate: 1}}}
	encoded, _ := json.Marshal(g)
	files := map[string]string{
		"Plans/P/README.md":    validPlan(false),
		"Plans/P/P-Graph.json": string(encoded),
		"docs/PRDs/probe.md":   content,
	}
	return files
}

func init() {
	Register(&Rule{
		Code: "SDD180", Severity: Error, Native: true,
		What:      "a declared graph input is unresolved",
		CheckRoot: graphInputFindings,
		Good:      []Example{{Name: "repository-prd-section", Files: inputRuleFixture(false)}},
		Bad:       []Example{{Name: "missing-prd-section", Files: inputRuleFixture(true)}},
	})
}
