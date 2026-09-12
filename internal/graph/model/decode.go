package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// DecodeError is one strict-decoding finding: a JSON path to the offending
// value and a message naming what is wrong and, where applicable, what would
// be right.
type DecodeError struct {
	Path string
	Msg  string
}

func (e DecodeError) Error() string {
	if e.Path == "" {
		return e.Msg
	}
	return e.Path + ": " + e.Msg
}

// DecodeErrors is every finding from one decode pass. Strict decoding never
// stops at the first problem: the repair loop is an edit to the payload
// file, and it should need exactly one round trip (DD-11, DD-12).
type DecodeErrors []DecodeError

func (es DecodeErrors) Error() string {
	msgs := make([]string, len(es))
	for i, e := range es {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// decoder accumulates findings while walking the raw JSON value tree.
type decoder struct {
	errs DecodeErrors
	// proposal rejects tool-owned fields rather than decoding them.
	proposal bool
}

func (d *decoder) errf(path, format string, args ...any) {
	d.errs = append(d.errs, DecodeError{Path: path, Msg: fmt.Sprintf(format, args...)})
}

// toolOwnedNodeKeys are computed by the tool and refused in proposals — the
// same posture the artifact compiler takes for tool-owned frontmatter: a
// payload asserting one is refused loudly, never silently discarded.
var toolOwnedNodeKeys = map[string]string{
	"intent_hashes": "compile embeds requirement hashes",
	"input_hashes":  "compile embeds input fingerprints",
	"claim":         "`next --claim` records claims under the store lock",
	"verification":  "`graph sync` records observations from parsed reports",
	"red_seqs":      "`graph sync` records first-failure seqs from parsed reports",
	"contract_rev":  "`graph amend` advances contract revisions",
	"origin":        "`graph amend` records which finding created a node",
}

// Allowed key sets per object, for unknown-key detection and did-you-mean.
var (
	graphKeys        = []string{"version", "seq_counter", "revision_lineage", "nodes", "retired", "retirement_sources", "amendments", "acknowledgements"}
	proposalKeys     = []string{"version", "nodes"}
	nodeKeys         = []string{"id", "role", "contract", "contract_rev", "origin", "justifies", "intent_hashes", "inputs", "input_hashes", "deps", "gate", "hazards", "artifacts", "estimate", "phase", "history", "claim", "verification", "red_seqs"}
	originKeys       = []string{"review", "finding"}
	amendmentKeys    = []string{"seq", "review", "artifact", "report_digest", "revised", "extended"}
	reviewedKeys     = []string{"contract_rev", "artifact_digests"}
	gateKeys         = []string{"type", "tests", "command", "lanes"}
	testKeys         = []string{"id", "file", "satisfies"}
	inputKeys        = []string{"root", "path", "section"}
	inputSectionKeys = []string{"heading_path"}
	claimKeys        = []string{"by", "lease_expires", "workspace"}
	verificationKeys = []string{"result", "seq", "contract_rev", "artifact_digests", "reviewed", "dependency_digests", "input_hashes", "intent_hashes", "report_digest", "isolation", "provenance"}
	ackKeys          = []string{"seq", "node", "kind", "key", "old", "new", "by"}
	provenanceKeys   = []string{"kind", "revision", "worktree", "changelist", "opened_files"}
)

// KeySets exposes the allowed-key vocabulary of the PROPOSAL payload shapes,
// keyed by object name. It exists for exactly one consumer: the
// graph-proposal JSON Schema's drift-gate test, which cross-checks the
// schema's `properties` against these lists so the two can never disagree
// silently (DD-12: skeleton and schema from one source). Node keys are the
// payload form — tool-owned fields excluded, since DecodeProposal rejects
// them. Returned slices are copies.
func KeySets() map[string][]string {
	proposalNodeKeys := make([]string, 0, len(nodeKeys))
	for _, k := range nodeKeys {
		if _, toolOwned := toolOwnedNodeKeys[k]; !toolOwned {
			proposalNodeKeys = append(proposalNodeKeys, k)
		}
	}
	cp := func(s []string) []string { return append([]string(nil), s...) }
	return map[string][]string{
		"proposal": cp(proposalKeys),
		"node":     proposalNodeKeys,
		"gate":     cp(gateKeys),
		"test":     cp(testKeys),
	}
}

// DecodeGraph strictly decodes a committed master graph.
func DecodeGraph(data []byte) (*Graph, error) {
	raw, err := parse(data)
	if err != nil {
		return nil, err
	}
	d := &decoder{}
	g := d.graph(raw)
	if len(d.errs) > 0 {
		return nil, d.errs
	}
	return g, nil
}

// DecodeProposal strictly decodes an authoring payload, refusing tool-owned
// fields.
func DecodeProposal(data []byte) (*Proposal, error) {
	raw, err := parse(data)
	if err != nil {
		return nil, err
	}
	d := &decoder{proposal: true}
	g := d.graph(raw)
	if len(d.errs) > 0 {
		return nil, d.errs
	}
	return &Proposal{Version: g.Version, Nodes: g.Nodes}, nil
}

// DecodeInputs strictly decodes a bare JSON array of input objects — the
// shape `graph set-inputs --file` accepts. The same strict posture as
// DecodeProposal: unknown keys, malformed values, and bad root selectors are
// errors carrying a JSON path.
func DecodeInputs(data []byte) ([]Input, error) {
	raw, err := parse(data)
	if err != nil {
		return nil, err
	}
	d := &decoder{}
	_, ok := raw.([]any)
	if !ok {
		return nil, DecodeErrors{{Msg: fmt.Sprintf("must be a JSON array of input objects, got %s", typeName(raw))}}
	}
	out := d.inputList("", raw)
	if len(d.errs) > 0 {
		return nil, d.errs
	}
	return out, nil
}

// parse turns raw bytes into the generic value tree, reporting JSON syntax
// errors with their byte offset. UseNumber keeps integers exact.
func parse(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		if syn, ok := err.(*json.SyntaxError); ok {
			return nil, DecodeErrors{{Msg: fmt.Sprintf("JSON syntax error at byte %d: %v", syn.Offset, syn.Error())}}
		}
		return nil, DecodeErrors{{Msg: "JSON syntax error: " + err.Error()}}
	}
	// Trailing garbage after the document is a malformed payload, not an
	// extension point.
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, DecodeErrors{{Msg: "trailing content after the JSON document"}}
	}
	return raw, nil
}

func (d *decoder) graph(raw any) *Graph {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf("", "document must be a JSON object, got %s", typeName(raw))
		return &Graph{}
	}
	allowed := graphKeys
	if d.proposal {
		allowed = proposalKeys
	}
	d.unknownKeys("", obj, allowed)

	g := &Graph{}
	switch v, present := obj["version"]; {
	case !present:
		d.errf("version", "missing required field")
	default:
		if n, ok := d.intVal("version", v); ok {
			if n != SchemaVersion {
				d.errf("version", "schema version %d is unsupported; this sdd supports version %d", n, SchemaVersion)
			}
			g.Version = n
		}
	}
	if v, present := obj["seq_counter"]; present && !d.proposal {
		if n, ok := d.intVal("seq_counter", v); ok {
			if n < 0 {
				d.errf("seq_counter", "must be >= 0, got %d", n)
			}
			g.SeqCounter = n
		}
	}
	if v, present := obj["revision_lineage"]; present && !d.proposal {
		g.RevisionLineage = d.revisionLineage(v)
	}
	if v, present := obj["retired"]; present && !d.proposal {
		g.Retired = d.stringList("retired", v)
	}
	if v, present := obj["retirement_sources"]; present && !d.proposal {
		g.RetirementSources = d.retirementSources(v)
	}
	if v, present := obj["acknowledgements"]; present && !d.proposal {
		list, ok := v.([]any)
		if !ok {
			d.errf("acknowledgements", "must be a list of acknowledgement records, got %s", typeName(v))
		} else {
			for i, item := range list {
				g.Acknowledgements = append(g.Acknowledgements, d.acknowledgement(fmt.Sprintf("acknowledgements[%d]", i), item))
			}
		}
	}
	if v, present := obj["amendments"]; present && !d.proposal {
		list, ok := v.([]any)
		if !ok {
			d.errf("amendments", "must be a list of amendment records, got %s", typeName(v))
		} else {
			for i, item := range list {
				g.Amendments = append(g.Amendments, d.amendment(fmt.Sprintf("amendments[%d]", i), item))
			}
		}
	}
	nodesRaw, present := obj["nodes"]
	if !present {
		d.errf("nodes", "missing required field")
		return g
	}
	list, ok := nodesRaw.([]any)
	if !ok {
		d.errf("nodes", "must be a list of node objects, got %s", typeName(nodesRaw))
		return g
	}
	for i, item := range list {
		g.Nodes = append(g.Nodes, d.node(fmt.Sprintf("nodes[%d]", i), item))
	}
	return g
}

var fullGitCommitID = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func (d *decoder) revisionLineage(raw any) map[string]string {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf("revision_lineage", "must be an object mapping old full Git commit IDs to new full Git commit IDs, got %s", typeName(raw))
		return nil
	}
	out := make(map[string]string, len(obj))
	normalized := make(map[string]string, len(obj))
	originalByNormalized := make(map[string]string, len(obj))
	for _, originalKey := range sortedKeys(obj) {
		path := "revision_lineage." + originalKey
		if !fullGitCommitID.MatchString(originalKey) {
			d.errf(path, "key must be a full 40-character Git commit ID")
			continue
		}
		value, ok := obj[originalKey].(string)
		if !ok {
			d.errf(path, "must be a string, got %s", typeName(obj[originalKey]))
			continue
		}
		if !fullGitCommitID.MatchString(value) {
			d.errf(path, "value must be a full 40-character Git commit ID")
			continue
		}
		oldRev, newRev := strings.ToLower(originalKey), strings.ToLower(value)
		if oldRev == newRev {
			d.errf(path, "self-mapping is not stored; unchanged revisions are an explicit remap no-op")
			continue
		}
		if _, exists := originalByNormalized[oldRev]; exists {
			d.errf(path, "commit identity conflicts with another spelling of the same source revision")
			continue
		}
		originalByNormalized[oldRev] = originalKey
		normalized[oldRev] = newRev
		out[originalKey] = value
	}

	// Rewrites are one-to-one. Fan-in would make the reverse identity
	// ambiguous and can silently conflate two independently observed commits.
	reverse := map[string]string{}
	for _, oldRev := range sortedKeys(normalized) {
		newRev := normalized[oldRev]
		if prior, exists := reverse[newRev]; exists && prior != oldRev {
			d.errf("revision_lineage."+originalByNormalized[oldRev], "fan-in is not allowed: %s and %s both map to %s", prior, oldRev, newRev)
			continue
		}
		reverse[newRev] = oldRev
	}

	// A lineage is a chain, never an alias cycle. Report each cycle once at
	// its lexicographically first member for deterministic diagnostics.
	reported := map[string]bool{}
	for _, start := range sortedKeys(normalized) {
		positions := map[string]int{}
		var walk []string
		cur := start
		for {
			if at, seen := positions[cur]; seen {
				cycle := append([]string(nil), walk[at:]...)
				sort.Strings(cycle)
				key := strings.Join(cycle, ",")
				if !reported[key] {
					reported[key] = true
					d.errf("revision_lineage."+originalByNormalized[cycle[0]], "cycle is not allowed in revision lineage: %s", strings.Join(cycle, " -> "))
				}
				break
			}
			next, exists := normalized[cur]
			if !exists {
				break
			}
			positions[cur] = len(walk)
			walk = append(walk, cur)
			cur = next
		}
	}
	return out
}

func (d *decoder) retirementSources(raw any) map[string]RetirementRecord {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf("retirement_sources", "must be an object keyed by retired id")
		return nil
	}
	out := map[string]RetirementRecord{}
	var keys []string
	for id := range obj {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		path := "retirement_sources." + id
		record, ok := obj[id].(map[string]any)
		if !ok {
			d.errf(path, "must be an object")
			continue
		}
		d.unknownKeys(path, record, []string{"source", "replaced_by"})
		source, ok := record["source"].(map[string]any)
		if !ok {
			d.errf(path+".source", "must be an object")
			continue
		}
		d.unknownKeys(path+".source", source, []string{"vcs", "revision", "path", "source_id"})
		s := RetirementSource{
			VCS:      d.requiredString(path+".source", source, "vcs"),
			Revision: d.requiredString(path+".source", source, "revision"),
			Path:     d.requiredString(path+".source", source, "path"),
			SourceID: d.requiredString(path+".source", source, "source_id"),
		}
		if s.VCS != "git" {
			d.errf(path+".source.vcs", "must be git")
		}
		out[id] = RetirementRecord{Source: s, ReplacedBy: d.stringList(path+".replaced_by", record["replaced_by"])}
	}
	return out
}

func (d *decoder) node(path string, raw any) Node {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf(path, "must be an object, got %s", typeName(raw))
		return Node{}
	}
	d.unknownKeys(path, obj, nodeKeys)
	if d.proposal {
		for key, why := range toolOwnedNodeKeys {
			if _, present := obj[key]; present {
				d.errf(path+"."+key, "tool-owned field rejected in payloads: %s", why)
			}
		}
	}

	n := Node{Estimate: 1}
	n.ID = d.requiredString(path, obj, "id")
	n.Role = d.optionalString(path+".role", obj["role"])
	if n.Role != "" && !KnownRole(n.Role) {
		d.errf(path+".role", "%q is not a role; valid roles are %q, %q, %q, %q", n.Role, RoleImplementation, RoleSharedMechanism, RoleReview, RoleIntegrationAcceptance)
	}
	n.Contract = d.requiredString(path, obj, "contract")
	n.Justifies = d.stringList(path+".justifies", obj["justifies"])
	n.Deps = d.stringList(path+".deps", obj["deps"])
	n.Artifacts = d.stringList(path+".artifacts", obj["artifacts"])
	n.Phase = d.optionalString(path+".phase", obj["phase"])
	n.History = d.optionalString(path+".history", obj["history"])

	if v, present := obj["inputs"]; present {
		n.Inputs = d.inputList(path+".inputs", v)
	}

	if v, present := obj["estimate"]; present {
		if e, ok := d.intVal(path+".estimate", v); ok {
			if e < 1 {
				d.errf(path+".estimate", "must be >= 1 (a unitless relative cost weight), got %d", e)
			} else {
				n.Estimate = e
			}
		}
	}

	switch v, present := obj["gate"]; {
	case !present:
		d.errf(path+".gate", "missing required field")
	default:
		n.Gate = d.gate(path+".gate", v)
	}

	switch v, present := obj["hazards"]; {
	case !present:
		d.errf(path+".hazards", "missing required field: a list of failure classes, an explicit empty list, or the string %q", UntriagedSentinel)
	default:
		n.Hazards = d.hazards(path+".hazards", v)
	}

	if !d.proposal {
		if v, present := obj["intent_hashes"]; present {
			n.IntentHashes = d.stringMap(path+".intent_hashes", v)
		}
		if v, present := obj["input_hashes"]; present {
			n.InputHashes = d.stringMap(path+".input_hashes", v)
		}
		if v, present := obj["claim"]; present {
			n.Claim = d.claim(path+".claim", v)
		}
		if v, present := obj["verification"]; present {
			n.Verification = d.verification(path+".verification", v)
		}
		if v, present := obj["red_seqs"]; present {
			n.RedSeqs = d.intMap(path+".red_seqs", v)
		}
		if v, present := obj["contract_rev"]; present {
			if r, ok := d.intVal(path+".contract_rev", v); ok {
				if r < 1 {
					d.errf(path+".contract_rev", "must be >= 1, got %d", r)
				}
				n.ContractRev = r
			}
		}
		if v, present := obj["origin"]; present {
			obj2, ok := v.(map[string]any)
			if !ok {
				d.errf(path+".origin", "must be an object, got %s", typeName(v))
			} else {
				d.unknownKeys(path+".origin", obj2, originKeys)
				n.Origin = &Origin{Review: d.requiredString(path+".origin", obj2, "review"), Finding: d.requiredString(path+".origin", obj2, "finding")}
			}
		}
	}
	return n
}

func (d *decoder) acknowledgement(path string, raw any) AcknowledgementRecord {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf(path, "must be an object, got %s", typeName(raw))
		return AcknowledgementRecord{}
	}
	d.unknownKeys(path, obj, ackKeys)
	a := AcknowledgementRecord{
		Node: d.requiredString(path, obj, "node"), Kind: d.requiredString(path, obj, "kind"),
		Key: d.requiredString(path, obj, "key"), New: d.requiredString(path, obj, "new"),
		Old: d.optionalString(path+".old", obj["old"]), By: d.optionalString(path+".by", obj["by"]),
	}
	if a.Kind != "citation" && a.Kind != "input" && a.Kind != "" {
		d.errf(path+".kind", "%q is not an acknowledgement kind; valid kinds are \"citation\" and \"input\"", a.Kind)
	}
	if v, present := obj["seq"]; present {
		a.Seq, _ = d.intVal(path+".seq", v)
	} else {
		d.errf(path+".seq", "missing required field")
	}
	return a
}

func (d *decoder) amendment(path string, raw any) AmendmentRecord {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf(path, "must be an object, got %s", typeName(raw))
		return AmendmentRecord{}
	}
	d.unknownKeys(path, obj, amendmentKeys)
	a := AmendmentRecord{
		Review: d.requiredString(path, obj, "review"), Artifact: d.requiredString(path, obj, "artifact"),
		ReportDigest: d.requiredString(path, obj, "report_digest"),
		Revised:      d.stringList(path+".revised", obj["revised"]), Extended: d.stringList(path+".extended", obj["extended"]),
	}
	if v, present := obj["seq"]; present {
		a.Seq, _ = d.intVal(path+".seq", v)
	} else {
		d.errf(path+".seq", "missing required field")
	}
	return a
}

func (d *decoder) gate(path string, raw any) Gate {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf(path, "must be an object, got %s", typeName(raw))
		return Gate{}
	}
	d.unknownKeys(path, obj, gateKeys)

	g := Gate{}
	g.Type = d.requiredString(path, obj, "type")
	switch g.Type {
	case GateTests, GateCommand, GateReview, "":
	case GateUnspecified:
		// Representable so converted graphs store and diff; compile refuses
		// it with a per-node finding (DD-15's sentinel-then-block).
	default:
		d.errf(path+".type", "%q is not a gate type; valid types are %q, %q, %q", g.Type, GateTests, GateCommand, GateReview)
	}
	if v, present := obj["tests"]; present {
		list, ok := v.([]any)
		if !ok {
			d.errf(path+".tests", "must be a list of test objects, got %s", typeName(v))
		} else {
			for i, item := range list {
				g.Tests = append(g.Tests, d.test(fmt.Sprintf("%s.tests[%d]", path, i), item))
			}
		}
	}
	g.Command = d.optionalString(path+".command", obj["command"])
	if v, present := obj["lanes"]; present {
		g.Lanes = d.lanes(path+".lanes", v)
	}
	return g
}

// lanes accepts the string "full" (nil in memory) or a non-empty list of
// lane names.
func (d *decoder) lanes(path string, raw any) Lanes {
	switch v := raw.(type) {
	case string:
		if v != "full" {
			d.errf(path, "%q is not a lane set; use \"full\" or a list of lane names", v)
		}
		return nil
	case []any:
		out := d.stringList(path, raw)
		if len(out) == 0 {
			d.errf(path, "an empty lane list selects nothing; use \"full\" for the full review")
		}
		return out
	default:
		d.errf(path, "must be \"full\" or a list of lane names, got %s", typeName(raw))
		return nil
	}
}

// hazards accepts the untriaged sentinel (nil in memory) or a list —
// including the explicit empty list, which is a claim, not a default.
func (d *decoder) hazards(path string, raw any) Hazards {
	switch v := raw.(type) {
	case string:
		if v != UntriagedSentinel {
			d.errf(path, "%q is not a hazards value; use a list of failure classes or the string %q", v, UntriagedSentinel)
		}
		return nil
	case []any:
		out := d.stringList(path, raw)
		if out == nil {
			out = []string{}
		}
		return out
	default:
		d.errf(path, "must be a list of failure classes or the string %q, got %s", UntriagedSentinel, typeName(raw))
		return nil
	}
}

func (d *decoder) test(path string, raw any) Test {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf(path, "must be an object, got %s", typeName(raw))
		return Test{}
	}
	d.unknownKeys(path, obj, testKeys)
	return Test{
		ID:        d.requiredString(path, obj, "id"),
		File:      d.requiredString(path, obj, "file"),
		Satisfies: d.stringList(path+".satisfies", obj["satisfies"]),
	}
}

// inputList decodes a node's declared read-only inputs.
func (d *decoder) inputList(path string, raw any) []Input {
	list, ok := raw.([]any)
	if !ok {
		d.errf(path, "must be a list of input objects, got %s", typeName(raw))
		return nil
	}
	out := make([]Input, 0, len(list))
	seen := map[string]bool{}
	for i, item := range list {
		in := d.input(fmt.Sprintf("%s[%d]", path, i), item)
		key := InputKey(in)
		if seen[key] {
			d.errf(fmt.Sprintf("%s[%d]", path, i), "duplicate input declaration")
		}
		seen[key] = true
		out = append(out, in)
	}
	return out
}

// input decodes one declared input. `root` must name one of the two explicit
// roots; `section`, when present, must carry a nonempty heading path.
func (d *decoder) input(path string, raw any) Input {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf(path, "must be an object, got %s", typeName(raw))
		return Input{}
	}
	d.unknownKeys(path, obj, inputKeys)

	in := Input{
		Root: d.requiredString(path, obj, "root"),
		Path: d.requiredString(path, obj, "path"),
	}
	switch in.Root {
	case InputRootRepository, InputRootPlanning:
	default:
		d.errf(path+".root", "%q is not an input root; valid roots are %q and %q", in.Root, InputRootRepository, InputRootPlanning)
	}
	if v, present := obj["section"]; present {
		in.Section = d.inputSection(path+".section", v)
	}
	return in
}

func (d *decoder) inputSection(path string, raw any) *InputSection {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf(path, "must be an object with a heading_path list, got %s", typeName(raw))
		return nil
	}
	d.unknownKeys(path, obj, inputSectionKeys)
	sec := &InputSection{}
	if v, present := obj["heading_path"]; present {
		list, ok := v.([]any)
		if !ok {
			d.errf(path+".heading_path", "must be a nonempty list of heading titles, got %s", typeName(v))
			return sec
		}
		if len(list) == 0 {
			d.errf(path+".heading_path", "must be a nonempty list of heading titles")
			return sec
		}
		for i, item := range list {
			s, ok := item.(string)
			if !ok {
				d.errf(fmt.Sprintf("%s.heading_path[%d]", path, i), "must be a string, got %s", typeName(item))
				continue
			}
			if strings.TrimSpace(s) == "" {
				d.errf(fmt.Sprintf("%s.heading_path[%d]", path, i), "must be a non-empty string")
				continue
			}
			sec.HeadingPath = append(sec.HeadingPath, s)
		}
	} else {
		d.errf(path, "missing required field heading_path")
	}
	return sec
}

func (d *decoder) claim(path string, raw any) *Claim {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf(path, "must be an object, got %s", typeName(raw))
		return nil
	}
	d.unknownKeys(path, obj, claimKeys)
	return &Claim{
		By:           d.requiredString(path, obj, "by"),
		LeaseExpires: d.requiredString(path, obj, "lease_expires"),
		Workspace:    d.optionalString(path+".workspace", obj["workspace"]),
	}
}

func (d *decoder) verification(path string, raw any) *Verification {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf(path, "must be an object, got %s", typeName(raw))
		return nil
	}
	d.unknownKeys(path, obj, verificationKeys)

	v := &Verification{}
	v.Result = d.requiredString(path, obj, "result")
	if v.Result != "" && v.Result != ResultPass && v.Result != ResultFail {
		d.errf(path+".result", "%q is not a result; valid results are %q and %q", v.Result, ResultPass, ResultFail)
	}
	switch sv, present := obj["seq"]; {
	case !present:
		d.errf(path+".seq", "missing required field")
	default:
		if n, ok := d.intVal(path+".seq", sv); ok {
			if n < 0 {
				d.errf(path+".seq", "must be >= 0, got %d", n)
			}
			v.Seq = n
		}
	}
	if av, present := obj["artifact_digests"]; present {
		v.ArtifactDigests = d.stringMap(path+".artifact_digests", av)
	}
	if rv, present := obj["contract_rev"]; present {
		if n, ok := d.intVal(path+".contract_rev", rv); ok {
			if n < 1 {
				d.errf(path+".contract_rev", "must be >= 1, got %d", n)
			}
			v.ContractRev = n
		}
	}
	if rv, present := obj["reviewed"]; present {
		obj2, ok := rv.(map[string]any)
		if !ok {
			d.errf(path+".reviewed", "must be an object keyed by node id, got %s", typeName(rv))
		} else {
			v.Reviewed = map[string]ReviewedRef{}
			var ids []string
			for id := range obj2 {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				p := path + ".reviewed." + id
				ref, ok := obj2[id].(map[string]any)
				if !ok {
					d.errf(p, "must be an object")
					continue
				}
				d.unknownKeys(p, ref, reviewedKeys)
				r := ReviewedRef{}
				if cv, present := ref["contract_rev"]; present {
					r.ContractRev, _ = d.intVal(p+".contract_rev", cv)
				} else {
					d.errf(p+".contract_rev", "missing required field")
				}
				if av, present := ref["artifact_digests"]; present {
					r.ArtifactDigests = d.stringMap(p+".artifact_digests", av)
				}
				v.Reviewed[id] = r
			}
		}
	}
	if dv, present := obj["dependency_digests"]; present {
		obj2, ok := dv.(map[string]any)
		if !ok {
			d.errf(path+".dependency_digests", "must be an object keyed by dependency id, got %s", typeName(dv))
		} else {
			v.DependencyDigests = map[string]map[string]string{}
			var ids []string
			for id := range obj2 {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				v.DependencyDigests[id] = d.stringMap(path+".dependency_digests."+id, obj2[id])
			}
		}
	}
	if iv, present := obj["input_hashes"]; present {
		v.InputHashes = d.stringMap(path+".input_hashes", iv)
	}
	if iv, present := obj["intent_hashes"]; present {
		v.IntentHashes = d.stringMap(path+".intent_hashes", iv)
	}
	v.ReportDigest = d.optionalString(path+".report_digest", obj["report_digest"])
	v.Isolation = d.requiredString(path, obj, "isolation")
	switch v.Isolation {
	case IsolationClean, IsolationSharedDirty, IsolationAsserted, "":
	default:
		d.errf(path+".isolation", "%q is not an isolation level; valid levels are %q, %q, %q",
			v.Isolation, IsolationClean, IsolationSharedDirty, IsolationAsserted)
	}
	if pv, present := obj["provenance"]; present {
		v.Provenance = d.provenance(path+".provenance", pv)
	}
	return v
}

func (d *decoder) provenance(path string, raw any) *Provenance {
	obj, ok := raw.(map[string]any)
	if !ok {
		d.errf(path, "must be an object, got %s", typeName(raw))
		return nil
	}
	d.unknownKeys(path, obj, provenanceKeys)
	return &Provenance{
		Kind:        d.requiredString(path, obj, "kind"),
		Revision:    d.optionalString(path+".revision", obj["revision"]),
		Worktree:    d.optionalString(path+".worktree", obj["worktree"]),
		Changelist:  d.optionalString(path+".changelist", obj["changelist"]),
		OpenedFiles: d.stringList(path+".opened_files", obj["opened_files"]),
	}
}

// --- primitive helpers ---

func (d *decoder) requiredString(objPath string, obj map[string]any, key string) string {
	path := key
	if objPath != "" {
		path = objPath + "." + key
	}
	v, present := obj[key]
	if !present {
		d.errf(path, "missing required field")
		return ""
	}
	s, ok := v.(string)
	if !ok {
		d.errf(path, "must be a string, got %s", typeName(v))
		return ""
	}
	if strings.TrimSpace(s) == "" {
		d.errf(path, "must be a non-empty string")
		return ""
	}
	return s
}

func (d *decoder) optionalString(path string, v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		d.errf(path, "must be a string, got %s", typeName(v))
		return ""
	}
	return s
}

func (d *decoder) stringList(path string, v any) []string {
	if v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		d.errf(path, "must be a list of strings, got %s", typeName(v))
		return nil
	}
	var out []string
	for i, item := range list {
		s, ok := item.(string)
		if !ok {
			d.errf(fmt.Sprintf("%s[%d]", path, i), "must be a string, got %s", typeName(item))
			continue
		}
		if strings.TrimSpace(s) == "" {
			d.errf(fmt.Sprintf("%s[%d]", path, i), "must be a non-empty string")
			continue
		}
		out = append(out, s)
	}
	return out
}

func (d *decoder) stringMap(path string, v any) map[string]string {
	obj, ok := v.(map[string]any)
	if !ok {
		d.errf(path, "must be an object of string values, got %s", typeName(v))
		return nil
	}
	out := make(map[string]string, len(obj))
	for _, k := range sortedKeys(obj) {
		s, ok := obj[k].(string)
		if !ok {
			d.errf(path+"."+k, "must be a string, got %s", typeName(obj[k]))
			continue
		}
		out[k] = s
	}
	return out
}

func (d *decoder) intMap(path string, v any) map[string]int {
	obj, ok := v.(map[string]any)
	if !ok {
		d.errf(path, "must be an object of integer values, got %s", typeName(v))
		return nil
	}
	out := make(map[string]int, len(obj))
	for _, k := range sortedKeys(obj) {
		if n, ok := d.intVal(path+"."+k, obj[k]); ok {
			if n < 0 {
				d.errf(path+"."+k, "must be >= 0, got %d", n)
				continue
			}
			out[k] = n
		}
	}
	return out
}

// intVal accepts exact JSON integers only: 2.0, "2", and 1e1 are all
// rejected, because a graph is committed state, not a lenient config file.
func (d *decoder) intVal(path string, v any) (int, bool) {
	num, ok := v.(json.Number)
	if !ok {
		d.errf(path, "must be an integer, got %s", typeName(v))
		return 0, false
	}
	i, err := num.Int64()
	if err != nil {
		d.errf(path, "must be an integer, got %s", num.String())
		return 0, false
	}
	return int(i), true
}

// unknownKeys reports every key not in allowed, with a did-you-mean when a
// known key is within edit distance 3. The error quality is the contract:
// the repair loop is one file edit, so the finding must name the fix.
func (d *decoder) unknownKeys(path string, obj map[string]any, allowed []string) {
	allowedSet := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		allowedSet[k] = true
	}
	for _, k := range sortedKeys(obj) {
		if allowedSet[k] {
			continue
		}
		p := path
		if p == "" {
			p = "(document)"
		}
		if near := nearestKey(k, allowed); near != "" {
			d.errf(p, "unknown key %q — did you mean %q?", k, near)
		} else {
			d.errf(p, "unknown key %q", k)
		}
	}
}

// nearestKey returns the allowed key closest to k by Levenshtein distance,
// when that distance is <= 3; ties break toward the earlier key in the
// allowed list so suggestions are deterministic.
func nearestKey(k string, allowed []string) string {
	best, bestDist := "", 4
	for _, cand := range allowed {
		if dist := levenshtein(k, cand); dist < bestDist {
			best, bestDist = cand, dist
		}
	}
	return best
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		return "number"
	case []any:
		return "list"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
