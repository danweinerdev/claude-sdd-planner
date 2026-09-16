package main

import (
	"bytes"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/reportevidence"
)

func TestProductionCLIHasNoTestCommand(t *testing.T) {
	for _, c := range newRootCmd().Commands() {
		if c.Name() == "test" {
			t.Fatal("production CLI still registers sdd test")
		}
	}
}

func TestEvidenceContractWorksWithoutPlanningRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name string
		args []string
		want []byte
	}{
		{"contract", nil, reportevidence.ContractMarkdown()},
		{"schema", []string{"--json"}, reportevidence.SchemaJSON()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := graphEvidenceContractCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), tc.want) {
				t.Fatalf("output differs from embedded authority")
			}
		})
	}
}

func TestGraphCLIExposesEvidenceContext(t *testing.T) {
	graph, _, err := newRootCmd().Find([]string{"graph"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range graph.Commands() {
		if c.Name() == "evidence-context" {
			return
		}
	}
	t.Fatal("graph evidence-context is absent")
}
