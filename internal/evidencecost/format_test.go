package evidencecost

import (
	"reflect"
	"strings"
	"testing"
)

func TestFormatStableOrderingAndMeaning(t *testing.T) {
	s := Summary{TotalNS: 9, PhaseNS: map[Phase]int64{Publication: 2, BundleIO: 1}, Counters: map[Counter]uint64{CASRetries: 4, GraphLoadRequests: 3}}
	got := Format(s)
	if !strings.Contains(got, "inclusive; do not sum") {
		t.Fatalf("missing inclusive meaning: %s", got)
	}
	bundleAt, publicationAt := strings.Index(got, "bundle_io"), strings.Index(got, "publication")
	loadAt, retryAt := strings.Index(got, "graph_load_requests"), strings.Index(got, "cas_retries")
	if bundleAt < 0 || publicationAt < 0 || loadAt < 0 || retryAt < 0 {
		t.Fatalf("formatter omitted requested labels: %s", got)
	}
	if bundleAt > publicationAt || loadAt > retryAt {
		t.Fatalf("unstable ordering: %s", got)
	}
}

func TestFormatterOrderCoversClosedVocabulary(t *testing.T) {
	wantPhases := []Phase{RootInputPreparation, ProfileResolution, GoProbes, ExecutableHash, CandidateSnapshot, ReportParse, BundleIO, TestExecution, Provenance, LegacyAnchors, Publication, WorkspaceRelease}
	wantCounters := []Counter{GraphLoadRequests, GraphReadRequests, GraphWriteRequests, ProfileResolutions, GoProbeRequests, ExecutableHashRequests, RootHashRequests, InputResolutions, BundleReadRequests, ReportParses, TestExecutionRequests, TestProcessesCompleted, LegacyAnchorScans, PublicationCallbacks, CASConflicts, CASRetries, HistoricalReplays, VCSQueryRequests, WorkspaceReleaseRequests}
	if !reflect.DeepEqual(phaseOrder, wantPhases) {
		t.Fatalf("phase formatter vocabulary = %v, want %v", phaseOrder, wantPhases)
	}
	if !reflect.DeepEqual(counterOrder, wantCounters) {
		t.Fatalf("counter formatter vocabulary = %v, want %v", counterOrder, wantCounters)
	}
}
