// Package decisionview_test checks the fork-aware model wire contract. The
// named gate tests were observed failing against the permissive model before
// strict decoding was implemented.
//
// Gate tests:
//   - TestForkModelHostileRoundTrip — round-trip JSON/YAML through every
//     typed model level with reserved words, quoted scalars, newlines, and
//     duplicate keys, then verify the re-parsed output preserves or rejects
//     faithfully. Must detect lossy/unsafe format handling (hazard
//     external-format).
//   - TestForkModelRejectsUnsupportedVersions — zero, negative, and
//     unsupported (>1) schema versions are rejected at parse time; version 1
//     is accepted.
package decisionview_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

// ---------------------------------------------------------------------------
// Hostile round-trip test  (gate: TestForkModelHostileRoundTrip)
// ---------------------------------------------------------------------------
//
// Hazard external-format requires that reserved words, quotes, and newlines
// survive a round trip through JSON and YAML, and that the re-parsed output
// does not silently lose information. This test exercises every typed model
// level with hostile values.
//
// Regression classes exercised against the permissive baseline:
//   - JSON duplicate keys in ForkConfig are accepted by DecodeForkConfig
//     (encoding/json's DisallowUnknownFields does NOT detect duplicate keys).
//   - Unknown enum values must not be accepted as free-form string fields.
//   - Binding/OverrideBasis/AuthorityEvent have no custom JSON/YAML
//     unmarshallers, so unknown fields are silently accepted (FR-17,
//     DD-4 require rejection of unsupported fields).
//   - Default JSON struct marshalling does not guarantee byte-exact
//     round-trip for RawMessage-like fields in all embedded contexts.
//
// Valid free-form text must survive both encodings without losing values.

func TestForkModelHostileRoundTrip(t *testing.T) {
	// ---- SchemaVersion hostile values ----
	t.Run("SchemaVersion", func(t *testing.T) {
		// Version 1 must round-trip cleanly through JSON.
		ver := decisionview.Version1
		b, err := json.Marshal(ver)
		if err != nil {
			t.Fatalf("marshal Version1: %v", err)
		}
		var got decisionview.SchemaVersion
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal Version1: %v", err)
		}
		if got != decisionview.Version1 {
			t.Fatalf("Version1 round trip: got %v, want %v", got, decisionview.Version1)
		}

		// YAML: version 1 parses from an integer YAML node.
		var yn yaml.Node
		if err := yaml.Unmarshal([]byte("1"), &yn); err != nil {
			t.Fatalf("yaml parse 1: %v", err)
		}
		var ygot decisionview.SchemaVersion
		if err := ygot.UnmarshalYAML(&yn); err != nil {
			t.Fatalf("unmarshal YAML Version1: %v", err)
		}
		if ygot != decisionview.Version1 {
			t.Fatalf("YAML Version1: got %v, want %v", ygot, decisionview.Version1)
		}

		// YAML: version value must be integer, not string.
		var ys yaml.Node
		if err := yaml.Unmarshal([]byte(`"1"`), &ys); err != nil {
			t.Fatalf("yaml parse string 1: %v", err)
		}
		var ybad decisionview.SchemaVersion
		if err := ybad.UnmarshalYAML(&ys); err == nil {
			t.Fatal("YAML string version should be rejected (must be integer)")
		}
	})

	// ---- CollectionID / OwnerID with hostile strings ----
	t.Run("CollectionID", func(t *testing.T) {
		// Valid lowercase UUID survives.
		cid := decisionview.CollectionID("01234567-89ab-cdef-0123-456789abcdef")
		if err := cid.Validate(); err != nil {
			t.Fatalf("valid collection id rejected: %v", err)
		}
		// Uppercase UUID must fail.
		bad := decisionview.CollectionID("01234567-89AB-CDEF-0123-456789ABCDEF")
		if err := bad.Validate(); err == nil {
			t.Fatal("uppercase collection ID should be rejected")
		}
		// Empty UUID must fail.
		empty := decisionview.CollectionID("")
		if err := empty.Validate(); err == nil {
			t.Fatal("empty collection ID should be rejected")
		}
	})

	t.Run("OwnerID", func(t *testing.T) {
		oid := decisionview.OwnerID("fedcba98-7654-3210-fedc-ba9876543210")
		if err := oid.Validate(); err != nil {
			t.Fatalf("valid owner id rejected: %v", err)
		}
	})

	// ---- SourceLocator must reject invalid root enum and empty path ----
	t.Run("SourceLocator", func(t *testing.T) {
		sl := &decisionview.SourceLocator{
			Root: decisionview.SourceRootPlanning,
			Path: "Decisions/decisions.md",
		}
		if err := sl.Validate(); err != nil {
			t.Fatalf("valid source locator rejected: %v", err)
		}
		// Invalid root enum.
		bad := &decisionview.SourceLocator{
			Root: decisionview.SourceRoot("garbage"),
			Path: "Decisions/decisions.md",
		}
		if err := bad.Validate(); err == nil {
			t.Fatal("invalid source root should be rejected")
		}
		// Empty path.
		nopath := &decisionview.SourceLocator{
			Root: decisionview.SourceRootRepository,
			Path: "",
		}
		if err := nopath.Validate(); err == nil {
			t.Fatal("empty source path should be rejected")
		}
	})

	// ---- ForkConfig: duplicate JSON keys must be rejected ----
	//
	// RED stage FAIL: DecodeForkConfig uses DisallowUnknownFields but
	// encoding/json does NOT detect duplicate keys. The test asserts
	// rejection that the skeleton cannot yet provide.
	t.Run("ForkConfig/duplicateKeys", func(t *testing.T) {
		duplicateJSON := `{
			"version": 1,
			"mode": "fork",
			"path": "first.md",
			"path": "second.md",
			"ledgerId": "01234567-89ab-cdef-0123-456789abcdef"
		}`
		_, err := decisionview.DecodeForkConfig([]byte(duplicateJSON))
		if err == nil {
			t.Fatal("ForkConfig with duplicate JSON keys must be rejected (DD-4)")
		}
	})

	// A reserved word is not a valid mode. Free-form hostile strings are
	// round-tripped in statements below, not accepted as unknown enum values.
	t.Run("ForkConfig/yamlReservedWord", func(t *testing.T) {
		yamlInput := `version: 1
mode: yes
path: Decisions/fork.md
ledgerId: "01234567-89ab-cdef-0123-456789abcdef"
`
		if _, err := decisionview.DecodeForkConfigYAML([]byte(yamlInput)); err == nil {
			t.Fatal("YAML mode yes must be rejected as an unsupported enum value")
		}
	})

	// ---- OverrideBasis: unknown JSON fields must be rejected ----
	//
	// RED stage FAIL: OverrideBasis has no custom UnmarshalJSON, so
	// unknown fields are silently accepted.
	t.Run("OverrideBasis/unknownFields", func(t *testing.T) {
		badJSON := `{
			"version": 1,
			"targetId": "ledger:01234567-89ab-cdef-0123-456789abcdef:D-0001",
			"unknownField": "must be rejected"
		}`
		var ob decisionview.OverrideBasis
		err := json.Unmarshal([]byte(badJSON), &ob)
		if err == nil {
			t.Fatal("OverrideBasis with unknown JSON fields must be rejected")
		}
	})

	// ---- OverrideBasis: newlines and quotes in canonical content survive JSON ----
	t.Run("OverrideBasis/hostileContent", func(t *testing.T) {
		hostileContent := `{"statement":"line 1\nline 2","rationale":"he said \"no\"","scope":["yes","no","true","false"]}`
		ob := &decisionview.OverrideBasis{
			Version:          decisionview.Version1,
			TargetID:         "ledger:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee:D-0042",
			CanonicalContent: json.RawMessage(hostileContent),
		}
		b, err := json.Marshal(ob)
		if err != nil {
			t.Fatalf("marshal hostile basis: %v", err)
		}
		var ob2 decisionview.OverrideBasis
		if err := json.Unmarshal(b, &ob2); err != nil {
			t.Fatalf("unmarshal hostile basis: %v", err)
		}
		if string(ob2.CanonicalContent) != hostileContent {
			t.Fatalf("hostile content round trip:\n  got:  %q\n  want: %q",
				string(ob2.CanonicalContent), hostileContent)
		}
	})

	// ---- AuthorityEvent: unknown fields must be rejected ----
	//
	// RED stage FAIL: AuthorityEvent has no custom UnmarshalJSON.
	t.Run("AuthorityEvent/unknownFields", func(t *testing.T) {
		badJSON := `{
			"version": 1,
			"id": "EVT-0001",
			"kind": "override",
			"date": "2026-09-08",
			"decidedBy": "user",
			"bogus": "reject me"
		}`
		var ev decisionview.AuthorityEvent
		err := json.Unmarshal([]byte(badJSON), &ev)
		if err == nil {
			t.Fatal("AuthorityEvent with unknown JSON fields must be rejected")
		}
	})

	// ---- AuthorityEvent: unknown event kind fails validation ----
	t.Run("AuthorityEvent/unknownKind", func(t *testing.T) {
		badEv := &decisionview.AuthorityEvent{
			Version:   decisionview.Version1,
			ID:        "EVT-BAD",
			Kind:      decisionview.EventKind("destroy"),
			Date:      "2026-09-08",
			DecidedBy: "user",
		}
		if err := badEv.Validate(); err == nil {
			t.Fatal("unknown event kind should be rejected")
		}
	})

	// ---- AuthorityEvent: statement with newlines and quotes survives JSON ----
	t.Run("AuthorityEvent/hostileStatement", func(t *testing.T) {
		target := decisionview.QualifiedID("ledger:01234567-89ab-cdef-0123-456789abcdef:D-0001")
		origStatement := "This is a test override with \"quotes\" and newlines\nthat must survive."
		ev := &decisionview.AuthorityEvent{
			Version:   decisionview.Version1,
			ID:        "EVT-0002",
			Kind:      decisionview.EventOverride,
			Date:      "2026-09-08",
			DecidedBy: "user",
			Target:    &target,
			Statement: origStatement,
		}
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		var ev2 decisionview.AuthorityEvent
		if err := json.Unmarshal(b, &ev2); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if ev2.Statement != origStatement {
			t.Fatalf("statement round trip:\n  got:  %q\n  want: %q", ev2.Statement, origStatement)
		}
	})

	// ---- Binding: unknown JSON fields must be rejected ----
	//
	// RED stage FAIL: Binding has no custom UnmarshalJSON.
	t.Run("Binding/unknownFields", func(t *testing.T) {
		badJSON := `{
			"version": 1,
			"ownerId": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			"collectionId": "01234567-89ab-cdef-0123-456789abcdef",
			"source": {"root": "planning", "path": "Decisions/decisions.md"},
			"extra": "unknown"
		}`
		var b decisionview.Binding
		err := json.Unmarshal([]byte(badJSON), &b)
		if err == nil {
			t.Fatal("Binding with unknown JSON fields must be rejected")
		}
	})

	// ---- QualifiedID round trip ----
	t.Run("QualifiedID", func(t *testing.T) {
		ledger, local, ok := decisionview.ParseQualifiedID(
			"ledger:01234567-89ab-cdef-0123-456789abcdef:D-0001")
		if !ok || string(ledger) != "01234567-89ab-cdef-0123-456789abcdef" || local != "D-0001" {
			t.Fatalf("parse qualified id: got (%q, %q, %v), want (0123..., D-0001, true)",
				string(ledger), local, ok)
		}
		// Bare ID must not parse as qualified.
		_, _, ok = decisionview.ParseQualifiedID("D-0001")
		if ok {
			t.Fatal("bare D-NNNN should not parse as qualified ID")
		}
		// Uppercase UUID must not parse.
		_, _, ok = decisionview.ParseQualifiedID(
			"ledger:01234567-89AB-CDEF-0123-456789ABCDEF:D-0001")
		if ok {
			t.Fatal("uppercase UUID should not parse as qualified ID")
		}
		// Extra segments.
		_, _, ok = decisionview.ParseQualifiedID("ledger:uuid:extra:D-0001")
		if ok {
			t.Fatal("extra segment should not parse as qualified ID")
		}
	})
}

// ---------------------------------------------------------------------------
// Unsupported versions rejection test  (gate: TestForkModelRejectsUnsupportedVersions)
// ---------------------------------------------------------------------------
//
// Every versioned model type must reject zero, negative, and unsupported
// versions (>1). Version 1 is accepted. This protects against schema drift
// (FR-17, DD-4).

func TestForkModelRejectsUnsupportedVersions(t *testing.T) {
	// ---- SchemaVersion JSON ----
	t.Run("SchemaVersion/json", func(t *testing.T) {
		tests := []struct {
			input string
			desc  string
		}{
			{`0`, "zero version"},
			{`-1`, "negative version"},
			{`2`, "unsupported v2"},
			{`999`, "unsupported v999"},
		}
		for _, tt := range tests {
			var sv decisionview.SchemaVersion
			err := sv.UnmarshalJSON([]byte(tt.input))
			if err == nil {
				t.Errorf("json %s (%s): expected error, got %v", tt.desc, tt.input, sv)
			}
		}
		// Version 1 must succeed.
		var v1 decisionview.SchemaVersion
		if err := v1.UnmarshalJSON([]byte(`1`)); err != nil {
			t.Fatalf("json version 1 should be accepted: %v", err)
		}
		if v1 != decisionview.Version1 {
			t.Fatalf("json version 1: got %v, want 1", v1)
		}
	})

	// ---- SchemaVersion YAML ----
	t.Run("SchemaVersion/yaml", func(t *testing.T) {
		tests := []struct {
			input string
			desc  string
		}{
			{"0", "zero version"},
			{"-1", "negative version"},
			{"2", "unsupported v2"},
			{"999", "unsupported v999"},
		}
		for _, tt := range tests {
			var yn yaml.Node
			if err := yaml.Unmarshal([]byte(tt.input), &yn); err != nil {
				t.Fatalf("yaml parse %s: %v", tt.desc, err)
			}
			var sv decisionview.SchemaVersion
			err := sv.UnmarshalYAML(&yn)
			if err == nil {
				t.Errorf("yaml %s (%s): expected error, got %v", tt.desc, tt.input, sv)
			}
		}
		// Version 1 must succeed via YAML.
		var yn yaml.Node
		if err := yaml.Unmarshal([]byte("1"), &yn); err != nil {
			t.Fatalf("yaml parse version 1: %v", err)
		}
		var v1 decisionview.SchemaVersion
		if err := v1.UnmarshalYAML(&yn); err != nil {
			t.Fatalf("yaml version 1 should be accepted: %v", err)
		}
		if v1 != decisionview.Version1 {
			t.Fatalf("yaml version 1: got %v, want 1", v1)
		}
	})

	// ---- SchemaVersion: non-integer values ----
	t.Run("SchemaVersion/non-integer", func(t *testing.T) {
		var sv decisionview.SchemaVersion
		err := sv.UnmarshalJSON([]byte(`"1"`))
		if err == nil {
			t.Fatal("json string version should be rejected (must be integer)")
		}

		var yn yaml.Node
		if err := yaml.Unmarshal([]byte(`"1"`), &yn); err != nil {
			t.Fatalf("yaml parse string: %v", err)
		}
		var sv2 decisionview.SchemaVersion
		err = sv2.UnmarshalYAML(&yn)
		if err == nil {
			t.Fatal("yaml string version should be rejected (must be integer)")
		}
	})

	// ---- ForkConfig rejects unsupported version via JSON decoder ----
	t.Run("ForkConfig", func(t *testing.T) {
		tests := []struct {
			json string
			desc string
		}{
			{
				`{"version": 0, "mode": "fork", "path": "x.md", "ledgerId": "01234567-89ab-cdef-0123-456789abcdef"}`,
				"zero version",
			},
			{
				`{"version": 2, "mode": "fork", "path": "x.md", "ledgerId": "01234567-89ab-cdef-0123-456789abcdef"}`,
				"unsupported v2",
			},
			{
				`{"version": 999, "mode": "fork", "path": "x.md", "ledgerId": "01234567-89ab-cdef-0123-456789abcdef"}`,
				"unsupported v999",
			},
		}
		for _, tt := range tests {
			fc, err := decisionview.DecodeForkConfig([]byte(tt.json))
			if err == nil {
				t.Errorf("fork config %s: expected error, decoded as version %v", tt.desc, fc.Version)
			}
		}
		// Version 1 must succeed.
		validJSON := `{
			"version": 1,
			"mode": "fork",
			"path": "Decisions/fork.md",
			"ledgerId": "01234567-89ab-cdef-0123-456789abcdef"
		}`
		fc, err := decisionview.DecodeForkConfig([]byte(validJSON))
		if err != nil {
			t.Fatalf("fork config version 1 should be accepted: %v", err)
		}
		if fc.Version != decisionview.Version1 {
			t.Fatalf("fork config version: got %v, want 1", fc.Version)
		}
	})

	// ---- Binding rejects unsupported version ----
	t.Run("Binding", func(t *testing.T) {
		templateBinding := &decisionview.Binding{
			Version:      decisionview.Version1,
			OwnerID:      "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			CollectionID: "01234567-89ab-cdef-0123-456789abcdef",
			Source: decisionview.SourceLocator{
				Root: decisionview.SourceRootPlanning,
				Path: "Decisions/decisions.md",
			},
		}
		if err := templateBinding.Validate(); err != nil {
			t.Fatalf("valid binding rejected: %v", err)
		}
		// Zero version.
		zero := *templateBinding
		zero.Version = decisionview.SchemaVersion(0)
		if err := zero.Validate(); err == nil {
			t.Fatal("binding with version 0 should be rejected")
		}
		// Version 2.
		v2 := *templateBinding
		v2.Version = decisionview.SchemaVersion(2)
		if err := v2.Validate(); err == nil {
			t.Fatal("binding with version 2 should be rejected")
		}
		// Version -1.
		neg := *templateBinding
		neg.Version = decisionview.SchemaVersion(-1)
		if err := neg.Validate(); err == nil {
			t.Fatal("binding with version -1 should be rejected")
		}
	})

	// ---- OverrideBasis rejects unsupported version ----
	t.Run("OverrideBasis", func(t *testing.T) {
		templateBasis := &decisionview.OverrideBasis{
			Version:  decisionview.Version1,
			TargetID: "ledger:01234567-89ab-cdef-0123-456789abcdef:D-0001",
		}
		if err := templateBasis.Validate(); err != nil {
			t.Fatalf("valid basis rejected: %v", err)
		}
		// Version 0.
		zero := *templateBasis
		zero.Version = 0
		if err := zero.Validate(); err == nil {
			t.Fatal("basis with version 0 should be rejected")
		}
		// Version 2.
		v2 := *templateBasis
		v2.Version = 2
		if err := v2.Validate(); err == nil {
			t.Fatal("basis with version 2 should be rejected")
		}
	})

	// ---- AuthorityEvent rejects unsupported version ----
	t.Run("AuthorityEvent", func(t *testing.T) {
		templateEvent := &decisionview.AuthorityEvent{
			Version:   decisionview.Version1,
			ID:        "EVT-0001",
			Kind:      decisionview.EventOverride,
			Date:      "2026-09-08",
			DecidedBy: "user",
		}
		if err := templateEvent.Validate(); err != nil {
			t.Fatalf("valid event rejected: %v", err)
		}
		// Version 0.
		zero := *templateEvent
		zero.Version = 0
		if err := zero.Validate(); err == nil {
			t.Fatal("event with version 0 should be rejected")
		}
		// Version 2.
		v2 := *templateEvent
		v2.Version = 2
		if err := v2.Validate(); err == nil {
			t.Fatal("event with version 2 should be rejected")
		}
	})
}

// ---------------------------------------------------------------------------
// Supporting tests that document known framework gaps for GREEN
// ---------------------------------------------------------------------------

// TestJSONDuplicateKeys verifies that Go's encoding/json silently accepts
// duplicate keys in struct decode (last wins). This documents the exact gap
// that the GREEN implementation must fill with custom JSON decoders for
// every typed model.
//
// Currently PASSES — Go stdlib behavior confirmation.
func TestJSONDuplicateKeys(t *testing.T) {
	input := `{"key": "first", "key": "second"}`
	var m map[string]string
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("json unmarshal with duplicate keys failed: %v", err)
	}
	t.Logf("json stdlib last-wins: got %q (GREEN must reject duplicate JSON keys)", m["key"])
}

// TestNewlinesInJSON confirms that newlines inside JSON string values survive
// marshal/unmarshal — a basic precondition for hostile round-trips.
func TestNewlinesInJSON(t *testing.T) {
	type testStruct struct {
		Value string `json:"value"`
	}
	original := testStruct{Value: "line1\nline2\nline3"}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal with newlines: %v", err)
	}
	var restored testStruct
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatalf("unmarshal with newlines: %v", err)
	}
	if restored.Value != original.Value {
		t.Fatalf("newlines round trip:\n  got:  %q\n  want: %q",
			restored.Value, original.Value)
	}
}

// TestDisallowUnknownFieldsOnStruct confirms that encoding/json's
// DisallowUnknownFields correctly rejects unknown keys but does NOT reject
// duplicate keys. Used by DecodeForkConfig.
func TestDisallowUnknownFieldsOnStruct(t *testing.T) {
	type empty struct{}
	input := `{"unknown":"value"}`
	var e empty
	dec := json.NewDecoder(strings.NewReader(input))
	dec.DisallowUnknownFields()
	err := dec.Decode(&e)
	if err == nil {
		t.Fatal("DisallowUnknownFields should reject unknown keys")
	}
	t.Logf("DisallowUnknownFields correctly rejects: %v", err)
}

func TestForkModelStrictShapes(t *testing.T) {
	const valid = `{"version":1,"mode":"fork","path":"Decisions/fork.md","ledgerId":"01234567-89ab-cdef-0123-456789abcdef"}`
	for _, input := range []string{
		`null`, `{}`, valid + `{}`,
		strings.Replace(valid, `"mode":"fork"`, `"mode":null`, 1),
		strings.Replace(valid, `"mode":"fork"`, `"mode":true`, 1),
		strings.Replace(valid, `"mode"`, `"Mode"`, 1),
		strings.Replace(valid, `"version":1,`, ``, 1),
		strings.Replace(valid, `"path":"Decisions/fork.md"`, `"path":"../fork.md"`, 1),
		strings.Replace(valid, `"path":"Decisions/fork.md"`, `"path":"/fork.md"`, 1),
		strings.Replace(valid, `"path":"Decisions/fork.md"`, `"path":"C:/fork.md"`, 1),
		strings.Replace(valid, `"path":"Decisions/fork.md"`, `"path":"Decisions\\fork.md"`, 1),
		strings.Replace(valid, `"ledgerId"`, `"transaction":{"id":"tx","journal":"Decisions/tx.json","extra":true},"ledgerId"`, 1),
	} {
		if _, err := decisionview.DecodeForkConfig([]byte(input)); err == nil {
			t.Errorf("accepted invalid config: %s", input)
		}
	}
	if _, err := decisionview.DecodeForkConfig([]byte(strings.Replace(valid, `"fork"`, `"detached"`, 1))); err != nil {
		t.Fatalf("detached config: %v", err)
	}
	badUTF8 := []byte(strings.Replace(valid, "Decisions/fork.md", "Decisions/\xff.md", 1))
	if _, err := decisionview.DecodeForkConfig(badUTF8); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
	for _, id := range []string{"", "D-1", "D-0001:extra", "d-0001", "D--001"} {
		if _, _, ok := decisionview.ParseQualifiedID("ledger:01234567-89ab-cdef-0123-456789abcdef:" + id); ok {
			t.Errorf("accepted local identifier %q", id)
		}
	}
	for _, input := range []string{
		"version: 1\nmode: fork\npath: a.md\npath: b.md\nledgerId: 01234567-89ab-cdef-0123-456789abcdef\n",
		"version: 1\nmode: true\npath: a.md\nledgerId: 01234567-89ab-cdef-0123-456789abcdef\n",
		"version: 1\nmode: fork\npath: a.md\nledgerId: 01234567-89ab-cdef-0123-456789abcdef\n---\nextra: document\n",
		"version: 1\nmode: fork\npath: a.md\nledgerId: 01234567-89ab-cdef-0123-456789abcdef\nunknown: true\n",
		"version: 1\nmode: fork\npath: a.md\nledgerId: 01234567-89ab-cdef-0123-456789abcdef\nunknown: &cycle [*cycle]\n",
	} {
		if _, err := decisionview.DecodeForkConfigYAML([]byte(input)); err == nil {
			t.Errorf("accepted invalid YAML: %s", input)
		}
	}
}

func TestForkMetadataHostileYAMLRoundTrip(t *testing.T) {
	metadata := decisionview.ForkMetadata{
		Version:      decisionview.Version1,
		LedgerID:     "01234567-89ab-cdef-0123-456789abcdef",
		RepositoryID: "fedcba98-7654-3210-fedc-ba9876543210",
		Events: []decisionview.AuthorityEvent{{
			Version: decisionview.Version1, ID: "event-1", Kind: decisionview.EventOverride,
			Date: "2026-09-08", DecidedBy: "user-approved",
			Statement: "yes no true false\n\"quotes\" 雪", Rationale: "preserve values",
			Basis: &decisionview.OverrideBasis{
				Version: decisionview.Version1, TargetID: "ledger:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee:D-0042",
				CanonicalContent: json.RawMessage(`{"statement":"yes\n\"no\"","tags":["true","false"]}`),
			},
		}},
	}
	raw, err := yaml.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decisionview.DecodeForkMetadataYAML(raw)
	if err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	if got.Events[0].Statement != metadata.Events[0].Statement {
		t.Fatalf("statement changed: %q", got.Events[0].Statement)
	}
	var content map[string]any
	if err := json.Unmarshal(got.Events[0].Basis.CanonicalContent, &content); err != nil {
		t.Fatal(err)
	}
	if content["statement"] != "yes\n\"no\"" {
		t.Fatalf("canonical content changed: %#v", content)
	}
	if _, err := decisionview.DecodeForkMetadataYAML(append(raw, []byte("unknown: field\n")...)); err == nil {
		t.Fatal("accepted unknown metadata field")
	}
}
