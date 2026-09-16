package evidencecost

import (
	"sync"
	"time"
)

// Recorder owns costs for one invocation. Its methods are nil-safe.
type Recorder struct {
	mu       sync.Mutex
	now      func() time.Time
	started  time.Time
	phaseNS  map[Phase]int64
	counters map[Counter]uint64
}

// New starts a recorder. A nil clock uses time.Now.
func New(now func() time.Time) *Recorder {
	if now == nil {
		now = time.Now
	}
	return &Recorder{now: now, started: now(), phaseNS: map[Phase]int64{}, counters: map[Counter]uint64{}}
}

// Start begins an inclusive phase span and returns an idempotent end closure.
// Spans carry their own start time; there is no shared span stack.
func (r *Recorder) Start(phase Phase) func() {
	if r == nil || !knownPhase(phase) {
		return func() {}
	}
	started := r.now()
	var once sync.Once
	return func() {
		once.Do(func() {
			d := r.now().Sub(started).Nanoseconds()
			if d < 0 {
				d = 0
			}
			r.mu.Lock()
			r.phaseNS[phase] += d
			r.mu.Unlock()
		})
	}
}

// Inc increments a known counter once.
func (r *Recorder) Inc(counter Counter) {
	if r == nil || !knownCounter(counter) {
		return
	}
	r.mu.Lock()
	r.counters[counter]++
	r.mu.Unlock()
}

func knownPhase(p Phase) bool {
	switch p {
	case RootInputPreparation, ProfileResolution, GoProbes, ExecutableHash, CandidateSnapshot, ReportParse, BundleIO, TestExecution, Provenance, LegacyAnchors, Publication, WorkspaceRelease:
		return true
	default:
		return false
	}
}

func knownCounter(c Counter) bool {
	switch c {
	case GraphLoadRequests, GraphReadRequests, GraphWriteRequests, ProfileResolutions, GoProbeRequests, ExecutableHashRequests, RootHashRequests, InputResolutions, BundleReadRequests, ReportParses, TestExecutionRequests, TestProcessesCompleted, LegacyAnchorScans, PublicationCallbacks, CASConflicts, CASRetries, HistoricalReplays, VCSQueryRequests, WorkspaceReleaseRequests:
		return true
	default:
		return false
	}
}

// Snapshot returns independent map copies. TotalNS is wall time since New.
func (r *Recorder) Snapshot() Summary {
	if r == nil {
		return Summary{}
	}
	total := r.now().Sub(r.started).Nanoseconds()
	if total < 0 {
		total = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Summary{TotalNS: total, PhaseNS: make(map[Phase]int64, len(r.phaseNS)), Counters: make(map[Counter]uint64, len(r.counters))}
	for k, v := range r.phaseNS {
		s.PhaseNS[k] = v
	}
	for k, v := range r.counters {
		s.Counters[k] = v
	}
	return s
}
