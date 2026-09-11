package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

// Start with an unmodified legacy ledger, not hand-built fork metadata. Every
// authority change goes through the same preview/apply entry points users run.
func TestForkEndToEndRealEntryPoint(t *testing.T) {
	bin := stressBinary(t)
	for _, external := range []bool{false, true} {
		name := "internal"
		if external {
			name = "external"
		}
		t.Run(name, func(t *testing.T) {
			f := newForkEndToEndFixture(t, external)
			parentBefore := mustForkWriteRead(t, f.parentA)
			unrelatedBefore := mustForkWriteRead(t, filepath.Join(f.root, "unrelated.txt"))
			adoption := adoptionCLIProposal("adopt", "e2e-adopt", "binding-a", "", "Decisions/decisions.md")
			adoption["sourceLedgerId"] = forkWriteParentA
			adoption["legacyContexts"] = []decisionview.LegacyContext{{Root: decisionview.SourceRootPlanning, Path: "Research/legacy.md", Namespace: forkWriteParentA, LocalIDs: []string{"D-0001"}}}
			previewApplyForkCLI(t, bin, f, "adopt", adoption)
			replacement := "Use example.org/downstream/planner/v2; users install it themselves. The plugin never compiles or downloads binaries and enforces minSddVersion."
			override := overrideCLIProposal("override", "e2e-override", qualified(forkWriteParentA, "D-0001"), "", replacement)
			override["scope"] = []string{}
			override["confirmation"] = "Check the downstream /v2 install command, user-only provisioning, no plugin build/download, and the version floor."
			previewApplyForkCLI(t, bin, f, "override", override)
			beforeReads := forkReadSnapshot(t, filepath.Dir(f.root))
			for _, args := range [][]string{{"decide", "effective", "--json"}, {"decide", "list", "--json"}, {"decide", "search", "downstream", "--json"}} {
				out, diagnostic, err := runSdd(bin, f.root, args...)
				requireCLIExit(t, err, 0, out, diagnostic)
				var view decideForkReadOutput
				if err := json.Unmarshal([]byte(out), &view); err != nil {
					t.Fatal(err)
				}
				if view.Resolution != decisionview.ResolutionComplete || len(view.Decisions) != 1 || string(view.Decisions[0].ID) != qualified(forkWriteLocal, "D-0001") || view.Decisions[0].Original["statement"] != replacement {
					t.Fatalf("consumer %v disagrees with approved replacement: %s", args, out)
				}
			}
			out, diagnostic, err := runSdd(bin, f.root, "decide", "lookup", qualified(forkWriteParentA, "D-0001"), "--json")
			requireCLIExit(t, err, 0, out, diagnostic)
			var lookup decideForkReadOutput
			if err := json.Unmarshal([]byte(out), &lookup); err != nil {
				t.Fatal(err)
			}
			if lookup.Original == nil || lookup.Current == nil || string(lookup.Original.ID) != qualified(forkWriteParentA, "D-0001") || string(lookup.Current.ID) != qualified(forkWriteLocal, "D-0001") {
				t.Fatalf("historical identity was lost: %s", out)
			}
			for _, args := range [][]string{{"decide", "validate", "--json"}, {"validate", "--json"}} {
				out, diagnostic, err := runSdd(bin, f.root, args...)
				requireCLIExit(t, err, 0, out, diagnostic)
			}
			hook := runForkSessionStart(t, f.root)
			if !strings.Contains(hook, replacement) || strings.Contains(hook, "upstream provisioning authority") {
				t.Fatalf("hook disagrees with effective authority: %s", hook)
			}
			if after := forkReadSnapshot(t, filepath.Dir(f.root)); !reflect.DeepEqual(beforeReads, after) {
				for path, digest := range after {
					if beforeReads[path] != digest {
						t.Errorf("read-only consumer changed or created %s", path)
					}
				}
				t.Fatal("read-only consumers changed source, local, config, or unrelated bytes")
			}
			if !reflect.DeepEqual(parentBefore, mustForkWriteRead(t, f.parentA)) || !reflect.DeepEqual(unrelatedBefore, mustForkWriteRead(t, filepath.Join(f.root, "unrelated.txt"))) {
				t.Fatal("adoption or override changed inherited/unrelated bytes")
			}
			for _, text := range []string{"See D-0001 and D-0001.", "See D-0001 and D-0001 and D-0001."} {
				want := 0
				if strings.Count(text, "D-0001") == 3 {
					want = 1
				}
				forkValidationRunInput(t, bin, f.root, []string{"section", "set", "Research/legacy.md", "--heading", "## Context", "--dry-run", "--json"}, text, want)
			}
		})
	}
}

func TestForkEndToEndSeedReplay(t *testing.T) {
	bin := stressBinary(t)
	run := func(t *testing.T, seed int64) []string {
		rng := rand.New(rand.NewSource(seed))
		f := newForkEndToEndFixture(t, true)
		adoption := adoptionCLIProposal("adopt", "replay-adopt", "binding-a", "", "Decisions/decisions.md")
		adoption["sourceLedgerId"] = forkWriteParentA
		previewApplyForkCLI(t, bin, f, "adopt", adoption)
		statement := fmt.Sprintf("complete replacement %d", rng.Int63())
		override := overrideCLIProposal("override", "replay-override", qualified(forkWriteParentA, "D-0001"), "", statement)
		override["scope"] = []string{}
		envelope := previewApplyForkCLI(t, bin, f, "override", override)
		var trace []string
		read := func(exit int, args ...string) map[string]any {
			before := forkReadSnapshot(t, filepath.Dir(f.root))
			out, diagnostic, err := runSdd(bin, f.root, args...)
			requireCLIExit(t, err, exit, out, diagnostic)
			value := decodeCLIObject(t, out)
			trace = append(trace, out)
			if !reflect.DeepEqual(before, forkReadSnapshot(t, filepath.Dir(f.root))) {
				t.Fatalf("read %v changed files", args)
			}
			return value
		}
		// A lost reply must neither append another decision nor allocate another ID.
		out, diagnostic, err := runSdd(bin, f.root, "decide", "fork", "apply", "--file", filepath.Join(f.root, "override-envelope.json"), "--approval-digest", envelope["digest"].(string), "--json")
		requireCLIExit(t, err, 0, out, diagnostic)
		if decodeCLIObject(t, out)["replayed"] != true {
			t.Fatalf("lost reply was not recognized: %s", out)
		}
		read(0, "decide", "effective", "--json")
		localBefore := mustForkWriteRead(t, f.local)
		old := forkReadEntry("D-0001", "accepted", "upstream provisioning authority")
		old["scope"] = []string{}
		addition := forkReadEntry("D-0002", "accepted", fmt.Sprintf("another upstream answer %d", rng.Int63()))
		addition["scope"] = []string{}
		addition["rejected"] = []string{statement}
		forkReadWriteLedger(t, f.planning, "Decisions/decisions.md", []map[string]any{old, addition}, nil)
		value := read(0, "decide", "effective", "--json")
		if value["resolution"] != "complete" || !jsonContainsString(value["diagnostics"], "DLG061") || !jsonContainsSubstring(value["diagnostics"], "Judge whether") {
			t.Fatalf("compatible source addition was not a non-gating collision candidate: %#v", value)
		}
		if !reflect.DeepEqual(localBefore, mustForkWriteRead(t, f.local)) {
			t.Fatal("compatible update refreshed approved local bytes")
		}
		old["status"], old["superseded_by"] = "superseded", "D-0003"
		successor := forkReadEntry("D-0003", "accepted", "successor provisioning authority")
		successor["scope"], successor["supersedes"] = []string{}, "D-0001"
		forkReadWriteLedger(t, f.planning, "Decisions/decisions.md", []map[string]any{old, addition, successor}, nil)
		value = read(1, "decide", "search", "no-such-decision", "--json")
		if value["resolution"] == "complete" || len(value["diagnostics"].([]any)) == 0 {
			t.Fatal("filter hid stale authority")
		}
		reconcile := overrideCLIProposal("reconcile", "replay-reconcile", qualified(forkWriteParentA, "D-0003"), qualified(forkWriteLocal, "D-0001"), "explicitly reconciled replacement")
		reconcile["scope"] = []string{}
		previewApplyForkCLI(t, bin, f, "reconcile", reconcile)
		value = read(0, "decide", "history", "--json")
		if !jsonContainsString(value, statement) || !jsonContainsString(value, "explicitly reconciled replacement") {
			t.Fatal("reconciliation erased immutable local history")
		}
		restore := map[string]any{"version": 1, "operation": "restore", "operationId": "replay-restore", "date": "2026-09-10", "decidedBy": "user-approved", "confirmation": "restore the current parent without erasing the replacement", "replacement": qualified(forkWriteLocal, "D-0002")}
		previewApplyForkCLI(t, bin, f, "restore", restore)
		value = read(0, "decide", "effective", "--json")
		if !jsonContainsString(value["decisions"], qualified(forkWriteParentA, "D-0003")) || jsonContainsString(value["decisions"], qualified(forkWriteLocal, "D-0002")) {
			t.Fatal("restoration reactivated an ended local override")
		}
		return trace
	}
	for _, seed := range []int64{7, 41} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			if first, second := run(t, seed), run(t, seed); !reflect.DeepEqual(first, second) {
				t.Fatal("independent same-seed CLI traces differ")
			}
		})
	}
}

func TestForkEndToEndHostileFormats(t *testing.T) {
	bin := stressBinary(t)
	for _, source := range []string{"../escape.md", "https://example.invalid/source.md", "Decisions"} {
		t.Run(source, func(t *testing.T) {
			f := newForkEndToEndFixture(t, false)
			proposal := adoptionCLIProposal("adopt", "hostile-source", "binding-a", "", source)
			proposal["sourceLedgerId"] = forkWriteParentA
			path := writeForkProposal(t, f.root, "hostile.json", proposal)
			before := forkReadSnapshot(t, f.root)
			out, diagnostic, err := runSdd(bin, f.root, "decide", "fork", "preview", "--operation", "adopt", "--file", path, "--json")
			want := 2 // malformed locators are malformed invocation, not authority findings
			if source == "Decisions" {
				want = 1
			}
			requireCLIExit(t, err, want, out, diagnostic)
			if !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
				t.Fatal("hostile source refusal changed files")
			}
		})
	}
	f := newForkEndToEndFixture(t, false)
	proposal := adoptionCLIProposal("adopt", "hostile-adopt", "binding-a", "", "Decisions/decisions.md")
	proposal["sourceLedgerId"] = forkWriteParentA
	previewApplyForkCLI(t, bin, f, "adopt", proposal)
	const statement = "yes no true false null\n\"quoted\" \\ slash \u96ea"
	override := overrideCLIProposal("override", "hostile-override", qualified(forkWriteParentA, "D-0001"), "", statement)
	override["scope"] = []string{}
	previewApplyForkCLI(t, bin, f, "override", override)
	assertForkWriteRead(t, bin, f, qualified(forkWriteLocal, "D-0001"), statement)
	config := mustForkWriteRead(t, f.configPath)
	for _, raw := range []string{
		strings.Replace(string(config), `"version":1`, `"version":999`, 1),
		strings.Replace(string(config), `"mode":"fork"`, `"mode":"fork","mode":"detached"`, 1),
		strings.Replace(string(config), `"decisionLog":`, `"decision\u004cog":`, 1)[:len(config)-2],
	} {
		forkReadWrite(t, f.root, "planning-config.json", []byte(raw))
		before := forkReadSnapshot(t, f.root)
		out, diagnostic, err := runSdd(bin, f.root, "decide", "effective", "--json")
		requireCLIExit(t, err, 2, out, diagnostic)
		if hook := runForkSessionStart(t, f.root); !strings.Contains(hook, "Warning") || strings.Contains(hook, statement) {
			t.Fatalf("malformed authority became partial hook instructions: %s", hook)
		}
		if !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
			t.Fatal("hostile selector read repaired or changed files")
		}
	}
}

func newForkEndToEndFixture(t *testing.T, external bool) forkWriteFixture {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "repository")
	planningRel := ".plans"
	if external {
		planningRel = "../planning"
	}
	planning := filepath.Join(root, filepath.FromSlash(planningRel))
	config, _ := json.Marshal(map[string]any{"planningRoot": planningRel, "unrelated": map[string]any{"keep": true}})
	forkReadWrite(t, root, "planning-config.json", config)
	forkReadWrite(t, root, "unrelated.txt", []byte("untracked work must survive\n"))
	entry := forkReadEntry("D-0001", "accepted", "upstream provisioning authority")
	entry["scope"] = []string{}
	forkReadWriteLedger(t, planning, "Decisions/decisions.md", []map[string]any{entry}, nil)
	research := strings.Replace(forkValidationCompileResearch, "## Context\n", "## Context\n\nSee D-0001 and D-0001.\n", 1)
	forkReadWrite(t, planning, "Research/legacy.md", []byte(research))
	return forkWriteFixture{root: root, planning: planning, configPath: filepath.Join(root, "planning-config.json"), parentA: filepath.Join(planning, "Decisions", "decisions.md"), local: filepath.Join(planning, "Decisions", "fork.md")}
}

func TestForkEndToEndRecoveryAfterFinalConfig(t *testing.T) {
	bin := stressBinary(t)
	f := newForkWriteFixture(t)
	adoptForkCLI(t, bin, f)
	out, diagnostic, err := runSdd(bin, f.root, "decide", "fork", "inspect", "--operation", "cli-adopt-roundtrip", "--json")
	requireCLIExit(t, err, 0, out, diagnostic)
	inspection := decodeCLIObject(t, out)
	journalPath := filepath.Join(f.planning, filepath.FromSlash(inspection["journal"].(string)))
	var journal decisionview.ForkJournal
	if err := json.Unmarshal(mustForkWriteRead(t, journalPath), &journal); err != nil {
		t.Fatal(err)
	}
	// Reproduce the durable crash state after final config publication but
	// before commit/barrier release. Native process-crash tests cover the writer.
	journal.Status = "pending"
	writeForkProposal(t, filepath.Dir(journalPath), filepath.Base(journalPath), journal)
	barrierPath := filepath.Join(f.planning, "Decisions", ".fork-state", string(forkWriteLocal), "pending.json")
	var barrier decisionview.ForkBarrier
	if err := json.Unmarshal(mustForkWriteRead(t, barrierPath), &barrier); err != nil {
		t.Fatal(err)
	}
	barrier.Status = "pending"
	writeForkProposal(t, filepath.Dir(barrierPath), filepath.Base(barrierPath), barrier)
	before := forkReadSnapshot(t, f.root)
	out, diagnostic, err = runSdd(bin, f.root, "decide", "effective", "--json")
	requireCLIExit(t, err, 1, out, diagnostic)
	out, diagnostic, err = runSdd(bin, f.root, "decide", "fork", "inspect", "--operation", journal.OperationID, "--json")
	requireCLIExit(t, err, 0, out, diagnostic)
	inspection = decodeCLIObject(t, out)
	if inspection["authority"] != "recovery-required" || !jsonContainsString(inspection["barriers"], "pending") {
		t.Fatalf("inspection hides incomplete authority: %s", out)
	}
	out, diagnostic, err = runSdd(bin, f.root, "decide", "fork", "recover", "--operation", journal.OperationID, "--action", "finish", "--json")
	requireCLIExit(t, err, 0, out, diagnostic)
	digest := decodeCLIObject(t, out)["digest"].(string)
	if !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
		t.Fatal("recovery inspection repaired authority without approval")
	}
	out, diagnostic, err = runSdd(bin, f.root, "decide", "fork", "recover", "--operation", journal.OperationID, "--action", "finish", "--approval-digest", digest, "--json")
	requireCLIExit(t, err, 0, out, diagnostic)
	assertForkWriteRead(t, bin, f, qualified(forkWriteParentA, "D-0001"), "parent A authority")
	if _, err := os.Stat(f.local); err != nil {
		t.Fatal(err)
	}
}

func TestForkEndToEndOwningStoreHistory(t *testing.T) {
	bin := stressBinary(t)
	for _, staged := range []bool{false, true} {
		t.Run(fmt.Sprint(staged), func(t *testing.T) {
			f := newForkEndToEndFixture(t, true)
			proposal := adoptionCLIProposal("adopt", "history-adopt", "binding-a", "", "Decisions/decisions.md")
			proposal["sourceLedgerId"] = forkWriteParentA
			previewApplyForkCLI(t, bin, f, "adopt", proposal)
			override := overrideCLIProposal("override", "history-override", qualified(forkWriteParentA, "D-0001"), "", "immutable local replacement")
			override["scope"] = []string{}
			previewApplyForkCLI(t, bin, f, "override", override)
			gitStress(t, f.planning, "init", "-q")
			forkReadWrite(t, f.planning, "unrelated.txt", []byte("independent store history\n"))
			gitStress(t, f.planning, "add", "unrelated.txt")
			gitStress(t, f.planning, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "commit", "-qm", "Create unrelated store history")
			untracked, untrackedDiagnostic, untrackedErr := runSdd(bin, f.root, "decide", "effective", "--json")
			requireCLIExit(t, untrackedErr, 0, untracked, untrackedDiagnostic)
			if !jsonContainsString(decodeCLIObject(t, untracked), "untracked") || jsonContainsString(decodeCLIObject(t, untracked), "git-head-and-index") {
				t.Fatal("untracked ledger falsely claimed recorded history coverage")
			}
			gitStress(t, f.planning, "add", "Decisions/decisions.md", "Decisions/fork.md")
			gitStress(t, f.planning, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "commit", "-qm", "Record independent planning history")
			out, diagnostic, err := runSdd(bin, f.root, "decide", "effective", "--json")
			requireCLIExit(t, err, 0, out, diagnostic)
			if !jsonContainsString(decodeCLIObject(t, out), "git-head-and-index") {
				t.Fatal("effective view omits owning-store history coverage")
			}
			original := mustForkWriteRead(t, f.local)
			changed := strings.Replace(string(original), "immutable local replacement", "unapproved local replacement", 1)
			forkReadWrite(t, f.planning, "Decisions/fork.md", []byte(changed))
			if staged {
				gitStress(t, f.planning, "add", "Decisions/fork.md")
				forkReadWrite(t, f.planning, "Decisions/fork.md", original)
			}
			before := forkReadSnapshot(t, filepath.Dir(f.root))
			out, diagnostic, err = runSdd(bin, f.root, "decide", "effective", "--json")
			requireCLIExit(t, err, 1, out, diagnostic)
			if !jsonContainsSubstring(decodeCLIObject(t, out), "immutable field statement") {
				t.Fatalf("owning store history failed to protect local accepted text: %s", out)
			}
			if !reflect.DeepEqual(before, forkReadSnapshot(t, filepath.Dir(f.root))) {
				t.Fatal("history audit changed planning/source bytes or Git state")
			}
		})
	}
}

func TestForkRebindRejectsSourceIdentityAlias(t *testing.T) {
	bin := stressBinary(t)
	f := newForkWriteFixture(t)
	adoptForkCLI(t, bin, f)
	forkReadWriteLedger(t, f.planning, "Decisions/alias.md", []map[string]any{forkReadEntry("D-0001", "accepted", "different authority")}, nil)
	proposal := adoptionCLIProposal("rebind", "alias-rebind", "binding-b", "binding-a", "Decisions/alias.md")
	proposal["sourceLedgerId"] = forkWriteParentA
	path := writeForkProposal(t, f.root, "alias.json", proposal)
	before := forkReadSnapshot(t, f.root)
	out, diagnostic, err := runSdd(bin, f.root, "decide", "fork", "preview", "--operation", "rebind", "--file", path, "--json")
	requireCLIExit(t, err, 1, out, diagnostic)
	if !strings.Contains(diagnostic, "multiple source locators") || !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
		t.Fatalf("alias preview was not refused without changing authority: %s", diagnostic)
	}
}
