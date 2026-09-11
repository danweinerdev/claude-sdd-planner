package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestForkCLIReviewSourceRestoreCapabilities(t *testing.T) {
	bin := stressBinary(t)

	t.Run("adoption accepts either declared source root and retains guards", func(t *testing.T) {
		t.Run("repository anchored source", func(t *testing.T) {
			f := newForkWriteFixture(t)
			writeForkSource(t, f.root, "Decisions/repository-parent.md", forkWriteParentA, "repository anchored authority")
			proposal := adoptionCLIProposal("adopt", "review-repository-source", "binding-repository", "", "Decisions/repository-parent.md")
			proposal["source"] = map[string]any{"root": "repository", "path": "Decisions/repository-parent.md"}
			sourceBefore := mustForkWriteRead(t, f.root+string(os.PathSeparator)+"Decisions"+string(os.PathSeparator)+"repository-parent.md")

			previewApplyForkCLI(t, bin, f, "adopt", proposal)
			assertForkWriteRead(t, bin, f, qualified(forkWriteParentA, "D-0001"), "repository anchored authority")
			if got := mustForkWriteRead(t, f.root+string(os.PathSeparator)+"Decisions"+string(os.PathSeparator)+"repository-parent.md"); !reflect.DeepEqual(got, sourceBefore) {
				t.Fatal("adoption edited the repository-anchored source")
			}
		})

		for name, test := range map[string]struct {
			exit   int
			mutate func(map[string]any)
		}{
			"unknown root": {exit: 2, mutate: func(p map[string]any) {
				p["source"] = map[string]any{"root": "workspace", "path": "Decisions/parent-a.md"}
			}},
			"root escape": {exit: 2, mutate: func(p map[string]any) {
				p["source"] = map[string]any{"root": "planning", "path": "../planning-config.json"}
			}},
			"source owner mismatch": {exit: 1, mutate: func(p map[string]any) {
				p["sourceOwnerId"] = "50000000-0000-4000-8000-000000000005"
			}},
		} {
			t.Run(name, func(t *testing.T) {
				f := newForkWriteFixture(t)
				proposal := adoptionCLIProposal("adopt", "review-guard-"+strings.ReplaceAll(name, " ", "-"), "binding-guard", "", "Decisions/parent-a.md")
				test.mutate(proposal)
				path := writeForkProposal(t, f.root, "guard.json", proposal)
				before := forkReadSnapshot(t, f.root)
				stdout, stderr, err := runSdd(bin, f.root, "decide", "fork", "preview", "--operation", "adopt", "--file", path, "--json")
				requireCLIExit(t, err, test.exit, stdout, stderr)
				if !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
					t.Fatal("rejected adoption changed persisted files")
				}
			})
		}

		t.Run("apply still requires exact approval", func(t *testing.T) {
			f := newForkWriteFixture(t)
			proposal := writeForkProposal(t, f.root, "approval.json", adoptionCLIProposal("adopt", "review-approval", "binding-approval", "", "Decisions/parent-a.md"))
			stdout, stderr, err := runSdd(bin, f.root, "decide", "fork", "preview", "--operation", "adopt", "--file", proposal, "--json")
			requireCLIExit(t, err, 0, stdout, stderr)
			envelope := writeForkProposal(t, f.root, "approval-envelope.json", decodeCLIObject(t, stdout))
			before := forkReadSnapshot(t, f.root)
			stdout, stderr, err = runSdd(bin, f.root, "decide", "fork", "apply", "--file", envelope, "--json")
			requireCLIExit(t, err, 1, stdout, stderr)
			if !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
				t.Fatal("missing approval changed persisted files")
			}
		})
	})

	t.Run("stale override can be restored but cannot be detached", func(t *testing.T) {
		makeStale := func(t *testing.T) forkWriteFixture {
			t.Helper()
			f := newForkWriteFixture(t)
			adoptForkCLI(t, bin, f)
			previewApplyForkCLI(t, bin, f, "override", overrideCLIProposal("override", "review-stale-override", qualified(forkWriteParentA, "D-0001"), "", "stale local replacement"))
			oldParent := forkReadEntry("D-0001", "superseded", "parent A authority")
			oldParent["superseded_by"] = "D-0002"
			newParent := forkReadEntry("D-0002", "accepted", "parent A successor")
			newParent["supersedes"] = "D-0001"
			writeForkSourceEntries(t, f.planning, "Decisions/parent-a.md", forkWriteParentA, []map[string]any{oldParent, newParent})
			return f
		}

		t.Run("restore preview and apply", func(t *testing.T) {
			f := makeStale(t)
			restore := map[string]any{"version": 1, "operation": "restore", "operationId": "review-stale-restore", "date": "2026-09-10", "decidedBy": "user-approved", "confirmation": "restore this exact retained stale override", "replacement": qualified(forkWriteLocal, "D-0001")}
			previewApplyForkCLI(t, bin, f, "restore", restore)
			assertForkWriteRead(t, bin, f, qualified(forkWriteParentA, "D-0002"), "parent A successor")
		})

		t.Run("detach refusal is a no-op", func(t *testing.T) {
			f := makeStale(t)
			detach := map[string]any{"version": 1, "operation": "detach", "operationId": "review-stale-detach", "date": "2026-09-10", "decidedBy": "user-approved", "confirmation": "detach this exact selected fork and retain local history"}
			path := writeForkProposal(t, f.root, "detach-stale.json", detach)
			before := forkReadSnapshot(t, f.root)
			stdout, stderr, err := runSdd(bin, f.root, "decide", "fork", "preview", "--operation", "detach", "--file", path, "--json")
			requireCLIExit(t, err, 1, stdout, stderr)
			if !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
				t.Fatal("stale detach refusal changed persisted files")
			}
		})
	})

	t.Run("capabilities use the canonical versioned contract", func(t *testing.T) {
		f := newForkWriteFixture(t)
		stdout, stderr, err := runSdd(bin, f.root, "decide", "capabilities", "--json")
		requireCLIExit(t, err, 0, stdout, stderr)
		capabilities := decodeCLIObject(t, stdout)
		if _, legacy := capabilities["decisionforks"]; legacy {
			t.Fatalf("capabilities retain non-canonical decisionforks key: %s", stdout)
		}
		forks, ok := capabilities["decision_forks"].(map[string]any)
		if !ok || forks["schema"] != float64(1) || forks["transactions"] != float64(1) || forks["partial"] != true {
			t.Fatalf("decision_forks does not report a versioned truthful partial contract: %s", stdout)
		}
		for _, supported := range []string{"effective", "history", "adopt", "restore"} {
			if !jsonContainsString(forks, supported) {
				t.Errorf("decision_forks omits supported operation %q: %s", supported, stdout)
			}
		}
		transactionOperations := forks["transaction_operations"]
		if !jsonContainsString(transactionOperations, "apply") || !jsonContainsString(transactionOperations, "inspect") || !jsonContainsString(transactionOperations, "recover") {
			t.Errorf("decision_forks omits versioned apply/inspect/recover transaction support: %s", stdout)
		}
		if jsonContainsString(forks, "archive") || jsonContainsString(forks, "add") || jsonContainsString(forks, "release-complete") {
			t.Errorf("decision_forks advertises unsupported completion: %s", stdout)
		}
	})
}
