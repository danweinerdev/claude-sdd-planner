package states

import (
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func reportedStateGraph() *model.Graph {
	d := func(c byte) string {
		b := make([]byte, 64)
		for i := range b {
			b[i] = c
		}
		return "sha256:" + string(b)
	}
	redID, greenID, reportRed, reportGreen, metadataRed, metadataGreen, candidate, compatibility := d('a'), d('b'), d('c'), d('d'), d('e'), d('f'), d('1'), d('2')
	qid := "example.test/p::TestWork"
	red := model.RedEvidence{ReportID: redID, Seq: 1, CompatibilityKey: compatibility, Kind: "baseline"}
	redLinks := map[string]model.RedEvidence{qid: red}
	n := model.Node{ID: "n", Contract: "c", Hazards: model.Hazards{"wrong-result"}, Estimate: 1, Gate: model.Gate{Type: model.GateTests, Evidence: model.EvidenceReportedV1, Report: &model.ReportProfile{Format: "go-test-json-v1", Runner: "repo", EnvironmentKeys: []string{}, TestSupportInputs: []string{}, TestSupportArtifacts: []string{}}, Tests: []model.Test{{Package: "example.test/p", ID: "TestWork", File: "work_test.go", Satisfies: []string{"wrong-result"}}}}, RedSeqs: map[string]int{"TestWork": 1}, RedEvidence: redLinks}
	n.ReportEvidence = map[string]model.ReportEvidence{
		redID:   {Protocol: model.EvidenceReportedV1, ReportDigest: reportRed, MetadataDigest: metadataRed, ClaimInstance: "nonce", By: "worker", Phase: "red", RedKind: "baseline", CandidateDigest: candidate, CompatibilityHash: compatibility},
		greenID: {Protocol: model.EvidenceReportedV1, ReportDigest: reportGreen, MetadataDigest: metadataGreen, ClaimInstance: "nonce", By: "worker", Phase: "green", CandidateDigest: candidate, CompatibilityHash: compatibility},
	}
	n.ConsumedReports = map[string]model.ConsumedReport{
		redID:   {Protocol: model.EvidenceReportedV1, ReportDigest: reportRed, MetadataDigest: metadataRed, ClaimInstance: "nonce", By: "worker", Phase: "red", CandidateDigest: candidate, Seq: 1, Result: model.ResultFail, RedEvidence: redLinks},
		greenID: {Protocol: model.EvidenceReportedV1, ReportDigest: reportGreen, MetadataDigest: metadataGreen, ClaimInstance: "nonce", By: "worker", Phase: "green", CandidateDigest: candidate, Seq: 2, Result: model.ResultPass, RedEvidence: redLinks},
	}
	n.Verification = &model.Verification{Result: model.ResultPass, Seq: 2, ContractRev: 1, DependencyDigests: map[string]map[string]string{}, InputHashes: map[string]string{}, IntentHashes: map[string]string{}, ReportDigest: reportGreen, Isolation: model.IsolationClean, Report: &model.ReportSummary{ID: greenID, Protocol: model.EvidenceReportedV1, ReportDigest: reportGreen, MetadataDigest: metadataGreen, ClaimInstance: "nonce", By: "worker", Phase: "green", CandidateDigest: candidate}, RedEvidence: redLinks}
	return &model.Graph{Version: 1, SeqCounter: 2, Nodes: []model.Node{n}}
}

func TestReportedPassRequiresCompleteLinkedLineage(t *testing.T) {
	if got := Derive(Inputs{Graph: reportedStateGraph()})["n"].State; got != Green {
		t.Fatalf("valid lineage = %s", got)
	}
	cases := map[string]func(*model.Node){
		"summary phase":  func(n *model.Node) { n.Verification.Report.Phase = "red" },
		"summary holder": func(n *model.Node) { n.Verification.Report.By = "other" },
		"evidence digest": func(n *model.Node) {
			e := n.ReportEvidence[n.Verification.Report.ID]
			e.MetadataDigest = "sha256:bad"
			n.ReportEvidence[n.Verification.Report.ID] = e
		},
		"consumed nonce": func(n *model.Node) {
			c := n.ConsumedReports[n.Verification.Report.ID]
			c.ClaimInstance = "other"
			n.ConsumedReports[n.Verification.Report.ID] = c
		},
		"consumed phase": func(n *model.Node) {
			c := n.ConsumedReports[n.Verification.Report.ID]
			c.Phase = "red"
			n.ConsumedReports[n.Verification.Report.ID] = c
		},
		"red link": func(n *model.Node) {
			for qid, r := range n.Verification.RedEvidence {
				r.Seq = 9
				n.Verification.RedEvidence[qid] = r
			}
		},
		"hazard red omitted everywhere": func(n *model.Node) {
			n.Verification.RedEvidence = nil
			c := n.ConsumedReports[n.Verification.Report.ID]
			c.RedEvidence = nil
			n.ConsumedReports[n.Verification.Report.ID] = c
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			g := reportedStateGraph()
			mutate(&g.Nodes[0])
			if got := Derive(Inputs{Graph: g})["n"]; got.State == Green || !got.ObservedEvidenceStale {
				t.Fatalf("malformed lineage derived %+v", got)
			}
		})
	}
}
