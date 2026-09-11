package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func TestForkSessionStartRealEntryPoint(t *testing.T) {
	root := forkSessionOverrideFixture(t, "effective local replacement")
	before := forkReadSnapshot(t, root)
	context := runForkSessionStart(t, root)

	for _, want := range []string{
		qualified(forkReadLocal, "D-0001"),
		"effective local replacement",
		"planning:Decisions/fork.md",
		qualified(forkReadLocal, "D-0100"),
		"ordinary local rule",
	} {
		if !strings.Contains(context, want) {
			t.Errorf("real SessionStart context omits effective qualified authority %q: %q", want, context)
		}
	}
	if strings.Contains(context, "parent authority rule") || strings.Contains(context, qualified(forkReadParent, "D-0001")) {
		t.Errorf("real SessionStart injected the hidden inherited original as a competing instruction: %q", context)
	}
	if !reflect.DeepEqual(before, forkReadSnapshot(t, root)) {
		t.Fatal("SessionStart changed persisted fixture bytes or created read-side support files")
	}

	t.Run("legacy missing config and ledger is a silent no-op", func(t *testing.T) {
		legacy := t.TempDir()
		cmd := exec.Command(stressBinary(t), "hook", "sessionstart")
		cmd.Dir = legacy
		cmd.Stdin = strings.NewReader(`{"cwd":` + mustJSON(t, legacy) + `}`)
		if output, err := cmd.CombinedOutput(); err != nil || len(output) != 0 {
			t.Fatalf("legacy missing ledger must be a successful silent no-op: err=%v output=%q", err, output)
		}
	})

	t.Run("malformed stdin remains nonfatal", func(t *testing.T) {
		cmd := exec.Command(stressBinary(t), "hook", "sessionstart")
		cmd.Dir = root
		cmd.Stdin = strings.NewReader(`{"cwd":`)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("malformed hook stdin must remain nonfatal: %v: %s", err, output)
		}
		var envelope map[string]any
		if err := json.Unmarshal(output, &envelope); err != nil {
			t.Fatalf("malformed stdin fallback emitted invalid hook JSON: %v: %q", err, output)
		}
	})
}

func TestForkSessionStartHostileJSON(t *testing.T) {
	const hostile = "yes no true false null\n\"quoted\" \\ slash 雪"
	root := forkSessionOverrideFixture(t, hostile)
	before := forkReadSnapshot(t, root)
	context := runForkSessionStart(t, root)
	if !strings.Contains(context, hostile) {
		t.Fatalf("hostile persisted statement did not survive hook JSON exactly: %q", context)
	}
	if !reflect.DeepEqual(before, forkReadSnapshot(t, root)) {
		t.Fatal("hostile SessionStart read changed source bytes or created support files")
	}
}

func forkSessionOverrideFixture(t *testing.T, statement string) string {
	t.Helper()
	root := forkReadFixture(t, false, false, "ordinary local rule")
	planning := filepath.Join(root, ".plans")
	capture := decisionview.CaptureForRepository(root)
	local, parent := capture.Collections[forkReadLocal], capture.Collections[forkReadParent]
	if local == nil || local.Metadata == nil || parent == nil {
		t.Fatalf("persisted fork fixture did not resolve: %+v", capture.Diagnostics)
	}
	metadata := *local.Metadata
	target := decisionview.QualifiedID(qualified(forkReadParent, "D-0001"))
	basis, err := decisionview.CreateBasis(metadata.ParentBindingID, parent, target, []string{metadata.ParentBindingID})
	if err != nil {
		t.Fatal(err)
	}
	relation, err := json.Marshal(decisionview.OverrideDeclaration{Target: target, Basis: basis})
	if err != nil {
		t.Fatal(err)
	}
	var override map[string]any
	if err := json.Unmarshal(relation, &override); err != nil {
		t.Fatal(err)
	}
	replacement := forkReadEntry("D-0001", "accepted", statement)
	replacement["override"] = override
	forkReadWriteLedger(t, planning, "Decisions/fork.md", []map[string]any{
		replacement,
		forkReadEntry("D-0100", "accepted", "ordinary local rule"),
	}, metadata)
	return root
}

func runForkSessionStart(t *testing.T, root string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"cwd":               root,
		"hostile_extension": map[string]any{"bool": true, "null": nil, "number": 1.25, "text": "\\\"雪"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(stressBinary(t), "hook", "sessionstart")
	cmd.Dir = root
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real hook entry point failed: %v: %s", err, output)
	}
	var envelope struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		t.Fatalf("real hook did not emit valid protocol JSON: %v: %q", err, output)
	}
	if envelope.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Fatalf("wrong hook event envelope: %q", output)
	}
	return envelope.HookSpecificOutput.AdditionalContext
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
