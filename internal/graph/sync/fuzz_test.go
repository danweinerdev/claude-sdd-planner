package sync

// Fuzz coverage for the report parsers (Designs/SddGraph § Structural
// Verification): sync eats files produced by arbitrary CI systems, so the
// JUnit and go-test-json parsers are hostile-input consumers by design.
// Contract: structured errors only — refuse anything, panic on nothing,
// terminate on everything.
//
// Exploration runs locally (the corpus below replays as ordinary tests):
//
//	go test ./internal/graph/sync/ -fuzz FuzzJUnit -fuzztime 60s -run xxx
//	go test ./internal/graph/sync/ -fuzz FuzzGoTestJSON -fuzztime 60s -run xxx
//
// Discovered crashers land in testdata/fuzz/<Target>/ and replay on every
// plain `go test` run thereafter. Seed reports live in testdata/reports/:
// this repo's own `go test -json` output and a pytest-emitted JUnit file.

import (
	"os"
	"path/filepath"
	"testing"
)

var hostileXML = [][]byte{
	nil,
	[]byte(""),
	[]byte("<"),
	[]byte("<?xml version=\"1.0\"?>"),
	[]byte("<testsuites>"),
	[]byte("<testsuites></testsuite>"),
	[]byte("<testsuite><testcase/></testsuite>"),
	[]byte("<testsuite><testcase name=\"\"/></testsuite>"),
	[]byte("<testsuite><testcase name=\"a\"><failure/><skipped/></testcase></testsuite>"),
	[]byte("<testsuites><testsuite><testsuite><testcase name=\"nested\"/></testsuite></testsuite></testsuites>"),
	[]byte("<!DOCTYPE t [<!ENTITY e \"v\">]><testsuite><testcase name=\"&e;\"/></testsuite>"),
	[]byte("<testsuite\xff\xfe><testcase name=\"a\"/></testsuite>"),
	[]byte("<testsuite><testcase name=\"\xed\xa0\x80\"/></testsuite>"),
}

var hostileJSONStream = [][]byte{
	nil,
	[]byte(""),
	[]byte("\n\n\n"),
	[]byte("{"),
	[]byte("null\nnull\n"),
	[]byte(`{"Action": null}`),
	[]byte(`{"Action": 7, "Test": []}`),
	[]byte(`{"Action": "pass"}` + "\n" + `{"Action": "fail", "Test": "t"}` + "\ngarbage line\n"),
	[]byte(`{"Action": "pass", "Test": "t"}{"Action": "fail", "Test": "t"}`),
	[]byte(`{"Action": "output", "Output": "` + "\xff\xfe" + `"}`),
}

// seedReports loads the real-emitter seed files.
func seedReports(t testing.TB) map[string][]byte {
	out := map[string][]byte{}
	for _, name := range []string{"go-test.json", "junit-sample.xml"} {
		raw, err := os.ReadFile(filepath.Join("testdata", "reports", name))
		if err != nil {
			t.Fatalf("seed report missing: %v", err)
		}
		out[name] = raw
	}
	return out
}

func FuzzJUnit(f *testing.F) {
	for _, seed := range hostileXML {
		f.Add(seed)
	}
	f.Add(seedReports(f)["junit-sample.xml"])
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseJUnit(data)
	})
}

func FuzzGoTestJSON(f *testing.F) {
	for _, seed := range hostileJSONStream {
		f.Add(seed)
	}
	f.Add(seedReports(f)["go-test.json"])
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseGoTestJSON(data)
	})
}

// TestFuzzCorpusReports is the CI-friendly regression mode: hostile inputs,
// real seed reports, and the routing layer replay as ordinary tests.
func TestFuzzCorpusReports(t *testing.T) {
	seeds := seedReports(t)

	// The real seeds must PARSE — they are contract examples, not just
	// crash fodder.
	results, err := ParseReport("junit-sample.xml", seeds["junit-sample.xml"])
	if err != nil || len(results) != 4 {
		t.Fatalf("the pytest JUnit seed must parse to 4 cases: %d, %v", len(results), err)
	}
	results, err = ParseReport("go-test.json", seeds["go-test.json"])
	if err != nil || len(results) != 1 {
		t.Fatalf("the go-test-json seed must parse to 1 test id: %d, %v", len(results), err)
	}

	for i, data := range hostileXML {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("xml corpus[%d] panicked: %v\ninput: %q", i, r, data)
				}
			}()
			_, _ = ParseJUnit(data)
		}()
	}
	for i, data := range hostileJSONStream {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("json corpus[%d] panicked: %v\ninput: %q", i, r, data)
				}
			}()
			_, _ = ParseGoTestJSON(data)
		}()
	}

	// Routing refuses unknown formats by name, never sniffs.
	if _, err := ParseReport("report.tap", []byte("ok 1")); err == nil {
		t.Fatal("unknown report formats must refuse")
	}
}
