package evidencecost

import (
	"fmt"
	"strings"
)

var phaseOrder = []Phase{RootInputPreparation, ProfileResolution, GoProbes, ExecutableHash, CandidateSnapshot, ReportParse, BundleIO, TestExecution, Provenance, LegacyAnchors, Publication, WorkspaceRelease}
var counterOrder = []Counter{GraphLoadRequests, GraphReadRequests, GraphWriteRequests, ProfileResolutions, GoProbeRequests, ExecutableHashRequests, RootHashRequests, InputResolutions, BundleReadRequests, ReportParses, TestExecutionRequests, TestProcessesCompleted, LegacyAnchorScans, PublicationCallbacks, CASConflicts, CASRetries, HistoricalReplays, VCSQueryRequests, WorkspaceReleaseRequests}

// Format renders stable, concise human output. Phase values are inclusive and
// may overlap; the first line explicitly warns against summing them.
func Format(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cost: total_ns=%d (phase_ns is inclusive; do not sum)\n", s.TotalNS)
	for _, p := range phaseOrder {
		if v, ok := s.PhaseNS[p]; ok {
			fmt.Fprintf(&b, "cost phase %s=%d ns\n", p, v)
		}
	}
	for _, c := range counterOrder {
		if v, ok := s.Counters[c]; ok {
			fmt.Fprintf(&b, "cost count %s=%d\n", c, v)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}
