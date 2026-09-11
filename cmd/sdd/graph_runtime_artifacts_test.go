package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestGraphRuntimeArtifactBoundary(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root)

	want := []string{
		"Plans/P/P-Plan.md",
		"Plans/P/.graph.md",
		"Plans/P/nested/notes.md",
		"Plans/P/reviews/review.md",
		"Research/.graph/notes.md",
		"Research/.other/notes.md",
	}
	for _, rel := range want {
		writeRuntimeBoundaryFile(t, root, rel, []byte("not frontmatter\n"))
	}
	runtimeRel := "Plans/P/.graph/ws-X/runtime.md"
	writeRuntimeBoundaryFile(t, root, runtimeRel, []byte("not frontmatter\n"))

	stdout, stderr, err := runSdd(stressBinary(t), root, "validate", "--root", root, "--json")
	if err == nil {
		t.Fatalf("validate unexpectedly succeeded\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	var got struct {
		Artifacts []string `json:"artifacts_in_scope"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode validate output: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	sort.Strings(want)
	sort.Strings(got.Artifacts)
	if !reflect.DeepEqual(got.Artifacts, want) {
		t.Fatalf("discovered artifacts = %q, want %q; reserved runtime artifact %q must be excluded without suppressing similarly named paths", got.Artifacts, want, runtimeRel)
	}
}

func TestGraphRuntimeMigrationPreservesEvidence(t *testing.T) {
	for _, scope := range []string{"", "Plans", "Plans/P"} {
		t.Run(scope, func(t *testing.T) { testGraphRuntimeMigrationScope(t, scope) })
	}
}

func testGraphRuntimeMigrationScope(t *testing.T, scope string) {
	root := t.TempDir()
	writeConfig(t, root)

	ordinaryRel := "Plans/P/ordinary.md"
	runtimeRel := "Plans/P/.graph/ws-X/Research/evidence.md"
	source := []byte(validResearchDoc)
	ordinaryPath := writeRuntimeBoundaryFile(t, root, ordinaryRel, source)
	runtimePath := writeRuntimeBoundaryFile(t, root, runtimeRel, source)
	runtimeBefore, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}

	args := []string{"migrate", "--all", "--json"}
	if scope != "" {
		args = append(args, filepath.Join(root, filepath.FromSlash(scope)))
	}
	stdout, stderr, runErr := runSdd(stressBinary(t), root, args...)
	if runErr != nil {
		t.Fatalf("migrate --all: %v\nstdout: %s\nstderr: %s", runErr, stdout, stderr)
	}
	var got struct {
		Items []struct {
			Path string `json:"path"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode migrate output: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	var paths []string
	for _, item := range got.Items {
		paths = append(paths, item.Path)
	}
	if !reflect.DeepEqual(paths, []string{ordinaryRel}) {
		t.Errorf("migrate --all scanned %q, want only %q", paths, ordinaryRel)
	}
	ordinaryAfter, err := os.ReadFile(ordinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(ordinaryAfter, source) {
		t.Error("ordinary artifact was not migrated")
	}
	runtimeAfter, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeAfter, runtimeBefore) {
		t.Error("reserved graph runtime evidence changed during migrate --all")
	}
}

func writeRuntimeBoundaryFile(t *testing.T, root, rel string, content []byte) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
