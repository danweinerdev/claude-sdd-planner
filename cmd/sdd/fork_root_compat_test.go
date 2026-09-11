package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestForkLegacyRootResolutionCompatibility drives the public validate command
// with a scope whose governing legacy decision also names a repository-root
// file. This makes the assertion depend on validation's real repository scope,
// rather than merely checking resolveRoots' returned strings.
func TestForkLegacyRootResolutionCompatibility(t *testing.T) {
	bin := stressBinary(t)

	for _, tc := range []struct {
		name     string
		explicit bool
		vcs      bool
	}{
		{name: "explicit planning root", explicit: true, vcs: true},
		{name: "nested legacy config", vcs: true},
		{name: "nested legacy config outside VCS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := t.TempDir()
			if tc.vcs {
				gitStress(t, repository, "init", "-q")
			}

			configOwner := filepath.Join(repository, "nested", "owner")
			planning := filepath.Join(configOwner, ".plans")
			cwd := filepath.Join(configOwner, "work", "below")
			if err := os.MkdirAll(cwd, 0o755); err != nil {
				t.Fatal(err)
			}
			markerRoot := configOwner
			if tc.vcs {
				markerRoot = repository
			}
			if err := os.WriteFile(filepath.Join(markerRoot, "owner-only.txt"), []byte("repository scope marker\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if !tc.explicit {
				forkReadWrite(t, configOwner, "planning-config.json", []byte(`{"planningRoot":".plans"}`))
			}

			entry := forkReadEntry("D-0001", "accepted", "repository scoped legacy rule")
			entry["scope"] = []string{"Research/in-scope.md", "owner-only.txt"}
			forkReadWriteLedger(t, planning, "Decisions/decisions.md", []map[string]any{entry}, nil)
			doc := replaceOnce(validResearchDoc, "## Context\n\nText.", "## Context\n\nD-0001 governs this artifact.")
			forkReadWrite(t, planning, "Research/in-scope.md", []byte(doc))

			args := []string{"validate", "--scope", "Research/in-scope.md", "--json"}
			if tc.explicit {
				args = append(args, "--root", planning)
			}
			got := forkValidationRun(t, bin, cwd, args, 0)
			if jsonContainsSubstring(got.Value, "SDD145") {
				t.Fatalf("legacy repository-root scope was resolved against another root: %#v", got.Value)
			}
		})
	}
}

// TestForkExplicitOwnerRootResolution verifies that explicit --root does not
// discard the represented config owner. Both a valid selector and malformed
// selector are observed through scoped public validation; malformed authority
// must be captured and fail closed instead of becoming a legacy read.
func TestForkExplicitOwnerRootResolution(t *testing.T) {
	bin := stressBinary(t)

	t.Run("valid selector", func(t *testing.T) {
		owner := forkValidationFixture(t, true)
		planningRaw, err := os.ReadFile(filepath.Join(owner, "planning-config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			PlanningRoot string `json:"planningRoot"`
		}
		if err := json.Unmarshal(planningRaw, &config); err != nil {
			t.Fatal(err)
		}
		cwd := filepath.Join(owner, "nested", "cwd")
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}
		got := forkValidationRun(t, bin, cwd, []string{
			"validate", "--root", config.PlanningRoot, "--scope", "Research/in-scope.md", "--json",
		}, 0)
		requireForkValidationIDs(t, got, []string{qualified(forkReadLocal, "D-0100"), qualified(forkReadParent, "D-0001")})
	})

	t.Run("malformed selector", func(t *testing.T) {
		owner := forkValidationFixture(t, true)
		raw, err := os.ReadFile(filepath.Join(owner, "planning-config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			PlanningRoot string `json:"planningRoot"`
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		planningJSON, err := json.Marshal(config.PlanningRoot)
		if err != nil {
			t.Fatal(err)
		}
		malformed := append([]byte(`{"planningRoot":`), planningJSON...)
		malformed = append(malformed, []byte(`,"decisionLog":`)...)
		if err := os.WriteFile(filepath.Join(owner, "planning-config.json"), malformed, 0o644); err != nil {
			t.Fatal(err)
		}
		cwd := filepath.Join(owner, "nested", "cwd")
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}
		got := forkValidationRun(t, bin, cwd, []string{
			"validate", "--root", config.PlanningRoot, "--scope", "Research/in-scope.md", "--json",
		}, 1)
		requireForkValidationDiagnostic(t, got, "FDL020", "error", "selector")
	})
}
