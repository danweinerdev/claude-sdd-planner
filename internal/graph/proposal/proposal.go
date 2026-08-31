// Package proposal owns the graph-proposal authoring surface: the JSON
// Schema a payload validates against and the placeholder-complete exemplar
// an authoring session fills in (Designs/SddGraph DD-12).
//
// The exemplar is constructed as a TYPED model.Proposal and rendered, so it
// can never drift from the decoder: if the model changes shape, this package
// stops compiling or its round-trip test fails. The schema is an embedded
// document cross-checked against model.KeySets() and the model's constants
// by this package's tests — one source, two outputs, drift caught in CI
// exactly like the markdown template gate.
//
// Models replicate exemplars far more reliably than they satisfy
// specifications, so the exemplar favors demonstrative values: every gate
// type appears once, hazards appear filled, explicitly empty, and untriaged,
// and the terminal full review gate that satisfies compile's coverage
// invariant is present depending on every sink.
package proposal

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

//go:embed schema.json
var schemaJSON []byte

// SchemaJSON returns the graph-proposal JSON Schema document.
func SchemaJSON() []byte {
	return append([]byte(nil), schemaJSON...)
}

// Exemplar returns the placeholder-complete proposal an authoring session
// starts from. Placeholder conventions: requirement ids use the NN form
// (`AC-NN`, `FR-NN`, `DD-N`) and paths use a `.ext` suffix — both are meant
// to be replaced wholesale, never shipped.
func Exemplar() *model.Proposal {
	return &model.Proposal{
		Version: model.SchemaVersion,
		Nodes: []model.Node{
			{
				ID:        "define-schema",
				Contract:  "the schema describes every supported key and rejects an unknown one",
				Justifies: []string{"FR-NN"},
				Gate: model.Gate{
					Type: model.GateTests,
					Tests: []model.Test{
						{ID: "test_schema_covers_every_key", File: "tests/test_schema.ext"},
					},
				},
				// An explicit empty list is a claim: this node was triaged
				// and carries no failure class from the closed vocabulary.
				Hazards:   model.Hazards{},
				Artifacts: []string{"src/schema.ext"},
				Estimate:  1,
				Phase:     "01-example",
			},
			{
				ID:        "parse-config",
				Contract:  "loading a config accepts every documented key and refuses an unknown key by name",
				Justifies: []string{"AC-NN", "DD-N"},
				Deps:      []string{"define-schema"},
				Gate: model.Gate{
					Type: model.GateTests,
					Tests: []model.Test{
						{ID: "test_loads_valid_config", File: "tests/test_config.ext"},
						{
							ID:   "test_reparses_hostile_values",
							File: "tests/test_config.ext",
							// This test discharges the declared hazard, so
							// its shape must match the hazard's required
							// form and it must be seen failing before its
							// pass counts (red before green).
							Satisfies: []string{"external-format"},
						},
					},
				},
				Hazards:   model.Hazards{"external-format"},
				Artifacts: []string{"src/config.ext"},
				Estimate:  2,
				Phase:     "01-example",
			},
			{
				ID: "build-gate",
				Contract: "the tree builds clean; REPLACE the untriaged sentinel below with a " +
					"triaged hazard list or an explicit empty list before compiling",
				Justifies: []string{"FR-NN"},
				Gate: model.Gate{
					Type:    model.GateCommand,
					Command: "make build",
				},
				// nil Hazards renders as the "untriaged" sentinel: the
				// unmade-judgment marker that blocks compile until an
				// operator resolves it.
				Hazards:  nil,
				Estimate: 1,
				Phase:    "01-example",
			},
			{
				ID:        "feature-review",
				Contract:  "the example feature survives a full validation cycle",
				Justifies: []string{"AC-NN"},
				Deps:      []string{"parse-config", "build-gate"},
				Gate: model.Gate{
					Type: model.GateReview,
					// nil Lanes renders as "full": the only lane set that
					// carries completion-grade closure. This terminal gate
					// depends on every sink node so compile's coverage
					// invariant is satisfied by the default payload;
					// removing it is a visible, deliberate act.
					Lanes: nil,
				},
				Hazards:  model.Hazards{},
				Estimate: 1,
				Phase:    "01-example",
			},
		},
	}
}

// ExemplarJSON renders the exemplar deterministically: two-space indent, LF,
// trailing newline — the same conventions as the committed graph.
func ExemplarJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(Exemplar()); err != nil {
		return nil, fmt.Errorf("encode graph-proposal exemplar: %w", err)
	}
	return buf.Bytes(), nil
}
