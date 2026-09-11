package decisionview

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

const configReplacementStage = ".planning-config.json.replacement-test"

func TestConfigReplacementStagedSequence(t *testing.T) {
	t.Run("injected operation order", func(t *testing.T) {
		var events []string
		old := []byte("old config")
		want := []byte("complete new config")
		target := append([]byte(nil), old...)
		staged := &recordingConfigFile{events: &events, want: want, target: &target, old: old}
		ops := configReplacementOps{
			create: func(*os.Root, string) (configReplacementFile, string, error) {
				events = append(events, "create")
				return staged, configReplacementStage, nil
			},
			remove: func(*os.Root, string) error {
				events = append(events, "remove")
				if !staged.synced || !staged.closed || !bytes.Equal(target, old) {
					return errors.New("old config removed before replacement was complete, flushed, and closed")
				}
				target = nil
				return nil
			},
			rename: func(*os.Root, string, string) error {
				events = append(events, "rename")
				if target != nil {
					return errors.New("rename happened before old config removal")
				}
				target = append([]byte(nil), staged.bytes...)
				return nil
			},
			syncDir: func(*os.Root) error {
				events = append(events, "sync-dir")
				return nil
			},
		}

		err := replaceConfigFile(nil, "planning-config.json", want, ops)
		if err != nil {
			t.Errorf("replace config: %v", err)
		}
		if wantEvents := []string{"create", "write", "sync", "close", "remove", "rename", "sync-dir"}; !reflect.DeepEqual(events, wantEvents) {
			t.Errorf("replacement events = %v, want %v", events, wantEvents)
		}
		if !bytes.Equal(target, want) {
			t.Errorf("final config = %q, want %q", target, want)
		}
	})

	t.Run("real filesystem round trip", func(t *testing.T) {
		root, cleanup := configReplacementRoot(t, []byte("old config"))
		defer cleanup()
		want := []byte("complete new config")

		if err := replaceConfigFile(root, "planning-config.json", want, realConfigReplacementOps(nil, nil)); err != nil {
			t.Errorf("replace config: %v", err)
		}
		got, err := root.ReadFile("planning-config.json")
		if err != nil {
			t.Fatalf("read replaced config: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("replaced config = %q, want %q", got, want)
		}
	})
}

func TestConfigReplacementFailurePreservesStagedBytes(t *testing.T) {
	want := []byte("complete staged config")

	t.Run("pre-remove error leaves old file", func(t *testing.T) {
		old := []byte("old config")
		root, cleanup := configReplacementRoot(t, old)
		defer cleanup()
		injected := errors.New("injected remove failure")
		ops := realConfigReplacementOps(func(*os.Root, string) error { return injected }, nil)

		err := replaceConfigFile(root, "planning-config.json", want, ops)
		if !errors.Is(err, injected) {
			t.Errorf("replacement error = %v, want injected remove error", err)
		}
		got, readErr := root.ReadFile("planning-config.json")
		if readErr != nil || !bytes.Equal(got, old) {
			t.Errorf("old config after remove error = %q, %v; want %q", got, readErr, old)
		}
		staged, stageErr := root.ReadFile(configReplacementStage)
		if stageErr != nil || !bytes.Equal(staged, want) {
			t.Errorf("staged config after remove error = %q, %v; want %q", staged, stageErr, want)
		}
	})

	t.Run("failed rename after remove keeps staged bytes", func(t *testing.T) {
		root, cleanup := configReplacementRoot(t, []byte("old config"))
		defer cleanup()
		injected := errors.New("injected rename failure")
		ops := realConfigReplacementOps(nil, func(*os.Root, string, string) error { return injected })

		err := replaceConfigFile(root, "planning-config.json", want, ops)
		if !errors.Is(err, injected) {
			t.Errorf("replacement error = %v, want injected rename error", err)
		}
		if _, statErr := root.Stat("planning-config.json"); !os.IsNotExist(statErr) {
			t.Errorf("old config still exists after successful remove: %v", statErr)
		}
		staged, stageErr := root.ReadFile(configReplacementStage)
		if stageErr != nil || !bytes.Equal(staged, want) {
			t.Errorf("staged config after rename error = %q, %v; want %q", staged, stageErr, want)
		}
	})

	t.Run("post-rename directory sync error reports installed replacement", func(t *testing.T) {
		root, cleanup := configReplacementRoot(t, []byte("old config"))
		defer cleanup()
		injected := errors.New("injected directory sync failure")
		ops := realConfigReplacementOps(nil, nil)
		ops.syncDir = func(*os.Root) error { return injected }

		err := replaceConfigFile(root, "planning-config.json", want, ops)
		if !errors.Is(err, injected) || !strings.Contains(err.Error(), "installed") || strings.Contains(err.Error(), "retained staged") {
			t.Errorf("post-rename sync error = %v, want installed replacement without retained-stage claim", err)
		}
		got, readErr := root.ReadFile("planning-config.json")
		if readErr != nil || !bytes.Equal(got, want) {
			t.Errorf("installed config after directory sync error = %q, %v; want %q", got, readErr, want)
		}
	})
}

func TestConfigReplacementDoesNotChangeLedgerPublisher(t *testing.T) {
	rootName := t.TempDir()
	root, err := os.OpenRoot(rootName)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.WriteFile("ledger.md", []byte("old ledger"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Ledger publication remains covered by the existing platform publisher;
	// the represented-repository config replacement seam is not involved.
	if err := publishTransactionFile(root, "ledger.md", []byte("new ledger"), true); err != nil {
		t.Fatalf("existing ledger publisher: %v", err)
	}
	got, err := root.ReadFile("ledger.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("new ledger")) {
		t.Errorf("published ledger = %q, want new ledger", got)
	}
}

func TestPublishForkTransactionFileRoutesOnlyRepositoryConfig(t *testing.T) {
	t.Run("repository config uses simple replacement", func(t *testing.T) {
		root, cleanup := configReplacementRoot(t, []byte("old config"))
		defer cleanup()
		if err := publishForkTransactionFile(SourceRootRepository, root, "planning-config.json", []byte("new config"), true); err != nil {
			t.Fatal(err)
		}
		got, err := root.ReadFile("planning-config.json")
		if err != nil || !bytes.Equal(got, []byte("new config")) {
			t.Errorf("repository config = %q, %v; want new config", got, err)
		}
	})

	t.Run("planning file with config basename uses generic publisher", func(t *testing.T) {
		root, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if err := publishForkTransactionFile(SourceRootPlanning, root, "planning-config.json", []byte("ledger bytes"), false); err != nil {
			t.Fatalf("generic planning publication: %v", err)
		}
		if got, err := root.ReadFile("planning-config.json"); err != nil || !bytes.Equal(got, []byte("ledger bytes")) {
			t.Errorf("planning-root config-named ledger = %q, %v", got, err)
		}
	})

	t.Run("ordinary ledger uses generic publisher", func(t *testing.T) {
		root, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if err := publishForkTransactionFile(SourceRootPlanning, root, "Decisions/fork.md", []byte("ledger bytes"), false); err != nil {
			t.Fatalf("generic ledger publication: %v", err)
		}
		if got, err := root.ReadFile("Decisions/fork.md"); err != nil || !bytes.Equal(got, []byte("ledger bytes")) {
			t.Errorf("ordinary ledger = %q, %v", got, err)
		}
	})

	t.Run("new repository config is refused", func(t *testing.T) {
		root, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		err = publishForkTransactionFile(SourceRootRepository, root, "planning-config.json", []byte("new config"), false)
		if err == nil || !strings.Contains(err.Error(), "requires an existing regular file") {
			t.Errorf("new repository config error = %v, want explicit refusal", err)
		}
	})
}

type recordingConfigFile struct {
	events *[]string
	want   []byte
	target *[]byte
	old    []byte
	bytes  []byte
	synced bool
	closed bool
}

func (f *recordingConfigFile) Write(p []byte) (int, error) {
	*f.events = append(*f.events, "write")
	if !bytes.Equal(*f.target, f.old) {
		return 0, errors.New("old config changed while staging")
	}
	f.bytes = append(f.bytes, p...)
	return len(p), nil
}

func (f *recordingConfigFile) Sync() error {
	*f.events = append(*f.events, "sync")
	if !bytes.Equal(f.bytes, f.want) {
		return errors.New("staged file was flushed before it was complete")
	}
	f.synced = true
	return nil
}

func (f *recordingConfigFile) Close() error {
	*f.events = append(*f.events, "close")
	if !f.synced {
		return errors.New("staged file was closed before flush")
	}
	f.closed = true
	return nil
}

func configReplacementRoot(t *testing.T, old []byte) (*os.Root, func()) {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("planning-config.json", old, 0o644); err != nil {
		root.Close()
		t.Fatal(err)
	}
	return root, func() { _ = root.Close() }
}

func realConfigReplacementOps(
	remove func(*os.Root, string) error,
	rename func(*os.Root, string, string) error,
) configReplacementOps {
	if remove == nil {
		remove = func(root *os.Root, name string) error { return root.Remove(name) }
	}
	if rename == nil {
		rename = func(root *os.Root, old, new string) error { return root.Rename(old, new) }
	}
	return configReplacementOps{
		create: func(root *os.Root, _ string) (configReplacementFile, string, error) {
			file, err := root.OpenFile(configReplacementStage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
			return file, configReplacementStage, err
		},
		remove: remove,
		rename: rename,
	}
}
