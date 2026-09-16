package model

import (
	"strings"
	"testing"
)

func TestInputKeysAreUnambiguous(t *testing.T) {
	decls := []Input{
		{Root: InputRootRepository, Path: "a.md", Section: &InputSection{HeadingPath: []string{"A/B"}}},
		{Root: InputRootRepository, Path: "a.md", Section: &InputSection{HeadingPath: []string{"A", "B"}}},
		{Root: InputRootRepository, Path: "a.md#A/B"},
		{Root: InputRootPlanning, Path: "a.md", Section: &InputSection{HeadingPath: []string{"A", "B"}}},
	}
	seen := map[string]bool{}
	for _, decl := range decls {
		key := InputKey(decl)
		if seen[key] {
			t.Fatalf("different input declarations share a fingerprint key: %s", key)
		}
		seen[key] = true
	}
}

func TestDuplicateInputsRefused(t *testing.T) {
	_, err := DecodeInputs([]byte(`[{"root":"repository","path":"a.md"},{"root":"repository","path":"a.md"}]`))
	if err == nil {
		t.Fatal("duplicate input declarations must be refused")
	}
}

func TestRetirementSourcesRemainToolOwned(t *testing.T) {
	raw := []byte(`{"version":1,"nodes":[],"retired":["old"],"retirement_sources":{"old":{"source":{"vcs":"git","revision":"1111111111111111111111111111111111111111","path":"old.md","source_id":"1.1"}}}}`)
	g, err := DecodeGraph(raw)
	if err != nil || g.RetirementSources["old"].Source.SourceID != "1.1" {
		t.Fatalf("graph retirement provenance did not round trip: %+v %v", g, err)
	}
	if _, err := DecodeProposal(raw); err == nil {
		t.Fatal("a proposal must not assert historical provenance")
	}
}

func TestRevisionLineageStrictRoundTripAndProposalRefusal(t *testing.T) {
	const oldRev = "1111111111111111111111111111111111111111"
	const midRev = "2222222222222222222222222222222222222222"
	const newRev = "3333333333333333333333333333333333333333"
	raw := []byte(`{"version":1,"revision_lineage":{"` + oldRev + `":"` + midRev + `","` + midRev + `":"` + newRev + `"},"nodes":[]}`)
	g, err := DecodeGraph(raw)
	if err != nil {
		t.Fatalf("decode lineage: %v", err)
	}
	if g.RevisionLineage[oldRev] != midRev || g.RevisionLineage[midRev] != newRev {
		t.Fatalf("lineage lost: %+v", g.RevisionLineage)
	}
	encoded, err := g.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeGraph(encoded)
	if err != nil {
		t.Fatalf("encoded lineage did not decode: %v\n%s", err, encoded)
	}
	if decoded.RevisionLineage[oldRev] != midRev || decoded.RevisionLineage[midRev] != newRev {
		t.Fatalf("round trip lost exact hashes: %+v", decoded.RevisionLineage)
	}
	if _, err := DecodeProposal(raw); err == nil {
		t.Fatal("a proposal must not assert tool-owned revision lineage")
	}
	upperOld, upperNew := strings.Repeat("A", 40), strings.Repeat("B", 40)
	upperRaw := []byte(`{"version":1,"revision_lineage":{"` + upperOld + `":"` + upperNew + `"},"nodes":[]}`)
	upperGraph, err := DecodeGraph(upperRaw)
	if err != nil {
		t.Fatalf("uppercase full commit IDs must decode: %v", err)
	}
	if upperGraph.RevisionLineage[upperOld] != upperNew {
		t.Fatalf("decode must preserve exact commit-ID spelling: %+v", upperGraph.RevisionLineage)
	}
	if chain := upperGraph.RevisionChain(strings.ToLower(upperOld)); len(chain) != 2 || chain[1] != upperNew {
		t.Fatalf("lineage lookup must compare commit identities case-insensitively: %v", chain)
	}
	upperEncoded, err := upperGraph.Encode()
	if err != nil || !strings.Contains(string(upperEncoded), `"`+upperOld+`": "`+upperNew+`"`) {
		t.Fatalf("encode must preserve exact commit-ID spelling: %v\n%s", err, upperEncoded)
	}
}

func TestRevisionLineageRejectsMalformedIdentityFanInAndCycles(t *testing.T) {
	const a = "1111111111111111111111111111111111111111"
	const b = "2222222222222222222222222222222222222222"
	const c = "3333333333333333333333333333333333333333"
	upper, lower := strings.Repeat("A", 40), strings.Repeat("a", 40)
	cases := []struct {
		name, lineage, want string
	}{
		{"short key", `{"abc":"` + b + `"}`, "revision_lineage.abc"},
		{"non-string value", `{"` + a + `":7}`, "must be a string"},
		{"short value", `{"` + a + `":"abc"}`, "full 40-character Git commit ID"},
		{"identity", `{"` + a + `":"` + a + `"}`, "self-mapping"},
		{"fan-in", `{"` + a + `":"` + c + `","` + b + `":"` + c + `"}`, "fan-in"},
		{"cycle", `{"` + a + `":"` + b + `","` + b + `":"` + a + `"}`, "cycle"},
		{"case alias", `{"` + upper + `":"` + b + `","` + lower + `":"` + b + `"}`, "commit identity conflicts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeGraph([]byte(`{"version":1,"revision_lineage":` + tc.lineage + `,"nodes":[]}`))
			wantFinding(t, err, tc.want)
		})
	}
}

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
	if strings.Contains(string(out), "intent_hashes") || strings.Contains(string(out), "artifact_digests") {
		t.Fatalf("removed legacy fields survived normalization:\n%s", out)
	}
	if _, err := DecodeGraph(out); err != nil {
		t.Fatalf("normalized graph does not decode: %v", err)
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
		"claim":        `"claim": {"by": "me", "lease_expires": "2026-08-31T00:00:00Z"}`,
		"verification": `"verification": {"result": "pass", "seq": 1, "isolation": "clean"}`,
		"red_seqs":     `"red_seqs": {"test_x": 1}`,
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

// TestInputDecodeRoundTrip: inputs decode strictly; a bad root selector, an
// empty heading path, and an unknown input key are all refusals.
func TestInputDecodeRoundTrip(t *testing.T) {
	src := `{"version": 1, "nodes": [{"id": "a", "contract": "c", "hazards": [],
		"gate": {"type": "tests"}, "inputs": [
			{"root": "repository", "path": "docs/a.md"},
			{"root": "planning", "path": "Plans/x/README.md",
			 "section": {"heading_path": ["A", "B"]}}
		]}]}`
	p, err := DecodeProposal([]byte(src))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	n := p.Nodes[0]
	if len(n.Inputs) != 2 {
		t.Fatalf("inputs = %d, want 2", len(n.Inputs))
	}
	if n.Inputs[0].Section != nil {
		t.Fatalf("first input should be whole-file, got section")
	}
	if n.Inputs[1].Section == nil || len(n.Inputs[1].Section.HeadingPath) != 2 {
		t.Fatalf("second input section = %+v", n.Inputs[1].Section)
	}

	// Bad root selector.
	_, err = DecodeInputs([]byte(`[{"root": "cwd", "path": "x.md"}]`))
	wantFinding(t, err, `"cwd" is not an input root`)

	// Empty heading path.
	_, err = DecodeInputs([]byte(`[{"root": "repository", "path": "x.md", "section": {"heading_path": []}}]`))
	wantFinding(t, err, "must be a nonempty list of heading titles")

	// Unknown key on the input object.
	_, err = DecodeInputs([]byte(`[{"root": "repository", "path": "x.md", "foo": 1}]`))
	wantFinding(t, err, `unknown key "foo"`)
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
