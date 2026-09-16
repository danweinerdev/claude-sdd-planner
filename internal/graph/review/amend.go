package review

// Review-driven amendments (Designs/ReviewDrivenAmendment DD-2, DD-5):
// every open finding in a frozen review artifact is either `revise` — the
// reviewed node's promise was wrong or unproven, so its contract, gate,
// justifies, or inputs change and its contract revision advances — or
// `extend` — the promise held but something is missing, so a new node
// sourced by the finding is added, depending on the reviewed node. The
// binary refuses an unclassified or no-op finding rather than guessing.
//
// This file turns an artifact's findings into an amendment plan against a
// graph. Nothing here writes; ops.AmendFromReview applies a plan under the
// store's compare-and-swap.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// Finding actions.
const (
	ActionRevise = "revise"
	ActionExtend = "extend"
)

// finding is the review-artifact object as the graph reads it: the existing
// fields plus the amendment classification.
type finding struct {
	ID     string `yaml:"id"`
	Status string `yaml:"status"`
	Title  string `yaml:"title"`
	// Action is required on every open finding: "revise" or "extend".
	Action string `yaml:"action"`
	// Nodes names the revised node(s) for a revise.
	Nodes []string `yaml:"nodes"`
	// Revise carries the changed normative fields (contract, gate,
	// justifies, inputs) in proposal-payload shape; unnamed fields keep
	// their current value.
	Revise map[string]any `yaml:"revise"`
	// Node is the extend node, in proposal-payload shape.
	Node map[string]any `yaml:"node"`
}

// Artifact is a review artifact as read from disk.
type Artifact struct {
	Path         string // as resolved on disk
	Rel          string // planning-root-relative, forward slashes
	Qualifier    string // citation qualifier (rel without .md)
	ReportDigest string
	Facts        *facts
}

// ValidatePlanName keeps graph operations inside root/Plans. Plan-taking CLI
// routes accept one directory name, never a path or volume-qualified name.
func ValidatePlanName(plan string) error {
	if plan == "" || plan == "." || plan == ".." || filepath.IsAbs(plan) || filepath.VolumeName(plan) != "" || strings.ContainsAny(plan, `/\`) {
		return fmt.Errorf("plan %q is not a single safe directory name under Plans/", plan)
	}
	return nil
}

// ReadArtifact reads and decodes a review artifact. Relative paths resolve
// against the planning root, and both lexical traversal and symlink escape are
// refused: a review outside the planning root is not admissible evidence.
func ReadArtifact(root, artifact string) (*Artifact, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving the planning root: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("resolving the planning root: %w", err)
	}
	path := filepath.FromSlash(artifact)
	if !filepath.IsAbs(path) {
		path = filepath.Join(rootReal, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving the review artifact: %w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("reading the review artifact: %w", err)
	}
	rel, err := filepath.Rel(rootReal, path)
	if err != nil || relEscapes(rel) {
		return nil, fmt.Errorf("review artifact %q is outside the planning root %s", artifact, rootReal)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the review artifact: %w", err)
	}
	f, err := readFacts(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", artifact, err)
	}
	rel = filepath.ToSlash(rel)
	sum := sha256.Sum256(raw)
	return &Artifact{Path: path, Rel: rel, Qualifier: rules.SourceQualifier(rel), ReportDigest: "sha256:" + hex.EncodeToString(sum[:]), Facts: f}, nil
}

func relEscapes(rel string) bool {
	return rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// OpenFindings returns the artifact's findings with status open.
func (a *Artifact) OpenFindings() []finding {
	var out []finding
	for _, f := range a.Facts.Findings {
		if f.Status == "open" {
			out = append(out, f)
		}
	}
	return out
}

// Amendment is one planned change.
type Amendment struct {
	Finding string `json:"finding"`
	Action  string `json:"action"`
	// Node is the revised node's id, or the new node's id for an extend.
	Node string `json:"node"`
	// Before/After are the revised node in proposal shape (revise only).
	Before *model.Node `json:"before,omitempty"`
	After  *model.Node `json:"after,omitempty"`
	// New is the extend node as it will be added (extend only).
	New *model.Node `json:"new,omitempty"`
	// Changed lists which normative fields a revise changes.
	Changed []string `json:"changed,omitempty"`
}

// Plan is the amendment preview for one artifact against one graph.
type Plan struct {
	Review       string      `json:"review"`
	Artifact     string      `json:"artifact"`
	ReportDigest string      `json:"report_digest"`
	Scope        []string    `json:"scope"`
	Amendments   []Amendment `json:"amendments"`
}

// Reach returns a review node's transitive dependency closure, excluding the
// review node itself. A review of a diff legitimately covers everything it
// depends on, not just the increment scope reviewScope derives (that scope
// already subtracts regions ceded to inner GREEN full reviews) — so revise
// and extend targets are validated against Reach, not against scope.
func Reach(g *model.Graph, reviewNode string) map[string]bool {
	adjacency := algorithms.Graph{}
	for i := range g.Nodes {
		adjacency[g.Nodes[i].ID] = g.Nodes[i].Deps
	}
	closure := algorithms.DependencyClosure(adjacency, reviewNode)
	delete(closure, reviewNode)
	return closure
}

// PlanAmendments validates every open finding and returns the amendments
// they demand, or refuses with every problem named. It does not write.
func PlanAmendments(g *model.Graph, reviewNode string, a *Artifact) (*Plan, error) {
	scope, err := Scope(g, reviewNode)
	if err != nil {
		return nil, err
	}
	return PlanAmendmentsInScope(g, reviewNode, a, scope)
}

// PlanAmendmentsInScope validates findings against an already-derived current
// scope. Publication paths use this form so artifact/intent/input-stale inner
// reviews cannot disappear behind graph-only GREEN.
func PlanAmendmentsInScope(g *model.Graph, reviewNode string, a *Artifact, scope []string) (*Plan, error) {
	node := g.NodeByID(reviewNode)
	if node == nil {
		return nil, fmt.Errorf("graph amend: node %q does not exist", reviewNode)
	}
	if node.EffectiveRole() != model.RoleReview {
		return nil, fmt.Errorf("graph amend: %q has role %q; amendments come from a review node's findings", reviewNode, node.EffectiveRole())
	}
	reach := Reach(g, reviewNode)
	reachList := make([]string, 0, len(reach))
	for id := range reach {
		reachList = append(reachList, id)
	}
	sort.Strings(reachList)
	existing := map[string]bool{}
	for _, id := range g.NodeIDs() {
		existing[id] = true
	}
	retired := map[string]bool{}
	for _, id := range g.Retired {
		retired[id] = true
	}
	plan := &Plan{Review: reviewNode, Artifact: a.Rel, ReportDigest: a.ReportDigest, Scope: scope}
	var problems []string
	addProblem := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	newIDs := map[string]bool{}
	revised := map[string]bool{}
	for _, f := range a.OpenFindings() {
		switch f.Action {
		case ActionRevise:
			if len(f.Nodes) == 0 {
				addProblem("%s: revise names no node in `nodes`", f.ID)
				continue
			}
			if len(f.Revise) == 0 {
				addProblem("%s: revise carries no `revise` block; a finding with no graph consequence is a comment — mark it answered or rejected", f.ID)
				continue
			}
			for _, id := range f.Nodes {
				if !reach[id] {
					addProblem("%s: names %q, which is outside %q's dependency closure (%s)", f.ID, id, reviewNode, strings.Join(reachList, ", "))
					continue
				}
				if revised[id] {
					addProblem("%s: %q is already revised by another finding in this artifact; merge them", f.ID, id)
					continue
				}
				before := g.NodeByID(id)
				after, changed, err := applyRevise(before, f.Revise)
				if err != nil {
					addProblem("%s: %v", f.ID, err)
					continue
				}
				if len(changed) == 0 {
					addProblem("%s: revise of %q changes nothing normative (contract, gate, justifies, inputs); mark the finding answered or rejected instead", f.ID, id)
					continue
				}
				// A review or command node states an obligation rather than
				// discharging one, so a decision can retire what its contract
				// demands and leave the node itself legitimate. Its contract
				// and the inputs that contract is read against are therefore
				// revisable in place — the ID, the gate and the dependencies
				// survive, and the contract-rev bump stales the prior
				// observation so the revised obligation is re-proved. Its
				// gate and what it justifies stay off limits: changing either
				// makes it a different node, which is a retire-and-replace.
				if before != nil && (before.Gate.Type == model.GateReview || before.Gate.Type == model.GateCommand) {
					if other := changedStructurally(changed); len(other) > 0 {
						addProblem("%s: names %q, which has gate type %q; a %s node's contract and inputs are revisable but its %s is not — retire and replace it instead", f.ID, id, before.Gate.Type, before.Gate.Type, strings.Join(other, ", "))
						continue
					}
				}
				revised[id] = true
				plan.Amendments = append(plan.Amendments, Amendment{Finding: f.ID, Action: ActionRevise, Node: id, Before: payloadCopy(before), After: after, Changed: changed})
			}
		case ActionExtend:
			if len(f.Node) == 0 {
				addProblem("%s: extend carries no `node` fragment", f.ID)
				continue
			}
			n, err := decodeFragment(f.Node)
			if err != nil {
				addProblem("%s: node fragment refused: %v", f.ID, err)
				continue
			}
			switch {
			case existing[n.ID]:
				addProblem("%s: node id %q already exists", f.ID, n.ID)
				continue
			case retired[n.ID]:
				addProblem("%s: node id %q was retired; retired ids are never reused", f.ID, n.ID)
				continue
			case newIDs[n.ID]:
				addProblem("%s: node id %q is added twice in this artifact", f.ID, n.ID)
				continue
			}
			hangsOffScope := false
			for _, dep := range n.Deps {
				if reach[dep] {
					hangsOffScope = true
				}
			}
			if !hangsOffScope {
				addProblem("%s: extend node %q must depend on at least one node in %q's dependency closure (%s)", f.ID, n.ID, reviewNode, strings.Join(reachList, ", "))
				continue
			}
			// Sourced necessity: the finding is the demand. The citation
			// resolves through the citation index's frozen-review sources.
			citation := a.Qualifier + ":" + f.ID
			if !containsString(n.Justifies, citation) {
				n.Justifies = append(n.Justifies, citation)
			}
			n.Origin = &model.Origin{Review: a.Rel, Finding: f.ID}
			n.ContractRev = 1
			newIDs[n.ID] = true
			plan.Amendments = append(plan.Amendments, Amendment{Finding: f.ID, Action: ActionExtend, Node: n.ID, New: n})
		case "":
			addProblem("%s: open finding has no `action`; every open finding is `revise` or `extend`", f.ID)
		default:
			addProblem("%s: action %q is not `revise` or `extend`", f.ID, f.Action)
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("graph amend: %s refused — %d problem(s):\n  %s", a.Rel, len(problems), strings.Join(problems, "\n  "))
	}
	sort.SliceStable(plan.Amendments, func(i, j int) bool { return plan.Amendments[i].Finding < plan.Amendments[j].Finding })
	return plan, nil
}

// changedStructurally returns the fields a revise changed that restructure a
// review or command node rather than restate its obligation, in applyRevise's
// order. The contract is the obligation itself and `inputs` are the sources it
// is read against — a revised obligation routinely cites the decision that
// revised it — so both follow the contract. `gate` and `justifies` are the
// node's role in the plan and its reason for existing; changing either makes
// it a different node, which is a retire-and-replace.
func changedStructurally(changed []string) []string {
	var other []string
	for _, f := range changed {
		if f == "gate" || f == "justifies" {
			other = append(other, f)
		}
	}
	return other
}

// applyRevise overlays a revise block onto a node's payload form and
// decodes the result strictly, reporting which normative fields changed.
func applyRevise(before *model.Node, revise map[string]any) (*model.Node, []string, error) {
	allowed := map[string]bool{"contract": true, "gate": true, "justifies": true, "inputs": true}
	for k := range revise {
		if !allowed[k] {
			return nil, nil, fmt.Errorf("revise field %q is not one of contract, gate, justifies, inputs", k)
		}
	}
	base, err := payloadMap(before)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range revise {
		base[k] = v
	}
	after, err := decodeFragment(base)
	if err != nil {
		return nil, nil, err
	}
	var changed []string
	if after.Contract != before.Contract {
		changed = append(changed, "contract")
	}
	if !sameJSON(after.Gate, before.Gate) {
		changed = append(changed, "gate")
	}
	if !sameJSON(after.Justifies, before.Justifies) {
		changed = append(changed, "justifies")
	}
	if !sameJSON(after.Inputs, before.Inputs) {
		changed = append(changed, "inputs")
	}
	return after, changed, nil
}

// payloadMap renders a node in proposal-payload form (tool-owned fields
// dropped) as a generic map, the overlay base for a revise.
func payloadMap(n *model.Node) (map[string]any, error) {
	cp := *n
	cp.Claim, cp.Verification, cp.RedSeqs = nil, nil, nil
	cp.ContractRev, cp.Origin = 0, nil
	raw, err := json.Marshal(cp)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// payloadCopy is the node in payload form, for the preview.
func payloadCopy(n *model.Node) *model.Node {
	m, err := payloadMap(n)
	if err != nil {
		return nil
	}
	out, err := decodeFragment(m)
	if err != nil {
		return nil
	}
	return out
}

// decodeFragment runs a generic node map through the strict proposal
// decoder — the same posture `sdd graph propose` takes.
func decodeFragment(m map[string]any) (*model.Node, error) {
	raw, err := json.Marshal(map[string]any{"version": model.SchemaVersion, "nodes": []any{yamlToJSON(m)}})
	if err != nil {
		return nil, err
	}
	p, err := model.DecodeProposal(raw)
	if err != nil {
		return nil, err
	}
	if len(p.Nodes) != 1 {
		return nil, fmt.Errorf("fragment decoded to %d nodes", len(p.Nodes))
	}
	n := p.Nodes[0]
	return &n, nil
}

// yamlToJSON converts yaml.v3's generic tree (which can carry
// map[interface{}]interface{} for nested maps) into JSON-encodable values.
func yamlToJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			out[k] = yamlToJSON(val)
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for k, val := range t {
			out[fmt.Sprint(k)] = yamlToJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = yamlToJSON(val)
		}
		return out
	case yaml.Node:
		var decoded any
		_ = t.Decode(&decoded)
		return yamlToJSON(decoded)
	default:
		return v
	}
}

func sameJSON(a, b any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
