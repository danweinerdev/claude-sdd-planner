package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForkWindowsHistoryCaseRename(t *testing.T) {
	bin := stressBinary(t)
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			f := newForkEndToEndFixture(t, true)
			proposal := adoptionCLIProposal("adopt", "case-adopt", "binding-a", "", "Decisions/decisions.md")
			proposal["sourceLedgerId"] = forkWriteParentA
			previewApplyForkCLI(t, bin, f, "adopt", proposal)
			override := overrideCLIProposal("override", "case-override", qualified(forkWriteParentA, "D-0001"), "", "immutable local replacement")
			override["scope"] = []string{}
			previewApplyForkCLI(t, bin, f, "override", override)
			gitStress(t, f.planning, "init", "-q")
			gitStress(t, f.planning, "add", "Decisions/decisions.md", "Decisions/fork.md")
			gitStress(t, f.planning, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "commit", "-qm", "Record original path spelling")
			old, next := f.local, filepath.Join(f.planning, "Decisions", "FORK.md")
			if directory {
				old, next = filepath.Dir(f.local), filepath.Join(f.planning, "DECISIONS")
			}
			if err := os.Rename(old, old+"-renaming"); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(old+"-renaming", next); err != nil {
				t.Fatal(err)
			}
			out, diagnostic, err := runSdd(bin, f.root, "decide", "effective", "--json")
			requireCLIExit(t, err, 0, out, diagnostic)
			if !jsonContainsString(decodeCLIObject(t, out), "git-head-and-index") || jsonContainsString(decodeCLIObject(t, out), "untracked") {
				t.Fatalf("case-only rename lost history coverage: %s", out)
			}
			raw := mustForkWriteRead(t, f.local)
			if err := os.WriteFile(f.local, []byte(strings.Replace(string(raw), "immutable local replacement", "unapproved local replacement", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			out, diagnostic, err = runSdd(bin, f.root, "decide", "effective", "--json")
			requireCLIExit(t, err, 1, out, diagnostic)
			if !jsonContainsSubstring(decodeCLIObject(t, out), "immutable field statement") {
				t.Fatalf("case-only rename bypassed retained history: %s", out)
			}
		})
	}
}
