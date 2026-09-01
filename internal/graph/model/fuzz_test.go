package model

// Fuzz coverage for the strict decoders (Designs/SddGraph § Structural
// Verification): payloads arrive from LLM sessions and humans, graphs from
// disk anyone can touch — both are hostile external input by design. The
// contract is STRUCTURED ERRORS ONLY: a decoder may refuse anything, and
// must panic on nothing.
//
// Exploration runs locally (the corpus below replays as ordinary tests):
//
//	go test ./internal/graph/model/ -fuzz FuzzDecode -fuzztime 60s -run xxx
//
// Discovered crashers land in testdata/fuzz/FuzzDecode/ and replay on every
// plain `go test` run thereafter.

import (
	"os"
	"path/filepath"
	"testing"
)

// hostileJSON is the curated corpus: every input class that has bitten a
// JSON consumer somewhere — deep nesting, wrong containers, nulls where
// objects go, duplicate keys, huge numbers, invalid UTF-8, truncations.
var hostileJSON = [][]byte{
	nil,
	[]byte(""),
	[]byte("null"),
	[]byte("[]"),
	[]byte(`""`),
	[]byte("0"),
	[]byte("{"),
	[]byte(`{"version": 1`),
	[]byte(`{"version": null, "nodes": null}`),
	[]byte(`{"version": 999999999999999999999999, "nodes": []}`),
	[]byte(`{"version": 1, "nodes": [null]}`),
	[]byte(`{"version": 1, "nodes": [[]]}`),
	[]byte(`{"version": 1, "nodes": [{"id": null}]}`),
	[]byte(`{"version": 1, "nodes": [{"id": {"a": "b"}}]}`),
	[]byte(`{"version": 1, "nodes": [{"id": "a", "gate": "tests"}]}`),
	[]byte(`{"version": 1, "nodes": [{"id": "a", "gate": {"type": ["tests"]}}]}`),
	[]byte(`{"version": 1, "nodes": [{"id": "a", "hazards": 7}]}`),
	[]byte(`{"version": 1, "nodes": [{"id": "a", "estimate": "three"}]}`),
	[]byte(`{"version": 1, "nodes": [{"id": "a", "estimate": 1e308}]}`),
	[]byte(`{"version": 1, "version": 2, "nodes": []}`),
	[]byte(`{"version": 1, "nodes": [{"id": "\xff\xfe"}]}`),
	[]byte("{\"version\": 1, \"nodes\": [{\"id\": \"\xff\xfe\"}]}"),
	[]byte(`{"version": 1, "nodes": [{"id": "a", "deps": [["nested"]]}]}`),
	[]byte(`{"version": 1, "nodes": [{"id": "a", "red_seqs": {"t": "one"}}]}`),
	[]byte(`{"version": 1, "nodes": [{"id": "a", "claim": []}]}`),
	[]byte(`{"version": 1, "nodes": [{"id": "a", "verification": {"result": 1}}]}`),
	[]byte(`[{"version": 1}]`),
	[]byte(`{"nodes": [{"gate": {"tests": [{"satisfies": null}]}}]}`),
}

// deepJSON builds pathological nesting without a giant literal.
func deepJSON(depth int) []byte {
	out := []byte(`{"version": 1, "nodes": `)
	for i := 0; i < depth; i++ {
		out = append(out, '[')
	}
	for i := 0; i < depth; i++ {
		out = append(out, ']')
	}
	return append(out, '}')
}

func FuzzDecode(f *testing.F) {
	for _, seed := range hostileJSON {
		f.Add(seed)
	}
	f.Add(deepJSON(2000))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Refusal is fine, success is fine; a panic fails the run.
		_, _ = DecodeProposal(data)
		_, _ = DecodeGraph(data)
	})
}

// TestFuzzCorpusDecode is the CI-friendly regression mode: the curated
// corpus plus every committed crasher replays as an ordinary test.
func TestFuzzCorpusDecode(t *testing.T) {
	for i, data := range append(append([][]byte{}, hostileJSON...), deepJSON(2000)) {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("corpus[%d] panicked: %v\ninput: %q", i, r, data)
				}
			}()
			_, _ = DecodeProposal(data)
			_, _ = DecodeGraph(data)
		}()
	}
	// Committed crashers, if any, in Go's native corpus location. Their
	// encoded form replays through the Fuzz target on plain `go test`;
	// this walk just asserts the directory is intact when present.
	dir := filepath.Join("testdata", "fuzz", "FuzzDecode")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d committed crasher(s) replay via FuzzDecode seed corpus", len(entries))
}
