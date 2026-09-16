package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeToleratesAndDropsRemovedFields(t *testing.T) {
	raw := `{"version":1,"nodes":[{"id":"n","contract":"c","intent_hashes":{"x":"h"},"input_hashes":{},"red_evidence":{},"consumed_attempts":{},"report_evidence":{},"consumed_reports":{},"gate":{"type":"tests","evidence":"reported-v1","execution":{},"report":{},"tests":[{"id":"T","file":"t.go"}]},"hazards":[],"estimate":1,"verification":{"result":"pass","seq":1,"contract_rev":1,"artifact_digests":{},"dependency_digests":{},"input_hashes":{},"intent_hashes":{},"attempt":{},"report":{},"red_evidence":{},"reviewed":{"n":{"contract_rev":1,"artifact_digests":{}}},"isolation":"clean"}}]}`
	g, err := DecodeGraph([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := g.Encode()
	for _, key := range []string{"intent_hashes", "input_hashes", "artifact_digests", "dependency_digests", "red_evidence", "consumed_attempts", "report_evidence", "consumed_reports", "\"evidence\"", "\"execution\"", "\"report\""} {
		if strings.Contains(string(encoded), key) {
			t.Fatalf("removed key %s survived:\n%s", key, encoded)
		}
	}
}

func TestDecodeProposalRefusesRemovedFields(t *testing.T) {
	fields := []struct {
		name, value string
		gate        bool
	}{
		{"intent_hashes", `{}`, false}, {"input_hashes", `{}`, false},
		{"red_evidence", `{}`, false}, {"consumed_attempts", `{}`, false},
		{"report_evidence", `{}`, false}, {"consumed_reports", `{}`, false},
		{"evidence", `"reported-v1"`, true}, {"execution", `{}`, true}, {"report", `{}`, true},
	}
	for _, tc := range fields {
		t.Run(tc.name, func(t *testing.T) {
			nodeExtra, gateExtra := "", ""
			if tc.gate {
				gateExtra = fmt.Sprintf(",%q:%s", tc.name, tc.value)
			} else {
				nodeExtra = fmt.Sprintf(",%q:%s", tc.name, tc.value)
			}
			raw := fmt.Sprintf(`{"version":1,"nodes":[{"id":"n","contract":"c"%s,"gate":{"type":"tests","tests":[{"id":"T","file":"t.go"}]%s},"hazards":[],"estimate":1}]}`, nodeExtra, gateExtra)
			_, err := DecodeProposal([]byte(raw))
			if err == nil || !strings.Contains(err.Error(), "removed field") || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("%s: %v", tc.name, err)
			}
		})
	}
}

func TestDecodeEveryCommittedPlanGraph(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", ".plans", "Plans", "*", "*-Graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no committed plan graphs found")
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeGraph(raw); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestVerificationRedClassificationValidation(t *testing.T) {
	base := `{"version":1,"nodes":[{"id":"n","contract":"c","gate":{"type":"command"},"hazards":[],"estimate":1,"verification":{"result":"%s","seq":1,"isolation":"clean"%s}}]}`
	bad := []string{fmt.Sprintf(base, "pass", `,"red_kind":"baseline"`), fmt.Sprintf(base, "fail", `,"red_kind":"sensitivity"`), fmt.Sprintf(base, "fail", `,"red_kind":"baseline","fault":"x"`)}
	for _, raw := range bad {
		if _, err := DecodeGraph([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := DecodeGraph([]byte(fmt.Sprintf(base, "fail", `,"red_kind":"sensitivity","fault":"mutant"`))); err != nil {
		t.Fatal(err)
	}
}
