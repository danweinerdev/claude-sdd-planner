package testevidence

import (
	"fmt"
	"strings"
	"testing"
)

type jsonEvent struct {
	action     string
	pkg        string
	importPath string
	test       string
	output     string
	elapsed    string
}

func stream(events ...jsonEvent) []byte {
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, `{"Time":"2026-09-14T00:00:00Z","Action":%q`, e.action)
		if e.pkg != "" {
			fmt.Fprintf(&b, `,"Package":%q`, e.pkg)
		}
		if e.importPath != "" {
			fmt.Fprintf(&b, `,"ImportPath":%q`, e.importPath)
		}
		if e.test != "" {
			fmt.Fprintf(&b, `,"Test":%q`, e.test)
		}
		if e.output != "" {
			fmt.Fprintf(&b, `,"Output":%q`, e.output)
		}
		if e.elapsed != "" {
			fmt.Fprintf(&b, `,"Elapsed":%s`, e.elapsed)
		}
		b.WriteString("}\n")
	}
	return []byte(b.String())
}

func packageRun(pkg, test, terminal string, children ...jsonEvent) []byte {
	events := []jsonEvent{{action: "start", pkg: pkg}, {action: "run", pkg: pkg, test: test}}
	events = append(events, children...)
	events = append(events, jsonEvent{action: terminal, pkg: pkg, test: test}, jsonEvent{action: terminal, pkg: pkg})
	return stream(events...)
}

func TestParseGoReportPassAndPackageNamespaces(t *testing.T) {
	raw := append(packageRun("example/a", "TestSame", "pass"), packageRun("example/b", "TestSame", "pass")...)
	selected := []SelectedTest{{Package: "example/a", ID: "TestSame", File: "a/a_test.go"}, {Package: "example/b", ID: "TestSame", File: "b/b_test.go"}}
	report, err := ParseGoReport(raw, selected, 0)
	if err != nil {
		t.Fatal(err)
	}
	if report.Result != "pass" || report.Executed != 2 || report.PackageCount != 2 || len(report.Tests) != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Tests[0].QualifiedID != "example/a::TestSame" || report.Tests[1].QualifiedID != "example/b::TestSame" {
		t.Fatalf("qualified identities lost: %+v", report.Tests)
	}
}

func TestParseGoReportExpectedFailure(t *testing.T) {
	report, err := ParseGoReport(packageRun("example/a", "TestRed", "fail"), []SelectedTest{{Package: "example/a", ID: "TestRed", File: "a_test.go"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if report.Result != "fail" || report.Tests[0].Outcome != "fail" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestParseGoReportSubtestsAndProgress(t *testing.T) {
	children := []jsonEvent{
		{action: "pause", pkg: "example/a", test: "TestFamily"},
		{action: "cont", pkg: "example/a", test: "TestFamily"},
		{action: "run", pkg: "example/a", test: "TestFamily/one"},
		{action: "pass", pkg: "example/a", test: "TestFamily/one"},
		{action: "run", pkg: "example/a", test: "TestFamily/two"},
		{action: "pass", pkg: "example/a", test: "TestFamily/two"},
	}
	report, err := ParseGoReport(packageRun("example/a", "TestFamily", "pass", children...), []SelectedTest{{Package: "example/a", ID: "TestFamily", File: "a_test.go"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestFamily/one", "TestFamily/two"}
	if fmt.Sprint(report.Tests[0].Cases) != fmt.Sprint(want) {
		t.Fatalf("cases %v, want %v", report.Tests[0].Cases, want)
	}
	if report.Executed != 3 {
		t.Fatalf("executed=%d, want parent plus two concrete subtests", report.Executed)
	}
}

func TestParseGoReportRefusals(t *testing.T) {
	selection := []SelectedTest{{Package: "example/a", ID: "TestChosen", File: "a_test.go"}}
	cases := map[string]struct {
		raw  []byte
		exit int
	}{
		"zero selections":                 {raw: packageRun("example/a", "TestChosen", "pass"), exit: 0},
		"child does not imply parent":     {raw: packageRun("example/a", "TestChosen/child", "pass"), exit: 0},
		"missing package end":             {raw: stream(jsonEvent{action: "start", pkg: "example/a"}, jsonEvent{action: "run", pkg: "example/a", test: "TestChosen"}, jsonEvent{action: "pass", pkg: "example/a", test: "TestChosen"}), exit: 0},
		"compile failure":                 {raw: stream(jsonEvent{action: "start", pkg: "example/a"}, jsonEvent{action: "output", pkg: "example/a", output: "build failed\n"}, jsonEvent{action: "fail", pkg: "example/a"}), exit: 1},
		"selected pass then package fail": {raw: stream(jsonEvent{action: "start", pkg: "example/a"}, jsonEvent{action: "run", pkg: "example/a", test: "TestChosen"}, jsonEvent{action: "pass", pkg: "example/a", test: "TestChosen"}, jsonEvent{action: "fail", pkg: "example/a"}), exit: 1},
		"skip":                            {raw: packageRun("example/a", "TestChosen", "skip"), exit: 0},
		"duplicate run":                   {raw: stream(jsonEvent{action: "start", pkg: "example/a"}, jsonEvent{action: "run", pkg: "example/a", test: "TestChosen"}, jsonEvent{action: "run", pkg: "example/a", test: "TestChosen"}, jsonEvent{action: "pass", pkg: "example/a", test: "TestChosen"}, jsonEvent{action: "pass", pkg: "example/a"}), exit: 0},
		"duplicate terminal":              {raw: stream(jsonEvent{action: "start", pkg: "example/a"}, jsonEvent{action: "run", pkg: "example/a", test: "TestChosen"}, jsonEvent{action: "pass", pkg: "example/a", test: "TestChosen"}, jsonEvent{action: "pass", pkg: "example/a", test: "TestChosen"}, jsonEvent{action: "pass", pkg: "example/a"}), exit: 0},
		"malformed":                       {raw: []byte("not json\n"), exit: 1},
		"build event without package":     {raw: stream(jsonEvent{action: "build-output", output: "compile failed\n"}), exit: 1},
		"unknown action":                  {raw: stream(jsonEvent{action: "mystery", pkg: "example/a"}), exit: 0},
		"nonzero with pass":               {raw: packageRun("example/a", "TestChosen", "pass"), exit: 1},
		"zero with failure":               {raw: packageRun("example/a", "TestChosen", "fail"), exit: 0},
		"other package failure":           {raw: append(packageRun("example/a", "TestChosen", "pass"), stream(jsonEvent{action: "start", pkg: "example/b"}, jsonEvent{action: "output", pkg: "example/b", output: "build failed\n"}, jsonEvent{action: "fail", pkg: "example/b"})...), exit: 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sel := selection
			if name == "zero selections" {
				sel = nil
			}
			if report, err := ParseGoReport(tc.raw, sel, tc.exit); err == nil {
				t.Fatalf("accepted unusable report: %+v", report)
			}
		})
	}
}

func TestParseGoReportFailPlusSkipFamilyRefuses(t *testing.T) {
	children := []jsonEvent{
		{action: "run", pkg: "example/a", test: "TestFamily/failure"}, {action: "fail", pkg: "example/a", test: "TestFamily/failure"},
		{action: "run", pkg: "example/a", test: "TestFamily/skipped"}, {action: "skip", pkg: "example/a", test: "TestFamily/skipped"},
	}
	raw := packageRun("example/a", "TestFamily", "fail", children...)
	if report, err := ParseGoReport(raw, []SelectedTest{{Package: "example/a", ID: "TestFamily", File: "a_test.go"}}, 1); err == nil {
		t.Fatalf("accepted withheld family: %+v", report)
	}
}

func TestParseGoReportBuildEvents(t *testing.T) {
	selected := []SelectedTest{{Package: "example/a", ID: "TestChosen", File: "a_test.go"}}
	benign := append(stream(jsonEvent{action: "build-output", importPath: "example/a", output: "warning: linker note\n"}), packageRun("example/a", "TestChosen", "pass")...)
	report, err := ParseGoReport(benign, selected, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.BuildEvents) != 1 || report.BuildEvents[0].ImportPath != "example/a" {
		t.Fatalf("build diagnostics lost: %+v", report)
	}
	failing := stream(jsonEvent{action: "build-output", importPath: "example/a", output: "compile error\n"}, jsonEvent{action: "build-fail", importPath: "example/a"})
	if _, err := ParseGoReport(failing, selected, 1); err == nil || !strings.Contains(err.Error(), "example/a") {
		t.Fatalf("build-fail err=%v", err)
	}
}
