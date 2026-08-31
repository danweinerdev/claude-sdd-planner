package model

import (
	"strings"
	"testing"
)

// fullGraph is a canonical full-featured fixture: three nodes covering the
// three gate types, a claim, a verification with provenance, red_seqs,
// and all three hazards shapes (filled, untriaged, explicit empty).
// Its formatting matches Encode's output exactly so the round-trip test can
// byte-compare.
const fullGraph = `{
  "version": 1,
  "seq_counter": 17,
  "nodes": [
    {
      "id": "watch-config-file",
      "contract": "watcher emits a change event within 500ms of an mtime change",
      "justifies": [
        "AC-04",
        "FR-02"
      ],
      "intent_hashes": {
        "AC-04": "sha256:aaaa",
        "FR-02": "sha256:bbbb"
      },
      "deps": [
        "config-schema"
      ],
      "gate": {
        "type": "tests",
        "tests": [
          {
            "id": "test_watch_emits",
            "file": "tests/test_watch.py",
            "satisfies": [
              "frame-coupled"
            ]
          }
        ]
      },
      "hazards": [
        "frame-coupled"
      ],
      "artifacts": [
        "src/watch.py"
      ],
      "estimate": 2,
      "phase": "01-core",
      "claim": {
        "by": "agent-0192f3",
        "lease_expires": "2026-08-31T21:00:00Z",
        "workspace": "0192f3ab"
      },
      "verification": {
        "result": "pass",
        "seq": 17,
        "artifact_digests": {
          "src/watch.py": "sha256:cccc"
        },
        "report_digest": "sha256:dddd",
        "isolation": "clean",
        "provenance": {
          "kind": "git",
          "revision": "a1b2c3d4"
        }
      },
      "red_seqs": {
        "test_watch_emits": 12
      }
    },
    {
      "id": "build-gate",
      "contract": "the tree builds clean at every merge",
      "gate": {
        "type": "command",
        "command": "make build"
      },
      "hazards": "untriaged",
      "estimate": 1
    },
    {
      "id": "feature-review",
      "contract": "the config-watch feature survives a full validation cycle",
      "deps": [
        "watch-config-file",
        "build-gate"
      ],
      "gate": {
        "type": "review",
        "lanes": "full"
      },
      "hazards": [],
      "estimate": 1
    }
  ]
}
`

func decodeErrs(t *testing.T, err error) DecodeErrors {
	t.Helper()
	if err == nil {
		t.Fatal("expected decode errors, got nil")
	}
	es, ok := err.(DecodeErrors)
	if !ok {
		t.Fatalf("expected DecodeErrors, got %T: %v", err, err)
	}
	return es
}

func wantFinding(t *testing.T, err error, fragment string) {
	t.Helper()
	for _, e := range decodeErrs(t, err) {
		if strings.Contains(e.Error(), fragment) {
			return
		}
	}
	t.Fatalf("no finding contains %q; got:\n%v", fragment, err)
}

func TestRoundTrip(t *testing.T) {
	g, err := DecodeGraph([]byte(fullGraph))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	out, err := g.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(out) != fullGraph {
		t.Fatalf("round-trip is not byte-identical.\n--- got ---\n%s\n--- want ---\n%s", out, fullGraph)
	}
}

func TestDecodedShapes(t *testing.T) {
	g, err := DecodeGraph([]byte(fullGraph))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if g.SeqCounter != 17 || len(g.Nodes) != 3 {
		t.Fatalf("unexpected graph shape: %+v", g)
	}
	watch := g.NodeByID("watch-config-file")
	if watch == nil || watch.Estimate != 2 || watch.Claim == nil || watch.Verification == nil {
		t.Fatalf("watch node decoded wrong: %+v", watch)
	}
	if watch.Verification.Provenance.Kind != "git" {
		t.Fatalf("provenance lost: %+v", watch.Verification)
	}
	if watch.RedSeqs["test_watch_emits"] != 12 {
		t.Fatalf("red_seqs lost: %+v", watch.RedSeqs)
	}
	// The three hazards shapes are distinct claims and must decode distinctly.
	if watch.Hazards == nil || len(watch.Hazards) != 1 {
		t.Fatalf("filled hazards decoded wrong: %#v", watch.Hazards)
	}
	if g.NodeByID("build-gate").Hazards != nil {
		t.Fatalf("untriaged must decode to nil, got %#v", g.NodeByID("build-gate").Hazards)
	}
	review := g.NodeByID("feature-review")
	if review.Hazards == nil || len(review.Hazards) != 0 {
		t.Fatalf("explicit empty hazards must decode to a non-nil empty slice, got %#v", review.Hazards)
	}
	if review.Gate.Lanes != nil {
		t.Fatalf(`lanes "full" must decode to nil, got %#v`, review.Gate.Lanes)
	}
	// Estimate defaults to 1 when absent.
	minimal := `{"version": 1, "nodes": [{"id": "a", "contract": "c", "gate": {"type": "tests"}, "hazards": []}]}`
	mg, err := DecodeGraph([]byte(minimal))
	if err != nil {
		t.Fatalf("minimal decode: %v", err)
	}
	if mg.Nodes[0].Estimate != 1 {
		t.Fatalf("estimate must default to 1, got %d", mg.Nodes[0].Estimate)
	}
}

func TestUnknownKeyDidYouMean(t *testing.T) {
	src := `{"version": 1, "nodes": [{"id": "a", "contract": "c", "hazards": [],
		"gate": {"type": "tests", "tets": []}}]}`
	_, err := DecodeGraph([]byte(src))
	wantFinding(t, err, `nodes[0].gate: unknown key "tets" — did you mean "tests"?`)
}

func TestUnknownKeyWithoutNearMatch(t *testing.T) {
	src := `{"version": 1, "nodes": [{"id": "a", "contract": "c", "hazards": [],
		"gate": {"type": "tests"}, "zzzzzzzzzz": 1}]}`
	_, err := DecodeGraph([]byte(src))
	for _, e := range decodeErrs(t, err) {
		if strings.Contains(e.Msg, "zzzzzzzzzz") {
			if strings.Contains(e.Msg, "did you mean") {
				t.Fatalf("no suggestion should be offered for a distant key: %v", e)
			}
			return
		}
	}
	t.Fatalf("unknown key not reported: %v", err)
}

func TestVersionHandling(t *testing.T) {
	_, err := DecodeGraph([]byte(`{"version": 2, "nodes": []}`))
	wantFinding(t, err, "schema version 2 is unsupported; this sdd supports version 1")
	if strings.Contains(err.Error(), "migrate") {
		t.Fatalf("no migrate verb exists; the error must not reference one: %v", err)
	}
	_, err = DecodeGraph([]byte(`{"nodes": []}`))
	wantFinding(t, err, "version: missing required field")
}

func TestEnumRejections(t *testing.T) {
	base := `{"version": 1, "nodes": [{"id": "a", "contract": "c", "hazards": [],
		"gate": {"type": "tests"},
		"verification": {"result": %q, "seq": 1, "isolation": %q}}]}`
	src := strings.ReplaceAll(base, "%q", `"ok"`)
	_, err := DecodeGraph([]byte(src))
	wantFinding(t, err, `nodes[0].verification.result: "ok" is not a result; valid results are "pass" and "fail"`)
	wantFinding(t, err, `nodes[0].verification.isolation: "ok" is not an isolation level`)

	_, err = DecodeGraph([]byte(`{"version": 1, "nodes": [{"id": "a", "contract": "c",
		"hazards": [], "gate": {"type": "manual"}}]}`))
	wantFinding(t, err, `nodes[0].gate.type: "manual" is not a gate type; valid types are "tests", "command", "review"`)
}

func TestEstimateRejections(t *testing.T) {
	_, err := DecodeGraph([]byte(`{"version": 1, "nodes": [{"id": "a", "contract": "c",
		"hazards": [], "gate": {"type": "tests"}, "estimate": 0}]}`))
	wantFinding(t, err, "nodes[0].estimate: must be >= 1")

	_, err = DecodeGraph([]byte(`{"version": 1, "nodes": [{"id": "a", "contract": "c",
		"hazards": [], "gate": {"type": "tests"}, "estimate": 1.5}]}`))
	wantFinding(t, err, "nodes[0].estimate: must be an integer, got 1.5")
}

func TestHazardsRejections(t *testing.T) {
	_, err := DecodeGraph([]byte(`{"version": 1, "nodes": [{"id": "a", "contract": "c",
		"hazards": "unknown-sentinel", "gate": {"type": "tests"}}]}`))
	wantFinding(t, err, `nodes[0].hazards: "unknown-sentinel" is not a hazards value`)

	_, err = DecodeGraph([]byte(`{"version": 1, "nodes": [{"id": "a", "contract": "c",
		"hazards": 42, "gate": {"type": "tests"}}]}`))
	wantFinding(t, err, "nodes[0].hazards: must be a list of failure classes")

	_, err = DecodeGraph([]byte(`{"version": 1, "nodes": [{"id": "a", "contract": "c",
		"gate": {"type": "tests"}}]}`))
	wantFinding(t, err, "nodes[0].hazards: missing required field")
}

func TestBatchedErrors(t *testing.T) {
	// One pass reports every finding: bad version, missing contract, bad
	// gate type, unknown key. Four problems, one error.
	src := `{"version": 3, "nodes": [{"id": "a", "hazards": [],
		"gate": {"type": "manual"}, "contrct": "typo"}]}`
	_, err := DecodeGraph([]byte(src))
	es := decodeErrs(t, err)
	if len(es) < 4 {
		t.Fatalf("expected at least 4 batched findings, got %d:\n%v", len(es), err)
	}
	wantFinding(t, err, "schema version 3 is unsupported")
	wantFinding(t, err, "nodes[0].contract: missing required field")
	wantFinding(t, err, `"manual" is not a gate type`)
	wantFinding(t, err, `did you mean "contract"?`)
}

func TestProposalRejectsToolOwnedFields(t *testing.T) {
	cases := map[string]string{
		"intent_hashes": `"intent_hashes": {"AC-01": "sha256:x"}`,
		"claim":         `"claim": {"by": "me", "lease_expires": "2026-08-31T00:00:00Z"}`,
		"verification":  `"verification": {"result": "pass", "seq": 1, "isolation": "clean"}`,
		"red_seqs":      `"red_seqs": {"test_x": 1}`,
	}
	for key, field := range cases {
		src := `{"version": 1, "nodes": [{"id": "a", "contract": "c", "hazards": [],
			"gate": {"type": "tests"}, ` + field + `}]}`
		_, err := DecodeProposal([]byte(src))
		wantFinding(t, err, "nodes[0]."+key+": tool-owned field rejected in payloads")
	}
	// The same document decodes fine as a master graph.
	src := `{"version": 1, "seq_counter": 1, "nodes": [{"id": "a", "contract": "c",
		"hazards": [], "gate": {"type": "tests"}, "red_seqs": {"test_x": 1}}]}`
	if _, err := DecodeGraph([]byte(src)); err != nil {
		t.Fatalf("graph decode of tool-owned fields must succeed: %v", err)
	}
	// seq_counter is itself tool-owned at the proposal level.
	_, err := DecodeProposal([]byte(`{"version": 1, "seq_counter": 5, "nodes": []}`))
	wantFinding(t, err, `unknown key "seq_counter"`)
}

func TestLanesRejections(t *testing.T) {
	_, err := DecodeGraph([]byte(`{"version": 1, "nodes": [{"id": "a", "contract": "c",
		"hazards": [], "gate": {"type": "review", "lanes": "some"}}]}`))
	wantFinding(t, err, `nodes[0].gate.lanes: "some" is not a lane set`)

	_, err = DecodeGraph([]byte(`{"version": 1, "nodes": [{"id": "a", "contract": "c",
		"hazards": [], "gate": {"type": "review", "lanes": []}}]}`))
	wantFinding(t, err, "an empty lane list selects nothing")
}

func TestSyntaxAndDocumentShape(t *testing.T) {
	_, err := DecodeGraph([]byte(`{"version": 1,`))
	wantFinding(t, err, "JSON syntax error")

	_, err = DecodeGraph([]byte(`[]`))
	wantFinding(t, err, "document must be a JSON object, got list")

	_, err = DecodeGraph([]byte(`{"version": 1, "nodes": []} {"extra": true}`))
	wantFinding(t, err, "trailing content after the JSON document")
}
