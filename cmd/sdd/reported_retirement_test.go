package main

import "testing"

func TestProductionCLIHasNoTestCommand(t *testing.T) {
	for _, c := range newRootCmd().Commands() {
		if c.Name() == "test" {
			t.Fatal("production CLI still registers sdd test")
		}
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
