// Package testevidence validates reports produced by owned test executions.
package testevidence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

type SelectedTest struct {
	Package string `json:"package"`
	ID      string `json:"id"`
	File    string `json:"file"`
}

type TestResult struct {
	Package            string             `json:"package"`
	ID                 string             `json:"id"`
	QualifiedID        string             `json:"qualified_id"`
	File               string             `json:"file"`
	Outcome            string             `json:"outcome"`
	Cases              []string           `json:"cases"`
	ElapsedSeconds     float64            `json:"elapsed_seconds,omitempty"`
	CaseElapsedSeconds map[string]float64 `json:"case_elapsed_seconds,omitempty"`
}

type BuildEvent struct {
	Action     string `json:"action"`
	ImportPath string `json:"import_path"`
}

type Report struct {
	Result         string       `json:"result"`
	Tests          []TestResult `json:"tests"`
	Executed       int          `json:"executed"`
	PackageCount   int          `json:"package_count"`
	Selected       int          `json:"selected"`
	ElapsedSeconds float64      `json:"elapsed_seconds"`
	BuildEvents    []BuildEvent `json:"build_events,omitempty"`
}

func ParseGoReport(raw []byte, selected []SelectedTest, exitCode int) (Report, error) {
	if len(selected) == 0 {
		return Report{}, fmt.Errorf("Go report has no selected tests")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return Report{}, fmt.Errorf("Go report is empty")
	}
	if exitCode < 0 {
		return Report{}, fmt.Errorf("Go report has invalid exit code %d", exitCode)
	}

	selectedPackages := make(map[string]bool)
	selectionKeys := make(map[string]bool)
	for i, test := range selected {
		if strings.TrimSpace(test.Package) == "" || strings.TrimSpace(test.ID) == "" || strings.TrimSpace(test.File) == "" {
			return Report{}, fmt.Errorf("selected test %d has an empty package, id, or file", i)
		}
		key := qualified(test.Package, test.ID)
		if selectionKeys[key] {
			return Report{}, fmt.Errorf("selected test %s is duplicated", key)
		}
		selectionKeys[key] = true
		selectedPackages[test.Package] = true
	}

	tests := make(map[string]*testFacts)
	packages := make(map[string]*packageFacts)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	eventCount := 0
	var buildEvents []BuildEvent
	for {
		var event goEvent
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return Report{}, fmt.Errorf("decode Go report event %d: %w", eventCount+1, err)
		}
		eventCount++
		if event.Action == "build-output" || event.Action == "build-fail" {
			if event.ImportPath == "" || event.Package != "" || event.Test != "" {
				return Report{}, fmt.Errorf("Go report %s event %d lacks a valid import path", event.Action, eventCount)
			}
			buildEvents = append(buildEvents, BuildEvent{Action: event.Action, ImportPath: event.ImportPath})
			if event.Action == "build-fail" {
				return Report{}, fmt.Errorf("Go report build failed for import path %s", event.ImportPath)
			}
			continue
		}
		if event.Action == "" || event.Package == "" {
			return Report{}, fmt.Errorf("Go report event %d lacks action or package", eventCount)
		}
		pkg := packages[event.Package]
		if pkg == nil {
			pkg = &packageFacts{}
			packages[event.Package] = pkg
		}
		if pkg.terminal != "" {
			return Report{}, fmt.Errorf("Go report has event after terminal for package %s", event.Package)
		}

		switch event.Action {
		case "start":
			if event.Test != "" || pkg.started {
				return Report{}, fmt.Errorf("Go report has invalid or duplicate start for package %s", event.Package)
			}
			pkg.started = true
		case "output":
			if !pkg.started {
				return Report{}, fmt.Errorf("Go report output precedes package start for %s", event.Package)
			}
			if event.Test != "" {
				facts := tests[qualified(event.Package, event.Test)]
				if facts == nil || !facts.ran || facts.terminal != "" {
					return Report{}, fmt.Errorf("Go report output has no active run for %s", qualified(event.Package, event.Test))
				}
			}
		case "run":
			facts, err := activeTestFacts(tests, pkg, event)
			if err != nil {
				return Report{}, err
			}
			if facts.ran {
				return Report{}, fmt.Errorf("Go report repeats run for %s", qualified(event.Package, event.Test))
			}
			facts.ran = true
		case "pause", "cont":
			facts, err := activeTestFacts(tests, pkg, event)
			if err != nil {
				return Report{}, err
			}
			if !facts.ran || facts.terminal != "" {
				return Report{}, fmt.Errorf("Go report has %s without an active run for %s", event.Action, qualified(event.Package, event.Test))
			}
		case "pass", "fail", "skip":
			if !pkg.started {
				return Report{}, fmt.Errorf("Go report terminal precedes package start for %s", event.Package)
			}
			if event.Test == "" {
				pkg.terminal = event.Action
				break
			}
			facts, err := activeTestFacts(tests, pkg, event)
			if err != nil {
				return Report{}, err
			}
			if !facts.ran {
				return Report{}, fmt.Errorf("Go report has terminal without run for %s", qualified(event.Package, event.Test))
			}
			if facts.terminal != "" {
				return Report{}, fmt.Errorf("Go report repeats terminal for %s", qualified(event.Package, event.Test))
			}
			facts.terminal = event.Action
			facts.elapsed = event.Elapsed
		default:
			return Report{}, fmt.Errorf("Go report event %d has unknown action %q", eventCount, event.Action)
		}
	}
	if eventCount == 0 {
		return Report{}, fmt.Errorf("Go report contains no events")
	}

	for name, pkg := range packages {
		if !pkg.started || pkg.terminal == "" {
			return Report{}, fmt.Errorf("Go report package %s is incomplete", name)
		}
		if pkg.terminal == "fail" && !selectedPackages[name] {
			return Report{}, fmt.Errorf("unselected package %s failed", name)
		}
	}
	for key, facts := range tests {
		if !facts.ran || facts.terminal == "" {
			return Report{}, fmt.Errorf("Go report test %s is incomplete", key)
		}
	}

	executed := 0
	for key := range tests {
		pkg, id, _ := strings.Cut(key, "::")
		if belongsToSelection(pkg, id, selected) {
			executed++
		}
	}
	report := Report{Tests: make([]TestResult, 0, len(selected)), Executed: executed, Selected: len(selected), PackageCount: len(selectedPackages), BuildEvents: buildEvents}
	failedPackages := make(map[string]bool)
	for _, selection := range selected {
		key := qualified(selection.Package, selection.ID)
		facts := tests[key]
		if facts == nil || !facts.ran || facts.terminal == "" {
			return Report{}, fmt.Errorf("selected test %s did not run to a terminal outcome", key)
		}
		outcome := facts.terminal
		cases := make([]string, 0)
		caseElapsed := map[string]float64{}
		for testKey, child := range tests {
			pkg, id, _ := strings.Cut(testKey, "::")
			if pkg != selection.Package || !strings.HasPrefix(id, selection.ID+"/") {
				continue
			}
			if !child.ran || child.terminal == "" {
				return Report{}, fmt.Errorf("selected family %s has incomplete case %s", key, id)
			}
			cases = append(cases, id)
			caseElapsed[id] = child.elapsed
			if child.terminal == "skip" {
				outcome = "skip"
			} else if child.terminal == "fail" && outcome != "skip" {
				outcome = "fail"
			}
		}
		sort.Strings(cases)
		if outcome == "skip" {
			return Report{}, fmt.Errorf("selected test family %s was skipped", key)
		}
		if outcome == "fail" {
			failedPackages[selection.Package] = true
		}
		report.Tests = append(report.Tests, TestResult{
			Package: selection.Package, ID: selection.ID, QualifiedID: key,
			File: selection.File, Outcome: outcome, Cases: cases, ElapsedSeconds: facts.elapsed, CaseElapsedSeconds: caseElapsed,
		})
		report.ElapsedSeconds += facts.elapsed
	}

	anyFailure := false
	for name, pkg := range packages {
		switch pkg.terminal {
		case "pass":
		case "skip":
			if selectedPackages[name] {
				return Report{}, fmt.Errorf("selected package %s was skipped", name)
			}
		case "fail":
			anyFailure = true
			if !failedPackages[name] {
				return Report{}, fmt.Errorf("failed package %s is not accounted for by a selected test failure", name)
			}
		}
	}
	for key, facts := range tests {
		pkgName, id, _ := strings.Cut(key, "::")
		if facts.terminal == "fail" {
			if packages[pkgName].terminal != "fail" {
				return Report{}, fmt.Errorf("failed test %s belongs to a non-failing package", key)
			}
			if !belongsToSelection(pkgName, id, selected) {
				return Report{}, fmt.Errorf("unselected test %s failed", key)
			}
		}
	}
	if exitCode == 0 && anyFailure {
		return Report{}, fmt.Errorf("Go report failed despite zero process exit code")
	}
	if exitCode != 0 && !anyFailure {
		return Report{}, fmt.Errorf("nonzero process exit code %d has no accounted package failure", exitCode)
	}
	if anyFailure {
		report.Result = "fail"
	} else {
		report.Result = "pass"
	}
	return report, nil
}

type goEvent struct {
	Time       string  `json:"Time"`
	Action     string  `json:"Action"`
	Package    string  `json:"Package"`
	Test       string  `json:"Test"`
	Output     string  `json:"Output"`
	Elapsed    float64 `json:"Elapsed"`
	ImportPath string  `json:"ImportPath"`
}

type testFacts struct {
	ran      bool
	terminal string
	elapsed  float64
}

type packageFacts struct {
	started  bool
	terminal string
}

func activeTestFacts(tests map[string]*testFacts, pkg *packageFacts, event goEvent) (*testFacts, error) {
	if !pkg.started || event.Test == "" {
		return nil, fmt.Errorf("Go report %s event lacks an active package or test", event.Action)
	}
	key := qualified(event.Package, event.Test)
	facts := tests[key]
	if facts == nil {
		facts = &testFacts{}
		tests[key] = facts
	}
	return facts, nil
}

func qualified(pkg, id string) string { return pkg + "::" + id }

func belongsToSelection(pkg, id string, selected []SelectedTest) bool {
	for _, selection := range selected {
		if selection.Package != pkg {
			continue
		}
		if id == selection.ID || strings.HasPrefix(id, selection.ID+"/") || strings.HasPrefix(selection.ID, id+"/") {
			return true
		}
	}
	return false
}
