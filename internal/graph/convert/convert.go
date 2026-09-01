// Package convert turns a v1 markdown plan into a staged graph proposal
// with every unmade judgment marked by a blocking sentinel (Designs/SddGraph
// DD-15). The tool does all mechanical work — tasks become nodes, declared
// dependency order becomes deps, justifies citations carry over, completed
// tasks keep their provenance as history annotations — and deliberately
// nothing else: hazards land untriaged, gates land unspecified, contracts
// carry the NEEDS-CONTRACT marker, and the converted graph does not compile
// until an operator resolves each one through the normal payload path.
//
// The recorded trap for this work: defaulting a gate (say `make test`) so
// converted graphs compile immediately would assert a verification contract
// nobody wrote. Sentinel-then-block is the requirement, not a UX defect.
//
// Conversion is a standing capability, not a migration event: it exists for
// as long as v1 plans do.
package convert

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// Result summarizes one conversion.
type Result struct {
	// Fragment is the staged proposal file.
	Fragment string
	// Nodes is how many tasks converted.
	Nodes int
	// CompletedCarried is how many carry a v1 completion history annotation.
	CompletedCarried int
	// Phases is how many phase docs were read.
	Phases int
}

// idTokenRe extracts identifier citations from a v1 task's prose justifies
// field: requirement/criterion ids, design decisions, and ledger entries.
var idTokenRe = regexp.MustCompile(`\b(?:FR|NFR|AC)-\d{2,}\b|\bDD-\d{1,4}[a-z]?\b|\bD-\d{4,}\b`)

// Run converts one v1 plan into a staged proposal fragment. The plan's graph
// is initialized when absent (initialization is mechanical; the judgments
// stay in the sentinels).
func Run(root, repoRoot, plan string) (*Result, error) {
	loaded, err := rules.LoadRootRepo(root, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("graph convert: loading planning root: %w", err)
	}
	planRel := "Plans/" + plan + "/README.md"
	planArt, ok := loaded.ByPath[planRel]
	if !ok || planArt.Meta == nil {
		return nil, fmt.Errorf("graph convert: %s does not exist or has no frontmatter", planRel)
	}

	type phaseDoc struct {
		id    int
		label string
		deps  []int
		nodes []string // node ids, in task order
	}
	var phases []phaseDoc
	nodesByPhaseID := map[int][]string{}
	var nodes []model.Node
	completed := 0

	for _, raw := range anyList(planArt.Meta["phases"]) {
		entry := anyMap(raw)
		if entry == nil {
			continue
		}
		doc := str(entry["doc"])
		if doc == "" {
			continue
		}
		docRel := "Plans/" + plan + "/" + doc
		phaseArt, ok := loaded.ByPath[docRel]
		if !ok || phaseArt.Meta == nil {
			return nil, fmt.Errorf("graph convert: phase doc %s does not resolve", docRel)
		}
		ph := phaseDoc{
			id:    intOf(entry["id"]),
			label: strings.TrimSuffix(doc, ".md"),
			deps:  intList(entry["depends_on"]),
		}

		evidence := completionEvidence(phaseArt.Body)
		for _, t := range anyList(phaseArt.Meta["tasks"]) {
			task := anyMap(t)
			if task == nil {
				continue
			}
			taskID := str(task["id"])
			if taskID == "" {
				continue
			}
			n := model.Node{
				ID:       nodeID(taskID),
				Contract: model.NeedsContractPrefix + str(task["title"]),
				// Untriaged, unspecified: the sentinels that keep this
				// proposal from compiling until an operator judges.
				Hazards:  nil,
				Gate:     model.Gate{Type: model.GateUnspecified},
				Estimate: 1,
				Phase:    ph.label,
			}
			for _, dep := range strList(task["depends_on"]) {
				n.Deps = append(n.Deps, nodeID(dep))
			}
			seen := map[string]bool{}
			for _, cited := range idTokenRe.FindAllString(str(task["justifies"]), -1) {
				if !seen[cited] {
					seen[cited] = true
					n.Justifies = append(n.Justifies, cited)
				}
			}
			if str(task["status"]) == "complete" {
				n.History = historyLine(taskID, evidence[taskID])
				completed++
			}
			nodes = append(nodes, n)
			ph.nodes = append(ph.nodes, n.ID)
		}
		nodesByPhaseID[ph.id] = ph.nodes
		phases = append(phases, ph)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("graph convert: %s lists no phase tasks to convert", planRel)
	}

	// Declared phase order becomes dependency structure: every task of a
	// phase depends on every task of each phase it depends_on. Dense, but it
	// is exactly what the v1 plan declared — inventing a narrower order
	// would be a judgment.
	depsOfPhase := map[int][]string{}
	for _, ph := range phases {
		var acc []string
		for _, pre := range ph.deps {
			acc = append(acc, nodesByPhaseID[pre]...)
		}
		depsOfPhase[ph.id] = acc
	}
	byID := map[string]*model.Node{}
	for i := range nodes {
		byID[nodes[i].ID] = &nodes[i]
	}
	for _, ph := range phases {
		for _, id := range ph.nodes {
			n := byID[id]
			present := map[string]bool{}
			for _, d := range n.Deps {
				present[d] = true
			}
			for _, d := range depsOfPhase[ph.id] {
				if !present[d] && d != n.ID {
					present[d] = true
					n.Deps = append(n.Deps, d)
				}
			}
			sort.Strings(n.Deps)
		}
	}

	planDir := filepath.Join(root, "Plans", plan)
	if _, err := os.Stat(gstore.PathFor(planDir)); os.IsNotExist(err) {
		if _, err := gstore.Init(planDir); err != nil {
			return nil, err
		}
	}
	payload, err := proposal.Encode(&model.Proposal{Version: model.SchemaVersion, Nodes: nodes})
	if err != nil {
		return nil, err
	}
	staged, err := proposal.Stage(planDir, payload)
	if err != nil {
		return nil, err
	}
	return &Result{Fragment: staged, Nodes: len(nodes), CompletedCarried: completed, Phases: len(phases)}, nil
}

// nodeID maps a v1 task id to a graph node id: `2.3` -> `task-2-3`. Stable
// and reversible, so provenance survives the regime change.
func nodeID(taskID string) string {
	return "task-" + strings.ReplaceAll(taskID, ".", "-")
}

var (
	verifiedRe = regexp.MustCompile(`(?m)^- Verified: (\S+)`)
	revisionRe = regexp.MustCompile("(?m)^- Revision / checkpoint: `?([0-9a-fA-F]{7,40})`?")
)

// completionEvidence extracts each task section's recorded completion
// identity: task id -> "verified <date>; revision <rev>". Best-effort and
// mechanical: what the regexes cannot find is reported as such, never
// invented.
func completionEvidence(body string) map[string]string {
	out := map[string]string{}
	sections := regexp.MustCompile(`(?m)^## ([0-9]+\.[0-9]+[a-z]?)[: ]`).FindAllStringSubmatchIndex(body, -1)
	for i, m := range sections {
		id := body[m[2]:m[3]]
		end := len(body)
		if i+1 < len(sections) {
			end = sections[i+1][0]
		}
		section := body[m[0]:end]
		verified, revision := "", ""
		if vm := verifiedRe.FindStringSubmatch(section); vm != nil {
			verified = vm[1]
		}
		if rm := revisionRe.FindStringSubmatch(section); rm != nil {
			revision = rm[1]
		}
		switch {
		case verified != "" && revision != "":
			out[id] = fmt.Sprintf("verified %s; revision %s", verified, revision)
		case revision != "":
			out[id] = "revision " + revision
		case verified != "":
			out[id] = "verified " + verified
		}
	}
	return out
}

// historyLine renders the completed-task annotation. It is context for the
// human reader; it grants nothing (no retroactive observations, DD-15).
func historyLine(taskID, evidence string) string {
	if evidence == "" {
		return fmt.Sprintf("complete as v1 task %s (evidence not mechanically extractable; see the v1 phase document)", taskID)
	}
	return fmt.Sprintf("complete as v1 task %s — %s", taskID, evidence)
}

// --- frontmatter value helpers (v1 metadata arrives as any) ---

func anyList(v any) []any {
	l, _ := v.([]any)
	return l
}

func anyMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// intOf accepts the frontmatter layer's value model: numbers may arrive as
// native ints or as strings (the compatibility parser preserves scalars
// textually). A value that parses as neither is 0, which the caller treats
// as "no id" — and a graph built from 0-keyed phases wires its dependency
// densification backwards, which is exactly the bug this string branch
// fixed.
func intOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(strings.Trim(n, `"'`)))
		if err != nil {
			return 0
		}
		return i
	default:
		return 0
	}
}

func intList(v any) []int {
	var out []int
	for _, item := range anyList(v) {
		out = append(out, intOf(item))
	}
	return out
}

func strList(v any) []string {
	var out []string
	for _, item := range anyList(v) {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
