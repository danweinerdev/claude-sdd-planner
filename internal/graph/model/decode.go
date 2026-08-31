package model

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	"claim":         "`next --claim` records claims under the store lock",
	"verification":  "`graph sync` records observations from parsed reports",
	"red_seqs":      "`graph sync` records first-failure seqs from parsed reports",
}

// Allowed key sets per object, for unknown-key detection and did-you-mean.
var (
	graphKeys        = []string{"version", "seq_counter", "nodes"}
	proposalKeys     = []string{"version", "nodes"}
	nodeKeys         = []string{"id", "contract", "justifies", "intent_hashes", "deps", "gate", "hazards", "artifacts", "estimate", "phase", "claim", "verification", "red_seqs"}
	gateKeys         = []string{"type", "tests", "command", "lanes"}
	testKeys         = []string{"id", "file", "satisfies"}
	claimKeys        = []string{"by", "lease_expires", "workspace"}
	verificationKeys = []string{"result", "seq", "artifact_digests", "report_digest", "isolation", "provenance"}
	provenanceKeys   = []string{"kind", "revision", "worktree", "changelist", "opened_files"}
)

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
	n.Contract = d.requiredString(path, obj, "contract")
	n.Justifies = d.stringList(path+".justifies", obj["justifies"])
	n.Deps = d.stringList(path+".deps", obj["deps"])
	n.Artifacts = d.stringList(path+".artifacts", obj["artifacts"])
	n.Phase = d.optionalString(path+".phase", obj["phase"])

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
		if v, present := obj["claim"]; present {
			n.Claim = d.claim(path+".claim", v)
		}
		if v, present := obj["verification"]; present {
			n.Verification = d.verification(path+".verification", v)
		}
		if v, present := obj["red_seqs"]; present {
			n.RedSeqs = d.intMap(path+".red_seqs", v)
		}
	}
	return n
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
