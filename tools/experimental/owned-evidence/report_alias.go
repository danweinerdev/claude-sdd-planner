package testevidence

import native "github.com/danweinerdev/claude-sdd-planner/v2/internal/testevidence"

type SelectedTest struct {
	Package string `json:"package"`
	ID      string `json:"id"`
	File    string `json:"file"`
}
type TestResult = native.TestResult
type Report = native.Report

func ParseGoReport(raw []byte, selected []SelectedTest, exit int) (Report, error) {
	in := make([]native.SelectedTest, len(selected))
	for i, s := range selected {
		in[i] = native.SelectedTest{Package: s.Package, ID: s.ID, File: s.File}
	}
	return native.ParseGoReport(raw, in, exit)
}
