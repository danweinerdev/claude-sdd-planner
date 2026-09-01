// Package sync reconciles the graph against real test-runner output
// (Designs/SddGraph DD-5): the graph is never told what happened, it is
// shown the report and derives the rest. There is no assert-pass verb in
// this package or anywhere else — recording an observation requires bytes a
// runner produced.
//
// Reports arrive in two formats normalized to one shape: JUnit XML (the
// lingua franca pytest/ctest/gradle emit) and `go test -json` (the
// self-hosting repo's native stream). Reconciliation reports four honest
// buckets — updated, unresolved (declared but absent), untracked (present
// but claimed by no node), ambiguous (the same exact id both passed and
// failed; never guessed at) — and parametrized cases fold onto their
// declared id: every case must pass, one failure fails the fold (an
// observation, not an ambiguity), and any skip withholds it entirely.
package sync

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Outcome is one test's reported result.
type Outcome string

const (
	Pass Outcome = "pass"
	Fail Outcome = "fail"
	Skip Outcome = "skip"
)

// TestResult is one runner-reported test id and its outcome.
type TestResult struct {
	ID      string
	Outcome Outcome
}

// ParseReport routes on the report file's extension: .xml is JUnit, .json is
// a `go test -json` stream. Anything else is refused by name rather than
// sniffed — a report format is a contract, not a guess.
func ParseReport(filename string, raw []byte) ([]TestResult, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xml":
		return ParseJUnit(raw)
	case ".json":
		return ParseGoTestJSON(raw)
	default:
		return nil, fmt.Errorf("sync: unsupported report format %q; supply JUnit XML (.xml) or a `go test -json` stream (.json)", filepath.Ext(filename))
	}
}

// junit shapes: a report may be rooted at <testsuites> or a single
// <testsuite>, and suites nest in the wild.
type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Suites []junitSuite `xml:"testsuite"`
	Cases  []junitCase  `xml:"testcase"`
}

type junitCase struct {
	Name    string    `xml:"name,attr"`
	Failure *struct{} `xml:"failure"`
	Error   *struct{} `xml:"error"`
	Skipped *struct{} `xml:"skipped"`
}

// ParseJUnit normalizes a JUnit XML report. The test id is the case's `name`
// attribute exactly as the runner wrote it (`test_x[a]` stays `test_x[a]`).
func ParseJUnit(raw []byte) ([]TestResult, error) {
	var suites junitSuites
	if err := xml.Unmarshal(raw, &suites); err != nil {
		// A report rooted at a bare <testsuite>.
		var single junitSuite
		if err2 := xml.Unmarshal(raw, &single); err2 != nil {
			return nil, fmt.Errorf("sync: JUnit report does not parse: %v", err)
		}
		suites.Suites = []junitSuite{single}
	}
	var out []TestResult
	var walk func(s junitSuite)
	walk = func(s junitSuite) {
		for _, c := range s.Cases {
			r := TestResult{ID: c.Name, Outcome: Pass}
			switch {
			case c.Skipped != nil:
				r.Outcome = Skip
			case c.Failure != nil || c.Error != nil:
				r.Outcome = Fail
			}
			out = append(out, r)
		}
		for _, sub := range s.Suites {
			walk(sub)
		}
	}
	for _, s := range suites.Suites {
		walk(s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sync: JUnit report contains no test cases")
	}
	return out, nil
}

// goTestEvent is one line of a `go test -json` stream.
type goTestEvent struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
}

// ParseGoTestJSON normalizes a `go test -json` stream: the terminal action
// per test id wins (run/pause/cont are progress, pass/fail/skip are
// outcomes). Package-level events (empty Test) are not tests.
func ParseGoTestJSON(raw []byte) ([]TestResult, error) {
	outcomes := map[string]Outcome{}
	var order []string
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var ev goTestEvent
		if err := json.Unmarshal([]byte(text), &ev); err != nil {
			return nil, fmt.Errorf("sync: go test -json stream line %d does not parse: %v", line, err)
		}
		if ev.Test == "" {
			continue
		}
		var o Outcome
		switch ev.Action {
		case "pass":
			o = Pass
		case "fail":
			o = Fail
		case "skip":
			o = Skip
		default:
			continue
		}
		if _, seen := outcomes[ev.Test]; !seen {
			order = append(order, ev.Test)
		}
		outcomes[ev.Test] = o
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("sync: reading go test -json stream: %v", err)
	}
	if len(outcomes) == 0 {
		return nil, fmt.Errorf("sync: go test -json stream contains no test results")
	}
	out := make([]TestResult, 0, len(outcomes))
	for _, id := range order {
		out = append(out, TestResult{ID: id, Outcome: outcomes[id]})
	}
	return out, nil
}

// Fold is one declared test id resolved against a report.
type Fold struct {
	// Outcome is meaningful only when Resolved.
	Outcome Outcome
	// Resolved: the declared id produced a decidable outcome.
	Resolved bool
	// Withheld: cases existed but at least one was skipped — as unresolved
	// as a test that never ran, and never guessed at.
	Withheld bool
	// Ambiguous: the same exact id appeared with both pass and fail.
	Ambiguous bool
	// Matched lists the report ids that fed this fold, sorted.
	Matched []string
}

// caseOf reports whether reported is a parameter/subtest case of declared:
// pytest's `declared[...]` or Go's `declared/...`.
func caseOf(declared, reported string) bool {
	return strings.HasPrefix(reported, declared+"[") || strings.HasPrefix(reported, declared+"/")
}

// FoldFor resolves one declared id against the report per the design's
// rules: exact occurrences and cases fold together; every contributing
// result must pass for the fold to pass; one failure fails it (an
// observation); any skip withholds it; conflicting exact duplicates are
// ambiguous.
func FoldFor(declared string, results []TestResult) Fold {
	f := Fold{}
	exactSaw := map[Outcome]bool{}
	sawSkip, sawFail, sawAny := false, false, false
	for _, r := range results {
		exact := r.ID == declared
		if !exact && !caseOf(declared, r.ID) {
			continue
		}
		sawAny = true
		f.Matched = append(f.Matched, r.ID)
		if exact {
			exactSaw[r.Outcome] = true
		}
		switch r.Outcome {
		case Skip:
			sawSkip = true
		case Fail:
			sawFail = true
		}
	}
	sort.Strings(f.Matched)
	if !sawAny {
		return f
	}
	if exactSaw[Pass] && exactSaw[Fail] {
		f.Ambiguous = true
		return f
	}
	if sawSkip {
		f.Withheld = true
		return f
	}
	f.Resolved = true
	if sawFail {
		f.Outcome = Fail
	} else {
		f.Outcome = Pass
	}
	return f
}
