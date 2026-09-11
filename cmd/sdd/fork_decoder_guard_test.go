package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func TestForkEscapedMalformedDeclaration(t *testing.T) {
	t.Run("consumer capture recognizes escaped top-level declaration", func(t *testing.T) {
		owner := forkValidationFixture(t, true)
		malformed, planning := escapedMalformedSelector(t, owner)

		capture := decisionview.CaptureForRepository(owner)
		if !capture.Declared {
			t.Fatal("escaped semantic decisionLog key silently fell back to legacy authority")
		}
		found := false
		for _, diagnostic := range capture.Diagnostics {
			if diagnostic.Code == "FDL020" && bytes.Contains([]byte(diagnostic.Message), []byte("selector")) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("malformed declaration did not retain selector diagnostics: %#v", capture.Diagnostics)
		}
		requireFileBytes(t, filepath.Join(owner, "planning-config.json"), malformed)
		if planning == "" {
			t.Fatal("fixture planning root is empty")
		}
	})

	t.Run("real validate command refuses escaped malformed selector", func(t *testing.T) {
		bin := stressBinary(t)
		owner := forkValidationFixture(t, true)
		malformed, planning := escapedMalformedSelector(t, owner)
		cwd := filepath.Join(owner, "nested", "cwd")
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}

		got := forkValidationRun(t, bin, cwd, []string{
			"validate", "--root", planning, "--scope", "Research/in-scope.md", "--json",
		}, 1)
		requireForkValidationDiagnostic(t, got, "FDL020", "error", "selector")
		requireFileBytes(t, filepath.Join(owner, "planning-config.json"), malformed)
	})

	t.Run("real validate refuses corruption before later declaration key", func(t *testing.T) {
		bin := stressBinary(t)
		owner := forkValidationFixture(t, true)
		malformed, planning := malformedNestedBeforeDeclaration(t, owner)
		cwd := filepath.Join(owner, "nested", "cwd")
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}

		got := forkValidationRun(t, bin, cwd, []string{
			"validate", "--root", planning, "--scope", "Research/in-scope.md", "--json",
		}, 1)
		requireForkValidationDiagnostic(t, got, "FDL020", "error", "selector")
		requireFileBytes(t, filepath.Join(owner, "planning-config.json"), malformed)
	})

	t.Run("valid nested key is not an authority declaration", func(t *testing.T) {
		raw := []byte(`{"nested":{"decisionLog":{}}}`)
		if configDeclaresDecisionLog(raw) {
			t.Fatal("valid nested decisionLog key was treated as repository authority")
		}
		owner := t.TempDir()
		if err := os.WriteFile(filepath.Join(owner, "planning-config.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		capture := decisionview.CaptureForRepository(owner)
		if capture.Declared {
			t.Fatal("consumer treated a nested decisionLog key as repository authority")
		}
		requireFileBytes(t, filepath.Join(owner, "planning-config.json"), raw)
	})

	t.Run("truncated nested config is classified conservatively", func(t *testing.T) {
		raw := []byte(`{"planningRoot":".plans","nested":{"decisionLog":`)
		if !configDeclaresDecisionLog(raw) {
			t.Fatal("truncated nested config was allowed to fall back to legacy authority")
		}
		owner := t.TempDir()
		if err := os.WriteFile(filepath.Join(owner, "planning-config.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		capture := decisionview.CaptureForRepository(owner)
		if !capture.Declared {
			t.Fatal("consumer allowed truncated nested config to fall back to legacy authority")
		}
		requireFileBytes(t, filepath.Join(owner, "planning-config.json"), raw)
	})
}

func escapedMalformedSelector(t *testing.T, owner string) ([]byte, string) {
	t.Helper()
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
	planning, err := json.Marshal(config.PlanningRoot)
	if err != nil {
		t.Fatal(err)
	}
	malformed := append([]byte(`{"planningRoot":`), planning...)
	malformed = append(malformed, []byte(`,"repositoryId":"11111111-2222-3333-4444-555555555555","decisionL\u006fg":`)...)
	if err := os.WriteFile(filepath.Join(owner, "planning-config.json"), malformed, 0o644); err != nil {
		t.Fatal(err)
	}
	return malformed, config.PlanningRoot
}

func malformedNestedBeforeDeclaration(t *testing.T, owner string) ([]byte, string) {
	t.Helper()
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
	planning, err := json.Marshal(config.PlanningRoot)
	if err != nil {
		t.Fatal(err)
	}
	malformed := append([]byte(`{"planningRoot":`), planning...)
	malformed = append(malformed, []byte(`,"nested":{"value":tru},"decisionL\u006fg":`)...)
	if err := os.WriteFile(filepath.Join(owner, "planning-config.json"), malformed, 0o644); err != nil {
		t.Fatal(err)
	}
	return malformed, config.PlanningRoot
}

func requireFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoder changed source bytes\ngot:  %q\nwant: %q", got, want)
	}
}
