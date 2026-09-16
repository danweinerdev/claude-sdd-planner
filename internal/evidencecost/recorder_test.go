package evidencecost

import (
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func TestRecorderNestedInclusiveAndIdempotent(t *testing.T) {
	c := &fakeClock{t: time.Unix(0, 0)}
	r := New(c.now)
	outer := r.Start(ProfileResolution)
	c.add(2 * time.Nanosecond)
	inner := r.Start(GoProbes)
	c.add(3 * time.Nanosecond)
	inner()
	inner()
	c.add(5 * time.Nanosecond)
	outer()
	outer()
	s := r.Snapshot()
	if s.TotalNS != 10 || s.PhaseNS[ProfileResolution] != 10 || s.PhaseNS[GoProbes] != 3 {
		t.Fatalf("inclusive snapshot = %+v", s)
	}
}

func TestRecorderConcurrentCountersAndSnapshotCopies(t *testing.T) {
	r := New(nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r.Inc(ReportParses)
			}
		}()
	}
	wg.Wait()
	one := r.Snapshot()
	if one.Counters[ReportParses] != 2000 {
		t.Fatalf("count = %d", one.Counters[ReportParses])
	}
	one.Counters[ReportParses] = 0
	one.PhaseNS[BundleIO] = 99
	two := r.Snapshot()
	if two.Counters[ReportParses] != 2000 || two.PhaseNS[BundleIO] != 0 {
		t.Fatalf("snapshot aliases recorder: %+v", two)
	}
}

func TestNilRecorderIsSafe(t *testing.T) {
	var r *Recorder
	r.Inc(ReportParses)
	r.Start(BundleIO)()
	if got := r.Snapshot(); got.TotalNS != 0 || got.PhaseNS != nil || got.Counters != nil {
		t.Fatalf("nil snapshot = %+v", got)
	}
}

func TestRecorderRejectsUnknownLabels(t *testing.T) {
	r := New(nil)
	r.Inc(Counter("filename"))
	r.Start(Phase("node-id"))()
	s := r.Snapshot()
	if len(s.Counters) != 0 || len(s.PhaseNS) != 0 {
		t.Fatalf("unknown labels escaped into snapshot: %+v", s)
	}
}
