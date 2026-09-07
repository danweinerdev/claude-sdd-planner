package proposal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSameMillisecondFragmentsKeepStagingOrder(t *testing.T) {
	dir := stagedPlanDir(t)
	// UUIDv7's random suffix need not increase within one millisecond.
	for i, id := range []string{"node-a", "node-b", "node-c"} {
		candidate := fmt.Sprintf("00000000-0001-7000-8000-%012x", 3-i)
		if _, err := stageWithID(dir, []byte(fragmentWith(id)), func() string { return candidate }); err != nil {
			t.Fatal(err)
		}
	}
	_, merged, err := Assemble(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, n := range merged.Nodes {
		ids = append(ids, n.ID)
	}
	if strings.Join(ids, ",") != "node-a,node-b,node-c" {
		t.Fatalf("same-ms random suffix reordered staged nodes: %v", ids)
	}
}

func TestFragmentOrderSurvivesClockRollbackAndMissingState(t *testing.T) {
	dir := stagedPlanDir(t)
	first, err := stageWithID(dir, []byte(fragmentWith("first")), func() string { return "00000000-0002-7000-8000-000000000000" })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, ".graph", "fragment-order")); err != nil {
		t.Fatal(err)
	}
	second, err := stageWithID(dir, []byte(fragmentWith("second")), func() string { return "00000000-0001-7000-8000-000000000000" })
	if err != nil || second <= first {
		t.Fatalf("rollback/old workspace must retain order: first=%s second=%s error=%v", first, second, err)
	}
}

func TestFragmentOrderRefusesCorruptHighWater(t *testing.T) {
	dir := stagedPlanDir(t)
	if err := os.WriteFile(filepath.Join(dir, ".graph", "fragment-order"), []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(dir, []byte(validFragment)); err == nil || !strings.Contains(err.Error(), "invalid fragment-order") {
		t.Fatalf("corrupt allocator state must refuse: %v", err)
	}
	if got := stagedFragments(t, dir); got != 0 {
		t.Fatalf("refusal published %d fragments", got)
	}
}

func TestFragmentOrderConcurrentReservations(t *testing.T) {
	dir := stagedPlanDir(t)
	const count = 16
	var wg sync.WaitGroup
	paths := make(chan string, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := stageWithID(dir, []byte(fragmentWith(fmt.Sprintf("node-%d", i))), func() string { return "00000000-0001-7000-8000-000000000000" })
			if err != nil {
				t.Errorf("stage %d: %v", i, err)
				return
			}
			paths <- p
		}(i)
	}
	wg.Wait()
	close(paths)
	unique := map[string]bool{}
	for p := range paths {
		unique[p] = true
	}
	_, merged, err := Assemble(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(unique) != count || len(merged.Nodes) != count {
		t.Fatalf("lost/colliding publications: paths=%d nodes=%d want=%d", len(unique), len(merged.Nodes), count)
	}
}

func TestStageOrderProcessHelper(t *testing.T) {
	dir := os.Getenv("SDD_STAGE_PROBE_DIR")
	if dir == "" {
		return
	}
	if _, err := stageWithID(dir, []byte(fragmentWith(os.Getenv("SDD_STAGE_PROBE_ID"))), func() string { return "00000000-0001-7000-8000-000000000000" }); err != nil {
		t.Fatal(err)
	}
}

func TestFragmentOrderAcrossProcesses(t *testing.T) {
	dir := stagedPlanDir(t)
	for _, id := range []string{"node-a", "node-b", "node-c"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestStageOrderProcessHelper$")
		cmd.Env = append(os.Environ(), "SDD_STAGE_PROBE_DIR="+dir, "SDD_STAGE_PROBE_ID="+id)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child stage %s: %v\n%s", id, err, output)
		}
	}
	_, merged, err := Assemble(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, n := range merged.Nodes {
		ids = append(ids, n.ID)
	}
	if strings.Join(ids, ",") != "node-a,node-b,node-c" {
		t.Fatalf("separate invocations lost ordering: %v", ids)
	}
}

func TestFragmentIDCarriesWithoutChangingUUIDBits(t *testing.T) {
	for _, tc := range [][2]string{
		{"00000000-0001-7000-bfff-ffffffffffff", "00000000-0001-7001-8000-000000000000"},
		{"00000000-0001-7fff-bfff-ffffffffffff", "00000000-0002-7000-8000-000000000000"},
	} {
		got, err := nextFragmentID(tc[0])
		if err != nil || got != tc[1] {
			t.Fatalf("carry %s: got %s, error %v; want %s", tc[0], got, err, tc[1])
		}
		if _, err := parseFragmentID(got); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := nextFragmentID("ffffffff-ffff-7fff-bfff-ffffffffffff"); err == nil {
		t.Fatal("exhausted UUID space must refuse instead of wrapping")
	}
}
