package decisionview

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestForkKnownMissingSelectorRefusal(t *testing.T) {
	f := newTransactionFixture(t, "consumer-known-missing-selector")
	if err := os.WriteFile(f.forkPath, f.newFork, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(f.repository)
	if err != nil {
		t.Fatal(err)
	}
	ops := defaultConfigReplacementOps(0o644)
	ops.rename = func(*os.Root, string, string) error { return errors.New("retain approved config staging") }
	if err := replaceConfigFile(root, "planning-config.json", f.newConfig, ops); err == nil {
		root.Close()
		t.Fatal("fixture did not stop after removing the old config")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(f.repository)
	if err != nil {
		t.Fatal(err)
	}
	var stagedPath string
	for _, entry := range entries {
		if len(entry.Name()) > len(".planning-config.json.config-") && entry.Name()[:len(".planning-config.json.config-")] == ".planning-config.json.config-" {
			stagedPath = f.repository + string(os.PathSeparator) + entry.Name()
		}
	}
	if stagedPath == "" {
		t.Fatal("approved replacement sequence did not retain repository-local staging")
	}
	staged := readTransactionBytes(t, stagedPath)

	capture := CaptureForRepository(f.repository)
	if !capture.Declared {
		t.Fatal("retained transaction history did not preserve knowledge of removed fork authority")
	}
	if capture.View != nil {
		t.Fatalf("missing selector activated staged authority: %+v", capture.View)
	}
	if len(capture.Diagnostics) == 0 || (capture.Diagnostics[0].Severity != Error && capture.Diagnostics[0].Severity != Operational) {
		t.Fatalf("known missing selector did not fail closed: %+v", capture.Diagnostics)
	}
	if got := readTransactionBytes(t, stagedPath); !bytes.Equal(got, staged) {
		t.Fatal("read-side refusal repaired or rewrote retained config staging")
	}

	t.Run("history-free legacy remains a no-op", func(t *testing.T) {
		repository := t.TempDir()
		legacy := CaptureForRepository(repository)
		if legacy.Declared || legacy.View != nil || len(legacy.Diagnostics) != 0 {
			t.Fatalf("never-configured repository did not remain legacy: %+v", legacy)
		}
	})
}
