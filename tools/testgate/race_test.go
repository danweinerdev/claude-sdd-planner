package testgate

import (
	"strings"
	"testing"
)

// racePackages are the shared-state packages whose correctness depends on
// locking that only the race detector observes. A test like
// TestWaiverMemoConcurrencySafe passes on an unguarded memo without -race, so a
// gate that never enables the detector cannot catch a removed lock.
var racePackages = []string{
	"./internal/rules",
	"./internal/procexec",
	"./internal/vcs",
	"./internal/graph/sync",
	"./internal/graph/ops",
	"./internal/graph/provider",
	"./tools/regression",
}

// makeRules splits a Makefile into target name -> {prerequisites, recipe
// lines}. Prerequisites accumulate across rules, because GNU make lets a target
// declare extra prerequisites in a later rule that carries no recipe.
type makeRule struct {
	prereqs []string
	recipe  []string
}

func parseMakeRules(src string) map[string]*makeRule {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	rules := map[string]*makeRule{}
	var current []string
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "\t") {
			for _, name := range current {
				rules[name].recipe = append(rules[name].recipe, strings.TrimPrefix(line, "\t"))
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		targets, prereqs, ok := strings.Cut(line, ":")
		// A variable assignment (`:=`, `::=`, `?=`, `+=`, or plain `=`) has its
		// colon immediately followed — for `::=` after one more colon — by `=`,
		// not a rule's prerequisite list; strings.Contains(targets, "=") alone
		// misses these because the `=` lands in prereqs, not targets.
		isAssignment := strings.HasPrefix(prereqs, "=") || strings.HasPrefix(prereqs, ":=")
		if !ok || isAssignment || strings.Contains(targets, "=") || strings.HasPrefix(strings.TrimSpace(targets), ".") {
			current = nil
			continue
		}
		prereqs = strings.TrimPrefix(prereqs, "=")
		current = nil
		for _, name := range strings.Fields(targets) {
			if rules[name] == nil {
				rules[name] = &makeRule{}
			}
			rules[name].prereqs = append(rules[name].prereqs, strings.Fields(prereqs)...)
			current = append(current, name)
		}
	}
	return rules
}

// coversPackage accepts either the directory form (./internal/rules) or the
// recursive form (./internal/rules/...), with or without a trailing slash.
func coversPackage(recipe, pkg string) bool {
	for _, form := range []string{pkg, pkg + "/", pkg + "/..."} {
		for _, field := range strings.Fields(recipe) {
			if field == form {
				return true
			}
		}
	}
	return false
}

func TestParseMakeRulesSkipsVariableAssignments(t *testing.T) {
	src := "FOO := bar\ntest: deps\n\t@go test\n"
	rules := parseMakeRules(src)
	if _, ok := rules["FOO"]; ok {
		t.Fatalf("variable assignment must not be parsed as a rule: %v", rules)
	}
	test := rules["test"]
	if test == nil {
		t.Fatal("test: deps must still be parsed as a rule")
	}
	if len(test.prereqs) != 1 || test.prereqs[0] != "deps" {
		t.Fatalf("test rule prereqs = %v, want [deps]", test.prereqs)
	}
	if len(test.recipe) != 1 || test.recipe[0] != "@go test" {
		t.Fatalf("test rule recipe = %v, want [@go test]", test.recipe)
	}
}

func TestMakeTestRunsRaceDetector(t *testing.T) {
	rules := parseMakeRules(makefile(t))

	test := rules["test"]
	if test == nil {
		t.Fatal("Makefile declares no test: rule")
	}

	invokesRace := false
	for _, name := range test.prereqs {
		if name == "test-race" {
			invokesRace = true
		}
	}
	for _, line := range test.recipe {
		if strings.Contains(line, "test-race") {
			invokesRace = true
		}
	}
	if !invokesRace {
		t.Fatalf("the authoritative gate must depend on or invoke test-race, so a removed memo or containment lock fails `make test`; prerequisites=%v recipe=%v", test.prereqs, test.recipe)
	}

	race := rules["test-race"]
	if race == nil {
		t.Fatal("Makefile declares no test-race: rule")
	}
	recipe := strings.Join(race.recipe, "\n")
	if !strings.Contains(recipe, "go test") {
		t.Fatalf("test-race must run go test; recipe:\n%s", recipe)
	}
	for _, flag := range []string{"-race", "-count=1"} {
		if !strings.Contains(recipe, flag) {
			t.Fatalf("test-race must run go test with %s; recipe:\n%s", flag, recipe)
		}
	}
	for _, pkg := range racePackages {
		if !coversPackage(recipe, pkg) {
			t.Fatalf("test-race must cover shared-state package %s; recipe:\n%s", pkg, recipe)
		}
	}
}
