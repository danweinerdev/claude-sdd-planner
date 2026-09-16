// Package evidencecost records optional, per-invocation costs for observed test
// evidence and graph synchronization. It is deliberately a small caller-owned
// recorder, not a process-wide tracing facility.
package evidencecost

// Phase is a closed timing label. Phase durations are inclusive and phases may
// nest, so callers must never sum PhaseNS. TotalNS is separate wall time.
type Phase string

const (
	RootInputPreparation Phase = "root_input_preparation"
	ProfileResolution    Phase = "profile_resolution"
	GoProbes             Phase = "go_probes"
	ExecutableHash       Phase = "executable_hash"
	CandidateSnapshot    Phase = "candidate_snapshot"
	ReportParse          Phase = "report_parse"
	BundleIO             Phase = "bundle_io"
	TestExecution        Phase = "test_execution"
	Provenance           Phase = "provenance"
	LegacyAnchors        Phase = "legacy_anchors"
	Publication          Phase = "publication"
	WorkspaceRelease     Phase = "workspace_release"
)

// Counter is a closed numeric label. Labels describe logical requests at the
// named call sites; hash request counts are not claims about kernel I/O.
type Counter string

const (
	GraphLoadRequests        Counter = "graph_load_requests"
	GraphReadRequests        Counter = "graph_read_requests"
	GraphWriteRequests       Counter = "graph_write_requests"
	ProfileResolutions       Counter = "profile_resolutions"
	GoProbeRequests          Counter = "go_probe_requests"
	ExecutableHashRequests   Counter = "executable_hash_requests"
	RootHashRequests         Counter = "root_hash_requests"
	InputResolutions         Counter = "input_resolutions"
	BundleReadRequests       Counter = "bundle_read_requests"
	ReportParses             Counter = "report_parses"
	TestExecutionRequests    Counter = "test_execution_requests"
	TestProcessesCompleted   Counter = "test_processes_completed"
	LegacyAnchorScans        Counter = "legacy_anchor_scans"
	PublicationCallbacks     Counter = "publication_callbacks"
	CASConflicts             Counter = "cas_conflicts"
	CASRetries               Counter = "cas_retries"
	HistoricalReplays        Counter = "historical_replays"
	VCSQueryRequests         Counter = "vcs_query_requests"
	WorkspaceReleaseRequests Counter = "workspace_release_requests"
)

// Summary is an anonymous numeric snapshot suitable for additive JSON output.
type Summary struct {
	TotalNS  int64              `json:"total_ns"`
	PhaseNS  map[Phase]int64    `json:"phase_ns"`
	Counters map[Counter]uint64 `json:"counters"`
}
