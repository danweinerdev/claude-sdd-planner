package model

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

func observedNode() Node {
	return Node{ID: "work", Contract: "works", Hazards: Hazards{}, Estimate: 1,
		Artifacts: []string{"work.go", "work_test.go", "fixture.txt"},
		Inputs:    []Input{{Root: InputRootRepository, Path: "support.go"}},
		Gate: Gate{Type: GateTests, Evidence: EvidenceObservedV1,
			Execution: &ExecutionProfile{Adapter: "go-test-v1", Args: []string{"-race", "-tags", "integration"}, TimeoutSeconds: 30,
				TestSupportInputs: []string{InputKey(Input{Root: InputRootRepository, Path: "support.go"})}, TestSupportArtifacts: []string{"fixture.txt"}},
			Tests: []Test{{ID: "TestWork", File: "work_test.go"}}}}
}

func TestObservedEvidenceRoundTripAndProofFingerprint(t *testing.T) {
	n := observedNode()
	n.Claim = &Claim{By: "worker", LeaseExpires: "2026-09-14T12:00:00Z", Instance: "nonce"}
	n.RedEvidence = map[string]RedEvidence{"pkg.TestWork": {AttemptID: "a1", Seq: 2, CompatibilityKey: "key", Kind: "baseline"}}
	n.ConsumedAttempts = map[string]ConsumedAttempt{"a1": {Digest: "sha256:a", Seq: 2, Result: ResultFail}}
	n.Verification = &Verification{Result: ResultPass, Seq: 3, Isolation: IsolationClean,
		Attempt: &AttemptSummary{ID: "a2", Digest: "sha256:b", Protocol: EvidenceObservedV1, ClaimInstance: "nonce", By: "worker", Phase: "green", Started: "s", Completed: "c", CandidateDigest: "sha256:c"}}
	g := &Graph{Version: SchemaVersion, Nodes: []Node{n}}
	raw, err := g.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeGraph(raw)
	if err != nil {
		t.Fatalf("decode round trip: %v\n%s", err, raw)
	}
	if got.Nodes[0].Gate.Execution.Adapter != "go-test-v1" || got.Nodes[0].Claim.Instance != "nonce" || got.Nodes[0].Verification.Attempt.ID != "a2" {
		t.Fatalf("observed fields were lost: %+v", got.Nodes[0])
	}
	base := n.ProofSnapshot()
	n.Gate.Execution.TimeoutSeconds++
	if n.ProofSnapshot() == base {
		t.Fatal("profile-only change must alter ProofSnapshot")
	}
}

func TestObservedGateValidationAndProposalToolOwnership(t *testing.T) {
	n := observedNode()
	if problems := ValidateEvidenceGate(&n); len(problems) != 0 {
		t.Fatalf("valid declaration: %v", problems)
	}
	n.Gate.Execution.Args = []string{"-run", "TestWork", "-race", "-race"}
	if got := strings.Join(ValidateEvidenceGate(&n), "\n"); !strings.Contains(got, "not allowed") || !strings.Contains(got, "repeat") {
		t.Fatalf("unsafe args not fully refused: %s", got)
	}
	for _, field := range []string{"red_evidence", "consumed_attempts"} {
		raw := `{"version":1,"nodes":[{"id":"x","contract":"x","gate":{"type":"tests","tests":[{"id":"T","file":"x_test.go"}]},"hazards":[],"` + field + `":{}}]}`
		if _, err := DecodeProposal([]byte(raw)); err == nil || !strings.Contains(err.Error(), "tool-owned") {
			t.Fatalf("proposal field %s was not refused: %v", field, err)
		}
	}
}

func TestObservedGateRejectsUnrepresentableTimeouts(t *testing.T) {
	if strconv.IntSize == 32 {
		t.Skip("duration boundary exceeds a 32-bit int; platform overflow is covered separately")
	}
	n := observedNode()
	maxTimeout := MaxExecutionTimeoutSeconds
	n.Gate.Execution.TimeoutSeconds = int(maxTimeout)
	if problems := ValidateEvidenceGate(&n); len(problems) != 0 {
		t.Fatalf("maximum representable timeout refused: %v", problems)
	}
	n.Gate.Execution.TimeoutSeconds++
	if got := strings.Join(ValidateEvidenceGate(&n), "\n"); !strings.Contains(got, "representable") {
		t.Fatalf("overflowing timeout was not refused: %s", got)
	}

	raw := fmt.Sprintf(`{"version":1,"nodes":[{"id":"x","contract":"x","gate":{"type":"tests","evidence":"observed-v1","execution":{"adapter":"go-test-v1","timeout_seconds":%d},"tests":[{"id":"T","file":"x_test.go"}]},"hazards":[],"artifacts":["x_test.go"]}]}`, maxTimeout)
	g, err := DecodeGraph([]byte(raw))
	if err != nil {
		t.Fatalf("decode boundary: %v", err)
	}
	encoded, err := (&Graph{Version: SchemaVersion, Nodes: g.Nodes}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := DecodeGraph(encoded)
	if err != nil {
		t.Fatalf("boundary round trip decode: %v", err)
	}
	if got := int64(roundTrip.Nodes[0].Gate.Execution.TimeoutSeconds); got != maxTimeout {
		t.Fatalf("boundary round trip timeout=%d, want %d", got, maxTimeout)
	}

	huge := strings.Replace(raw, strconv.FormatInt(maxTimeout, 10), strconv.FormatInt(math.MaxInt64, 10), 1)
	if _, err := DecodeGraph([]byte(huge)); err == nil || !strings.Contains(err.Error(), "representable") {
		t.Fatalf("huge timeout was not refused before duration conversion: %v", err)
	}
}

func TestDecoderRejectsPlatformIntOverflowBeforeCast(t *testing.T) {
	if strconv.IntSize != 32 {
		t.Skip("32-bit decoder boundary")
	}
	raw := `{"version":1,"nodes":[{"id":"x","contract":"x","estimate":2147483648,"gate":{"type":"tests","tests":[{"id":"T","file":"x_test.go"}]},"hazards":[]}]}`
	if _, err := DecodeProposal([]byte(raw)); err == nil || !strings.Contains(err.Error(), "not representable") {
		t.Fatalf("decoder accepted an overflowing int: %v", err)
	}
}

func TestObservedGateRejectsUnsafeAndDuplicateEnvironmentAndSupportNames(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ExecutionProfile)
		want   string
	}{
		{"empty environment", func(p *ExecutionProfile) { p.EnvironmentKeys = []string{""} }, "environment_keys"},
		{"invalid environment", func(p *ExecutionProfile) { p.EnvironmentKeys = []string{"BAD=VALUE"} }, "environment_keys"},
		{"nul environment", func(p *ExecutionProfile) { p.EnvironmentKeys = []string{"BAD\x00NAME"} }, "environment_keys"},
		{"case-folded duplicate environment", func(p *ExecutionProfile) { p.EnvironmentKeys = []string{"GOOS", "goos"} }, "duplicate"},
		{"duplicate support input", func(p *ExecutionProfile) {
			p.TestSupportInputs = []string{"repository:support.go", "repository:support.go"}
		}, "duplicate"},
		{"duplicate support artifact", func(p *ExecutionProfile) { p.TestSupportArtifacts = []string{"fixture.txt", "fixture.txt"} }, "duplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := observedNode()
			tc.mutate(n.Gate.Execution)
			if got := strings.Join(ValidateEvidenceGate(&n), "\n"); !strings.Contains(got, tc.want) {
				t.Fatalf("missing %q refusal: %s", tc.want, got)
			}
		})
	}

	legacy := observedNode()
	legacy.Gate.Evidence = ""
	legacy.Gate.Execution = nil
	if problems := ValidateEvidenceGate(&legacy); len(problems) != 0 {
		t.Fatalf("absent legacy evidence fields changed meaning: %v", problems)
	}
}

func TestEvidenceGateRejectsFieldsOnWrongGateType(t *testing.T) {
	cases := []struct {
		name string
		gate Gate
		want string
	}{
		{"tests command", Gate{Type: GateTests, Command: "true"}, "command"},
		{"tests lanes", Gate{Type: GateTests, Lanes: Lanes{"review_quality"}}, "lanes"},
		{"command tests", Gate{Type: GateCommand, Tests: []Test{{ID: "T", File: "t.go"}}}, "tests"},
		{"command execution", Gate{Type: GateCommand, Execution: &ExecutionProfile{}}, "execution"},
		{"review command", Gate{Type: GateReview, Command: "true"}, "command"},
		{"review evidence", Gate{Type: GateReview, Evidence: EvidenceLegacy}, "evidence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := Node{Gate: tc.gate}
			if got := strings.Join(ValidateEvidenceGate(&n), "\n"); !strings.Contains(got, tc.want) {
				t.Fatalf("missing wrong-type field refusal %q: %s", tc.want, got)
			}
		})
	}
}
