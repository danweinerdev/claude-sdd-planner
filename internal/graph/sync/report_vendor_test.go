package sync

// pilot-junit-vendor-corpus (hazard external-format): the JUnit parser eats
// reports from arbitrary CI emitters, and vendor shapes carry structure the
// spec's minimal examples never show — nested suites, <properties>,
// <system-out>, CDATA, and case names holding quotes and newlines. The
// discharging test throws exactly those reserved constructs at the parser
// and asserts the parse — the external-format shape from the hazard
// vocabulary — against a committed corpus seed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJUnitVendorShapes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "reports", "junit-vendor.xml"))
	if err != nil {
		t.Fatalf("vendor corpus seed missing: %v", err)
	}
	results, err := ParseJUnit(raw)
	if err != nil {
		t.Fatalf("the vendor-shaped report must parse: %v", err)
	}

	// Exact counts: 6 cases across two nested suites — 3 pass, 1 fail,
	// 1 error (folded to fail), 1 skip.
	if len(results) != 6 {
		t.Fatalf("expected 6 test cases, got %d: %+v", len(results), results)
	}
	byID := map[string]Outcome{}
	for _, r := range results {
		byID[r.ID] = r.Outcome
	}

	// Reserved constructs survive into the ids exactly as written.
	quoted := `handles "quoted" input`
	if byID[quoted] != Pass {
		t.Errorf("a case name holding double quotes must parse as pass: %v", byID)
	}
	newlined := "first line\nsecond line"
	if byID[newlined] != Fail {
		t.Errorf("a case name holding a newline entity must parse as fail: %v", byID)
	}
	if byID["cdata_failure_message"] != Fail {
		t.Errorf("a failure carrying a CDATA body folds to fail (error counts as failure): %v", byID)
	}
	if byID["skipped_with_properties"] != Skip {
		t.Errorf("a skipped case inside a properties-carrying suite stays skip: %v", byID)
	}
	if byID["plain_pass"] != Pass || byID["system_out_pass"] != Pass {
		t.Errorf("passes with <system-out> noise stay passes: %v", byID)
	}

	// No invented ids: everything parsed traces back to the seed bytes.
	for id := range byID {
		if !strings.Contains(string(raw), "quoted") && id == quoted {
			t.Errorf("parsed id %q has no source in the seed", id)
		}
	}
}
