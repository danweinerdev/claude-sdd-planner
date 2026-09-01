// Package model defines the wire shapes of the plan graph and its proposal
// payloads: the committed master graph (`Plans/<Plan>/<Plan>-Graph.json`) and
// the JSON payloads LLM sessions author graphs from.
//
// Two rules govern everything here (Designs/SddGraph DD-3, DD-12):
//
//   - Only structure and observations persist. There is no stored state
//     field anywhere in these shapes — BLOCKED/READY/RED/GREEN/STALE are
//     derived on read by the states package, never serialized. Adding a
//     state or cache field to these structs is a design violation, not an
//     optimization.
//
//   - Decoding is strict. Unknown keys are errors carrying a JSON path and
//     a did-you-mean suggestion; malformed values are errors naming the
//     valid forms; all decode errors are accumulated and reported together,
//     never one at a time. A silently dropped payload key becomes a wrong
//     graph that validates — the exact failure strict decoding exists to
//     prevent.
//
// Proposal payloads are the same node shape minus the tool-owned fields
// (`intent_hashes`, `claim`, `verification`, `red_seqs`): the tool computes
// those, so a payload carrying one is refused rather than honored — the same
// posture the artifact compiler takes for tool-owned frontmatter (FR-18).
package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// SchemaVersion is the graph and payload schema version this package
// understands. There is no older version and no migration path yet; a
// mismatch is reported as unsupported, and the error deliberately does not
// name a migrate verb, because none exists.
const SchemaVersion = 1

// UntriagedSentinel is the on-disk spelling of "nobody has triaged this
// node's hazards". It is a string rather than null or [] because it must be
// unmistakable in a diff: an empty list is a *claim* that the node carries
// no failure classes, and asserting that on the operator's behalf would
// launder a falsehood into the graph (Designs/SddGraph DD-15 rationale).
const UntriagedSentinel = "untriaged"

// Gate types. `tests` is the default and strongly preferred; `command` and
// `review` exist because not all truth is test-shaped (DD-9). The vocabulary
// is closed — there is no plugin surface.
//
// GateUnspecified is not a gate: it is conversion's blocking sentinel
// (DD-15) — "nobody has specified how this node is verified". The model
// represents it so a converted graph can be stored and diffed, and compile
// refuses it with a per-node finding. The authoring schema deliberately does
// not advertise it: authors specify gates, only `sdd graph convert` emits
// the sentinel.
const (
	GateTests       = "tests"
	GateCommand     = "command"
	GateReview      = "review"
	GateUnspecified = "unspecified"
)

// NeedsContractPrefix marks a converted node whose v1 task title could not
// be reduced to a falsifiable contract mechanically — which is every one of
// them: titles name work, contracts state truths, and deriving one from the
// other is a judgment (DD-15: the tool never asserts on the operator's
// behalf). Compile refuses any contract carrying the prefix.
const NeedsContractPrefix = "NEEDS-CONTRACT: "

// Verification results and isolation levels (DD-5, DD-7).
const (
	ResultPass = "pass"
	ResultFail = "fail"

	IsolationClean       = "clean"
	IsolationSharedDirty = "shared-dirty"
	IsolationAsserted    = "asserted"
)

// Graph is the committed master graph: structure plus observations, nothing
// derivable.
type Graph struct {
	Version    int    `json:"version"`
	SeqCounter int    `json:"seq_counter"`
	Nodes      []Node `json:"nodes"`
	// Retired is the append-only register of node ids that once existed and
	// were retired (split, cut). A retired id is never reused — the same
	// stable-identifier discipline the markdown artifacts carry — so
	// existing references (history annotations, review followups, human
	// memory) cannot silently re-bind. Tool-owned: proposals carrying it
	// are refused by the strict decoder's unknown-key rule.
	Retired []string `json:"retired,omitempty"`
}

// Node is one unit of work: a falsifiable contract, its dependencies, the
// gate that verifies it, and (tool-owned) the observations recorded against
// it. GREEN is assumed closure; completion-grade closure is derived, never
// stored (D-0022).
type Node struct {
	ID        string   `json:"id"`
	Contract  string   `json:"contract"`
	Justifies []string `json:"justifies,omitempty"`
	// IntentHashes is tool-owned: SHA-256 over the normalized text of each
	// cited requirement, embedded by compile and rechecked on read
	// (INTENT-STALE, DD-4). Rejected in proposal payloads.
	IntentHashes map[string]string `json:"intent_hashes,omitempty"`
	Deps         []string          `json:"deps,omitempty"`
	Gate         Gate              `json:"gate"`
	// Hazards is nil when untriaged (serialized as the string sentinel) and
	// an empty non-nil slice when the payload explicitly claims "no failure
	// classes". The two are different claims and never conflated.
	Hazards   Hazards  `json:"hazards"`
	Artifacts []string `json:"artifacts,omitempty"`
	// Estimate is a unitless positive-integer relative cost weight
	// (default 1) consumed only by critical-path analytics and `next`
	// ordering. It is not a time promise.
	Estimate int    `json:"estimate"`
	Phase    string `json:"phase,omitempty"`
	// History is an authored annotation carried for the human reader —
	// conversion writes the v1 task's completion record here ("complete in
	// v1; revision …") so finished work keeps its provenance visible. It is
	// NEVER machine-consumed: states derive from observations alone, and a
	// history line grants no GREEN (DD-15: no retroactive observations).
	History string `json:"history,omitempty"`
	// Claim is tool-owned transient bookkeeping (DD-10): cleared on merge or
	// lease expiry, the only mutable non-observation field.
	Claim *Claim `json:"claim,omitempty"`
	// Verification is the latest observation (DD-5): never a status, always
	// provenance for what a report actually said.
	Verification *Verification `json:"verification,omitempty"`
	// RedSeqs is tool-owned: per declared test id, the seq of its first
	// observed failure. The merge gate's red-before-green check reads it —
	// a test never seen to fail proves nothing about the code that makes it
	// pass (DD-5).
	RedSeqs map[string]int `json:"red_seqs,omitempty"`
}

// Hazards is a node's triaged failure-class list. nil = untriaged
// (serialized as UntriagedSentinel); empty = explicit no-hazards claim.
type Hazards []string

// MarshalJSON writes the untriaged sentinel for nil and a plain list
// otherwise.
func (h Hazards) MarshalJSON() ([]byte, error) {
	if h == nil {
		return json.Marshal(UntriagedSentinel)
	}
	return json.Marshal([]string(h))
}

// Gate is how a node's contract is verified. Exactly one of the type-specific
// fields is meaningful, keyed by Type (DD-9).
type Gate struct {
	Type string `json:"type"`
	// Tests names the runner-reported test ids a `tests` gate is satisfied
	// by.
	Tests []Test `json:"tests,omitempty"`
	// Command is the check command whose exit code is a `command` gate's
	// observation.
	Command string `json:"command,omitempty"`
	// Lanes is a `review` gate's lane set: nil means the full four-lane
	// review (serialized as "full"); a non-empty list names a subset. Only
	// full gates carry completion-grade closure (DD-9).
	Lanes Lanes `json:"lanes,omitempty"`
}

// MarshalJSON renders the gate with only its type-relevant fields. It exists
// because a review gate's full-lane selection is a nil slice in memory — a
// struct-tag `omitempty` would silently drop `"lanes": "full"` from review
// gates, and lane selection must be explicit in the committed graph.
func (g Gate) MarshalJSON() ([]byte, error) {
	type wireTest = Test
	out := struct {
		Type    string     `json:"type"`
		Tests   []wireTest `json:"tests,omitempty"`
		Command string     `json:"command,omitempty"`
		Lanes   *Lanes     `json:"lanes,omitempty"`
	}{Type: g.Type, Tests: g.Tests, Command: g.Command}
	if g.Type == GateReview || g.Lanes != nil {
		out.Lanes = &g.Lanes
	}
	return json.Marshal(out)
}

// Lanes is a review gate's lane selection. nil = full.
type Lanes []string

// MarshalJSON writes "full" for nil and a plain list otherwise.
func (l Lanes) MarshalJSON() ([]byte, error) {
	if l == nil {
		return json.Marshal("full")
	}
	return json.Marshal([]string(l))
}

// Test is one runner-visible test a gate names, optionally discharging a
// declared hazard via Satisfies (checked by compile against the hazard
// vocabulary's required shapes).
type Test struct {
	ID        string   `json:"id"`
	File      string   `json:"file"`
	Satisfies []string `json:"satisfies,omitempty"`
}

// Claim records who is working a node and until when. Double-claim
// prevention lives in the store lock, not in By's uniqueness (DD-10).
type Claim struct {
	By           string `json:"by"`
	LeaseExpires string `json:"lease_expires"`
	Workspace    string `json:"workspace,omitempty"`
}

// Verification is one recorded observation: what a parsed report said, with
// enough provenance to detect drift. It is the only path to GREEN (DD-5) and
// anchors to content digests plus seq, with VCS revisions as supplementary
// provenance (DD-6).
type Verification struct {
	Result          string            `json:"result"`
	Seq             int               `json:"seq"`
	ArtifactDigests map[string]string `json:"artifact_digests,omitempty"`
	ReportDigest    string            `json:"report_digest,omitempty"`
	Isolation       string            `json:"isolation"`
	Provenance      *Provenance       `json:"provenance,omitempty"`
}

// Provenance is whatever the VCS natively produces: a git commit and
// worktree, a p4 changelist and opened files, or nothing for plain trees.
// Supplementary by design — the digest anchor is load-bearing (DD-6).
type Provenance struct {
	Kind        string   `json:"kind"`
	Revision    string   `json:"revision,omitempty"`
	Worktree    string   `json:"worktree,omitempty"`
	Changelist  string   `json:"changelist,omitempty"`
	OpenedFiles []string `json:"opened_files,omitempty"`
}

// Proposal is the payload shape an authoring session submits: the same node
// shape as the graph, minus tool-owned fields, which DecodeProposal rejects.
type Proposal struct {
	Version int    `json:"version"`
	Nodes   []Node `json:"nodes"`
}

// Encode serializes a graph deterministically: struct-order keys, sorted map
// keys (encoding/json's map behavior), two-space indent, LF line endings,
// trailing newline. The graph is committed and diffed by humans, so byte
// stability is part of the contract (DD-3).
func (g *Graph) Encode() ([]byte, error) {
	// A nil Nodes slice would marshal as `"nodes": null`, which the strict
	// decoder rejects — an empty graph is `[]`, never null. Normalize on a
	// shallow copy so encoding is never the thing that mutates a graph.
	out := *g
	if out.Nodes == nil {
		out.Nodes = []Node{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&out); err != nil {
		return nil, fmt.Errorf("encode graph: %w", err)
	}
	return buf.Bytes(), nil
}

// NodeIDs returns the graph's node ids in declaration order.
func (g *Graph) NodeIDs() []string {
	ids := make([]string, 0, len(g.Nodes))
	for i := range g.Nodes {
		ids = append(ids, g.Nodes[i].ID)
	}
	return ids
}

// NodeByID returns the node with the given id, or nil.
func (g *Graph) NodeByID(id string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

// sortedKeys returns a map's keys in sorted order — small helper shared by
// deterministic walks in this package and its tests.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
