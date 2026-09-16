package model

import (
	"strings"
	"testing"
)

func TestReportedEvidenceGateContract(t *testing.T) {
	n := &Node{Artifacts: []string{"work_test.go"}, Gate: Gate{
		Type: GateTests, Evidence: EvidenceReportedV1,
		Report: &ReportProfile{Format: "go-test-json-v1", Runner: "repository-unit-tests", EnvironmentKeys: []string{}, TestSupportInputs: []string{}, TestSupportArtifacts: []string{}},
		Tests:  []Test{{Package: "example.test/work", ID: "TestWork", File: "work_test.go"}},
	}}
	if problems := ValidateEvidenceGate(n); len(problems) != 0 {
		t.Fatalf("valid reported-v1 gate: %v", problems)
	}
	n.Gate.Execution = &ExecutionProfile{Adapter: "go-test-v1", TimeoutSeconds: 1}
	if got := strings.Join(ValidateEvidenceGate(n), "; "); !strings.Contains(got, "execution is forbidden") {
		t.Fatalf("reported-v1 execution accepted: %q", got)
	}
}

func TestReportedProfileRequiresExplicitUniqueArrays(t *testing.T) {
	base := `{"version":1,"nodes":[{"id":"x","contract":"x","gate":{"type":"tests","evidence":"reported-v1","report":REPORT,"tests":[{"package":"example.test/p","id":"T","file":"x_test.go"}]},"hazards":[],"artifacts":["x_test.go"]}]}`
	valid := `{"format":"go-test-json-v1","runner":"repo","environment_keys":[],"test_support_inputs":[],"test_support_artifacts":[]}`
	for name, report := range map[string]string{
		"omitted":           `{"format":"go-test-json-v1","runner":"repo"}`,
		"null":              strings.Replace(valid, `"environment_keys":[]`, `"environment_keys":null`, 1),
		"duplicate support": `{"format":"go-test-json-v1","runner":"repo","environment_keys":[],"test_support_inputs":[],"test_support_artifacts":["x_test.go","x_test.go"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeProposal([]byte(strings.Replace(base, "REPORT", report, 1))); err == nil {
				t.Fatal("invalid report profile accepted")
			}
		})
	}
	p, err := DecodeProposal([]byte(strings.Replace(base, "REPORT", valid, 1)))
	if err != nil {
		t.Fatal(err)
	}
	r := p.Nodes[0].Gate.Report
	if r.EnvironmentKeys == nil || r.TestSupportInputs == nil || r.TestSupportArtifacts == nil {
		t.Fatal("explicit arrays were not preserved")
	}
	if got := ValidateEvidenceGate(&Node{Artifacts: []string{"x_test.go"}, Gate: Gate{Type: GateTests, Evidence: EvidenceReportedV1, Report: &ReportProfile{Format: "go-test-json-v1", Runner: "repo"}, Tests: []Test{{Package: "example.test/p", ID: "T", File: "x_test.go"}}}}); len(got) != 3 {
		t.Fatalf("in-memory omitted arrays accepted: %v", got)
	}
}

func TestProposalRefusesRetiredObservedEvidence(t *testing.T) {
	raw := `{"version":1,"nodes":[{"id":"x","contract":"x","gate":{"type":"tests","evidence":"observed-v1","execution":{"adapter":"go-test-v1","timeout_seconds":1},"tests":[{"id":"T","file":"x_test.go"}]},"hazards":[],"artifacts":["x_test.go"]}]}`
	if _, err := DecodeProposal([]byte(raw)); err == nil || !strings.Contains(err.Error(), "reported-v1") {
		t.Fatalf("retired observed proposal accepted: %v", err)
	}
	if _, err := DecodeGraph([]byte(raw)); err != nil {
		t.Fatalf("historical stored observed graph became unreadable: %v", err)
	}
}
