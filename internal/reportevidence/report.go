// Package reportevidence validates repository-produced native test reports.
// It never executes or probes a test runner.
package reportevidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/inputs"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/testevidence"
)

const Protocol = "reported-v1"
const MaxMetadataBytes = 4 << 20
const MaxReportBytes = 64 << 20

type Context struct {
	Protocol          string                      `json:"protocol"`
	Plan              string                      `json:"plan"`
	Node              string                      `json:"node"`
	By                string                      `json:"by"`
	ClaimInstance     string                      `json:"claim_instance"`
	WorkspaceIdentity string                      `json:"workspace_identity"`
	Selected          []testevidence.SelectedTest `json:"selected"`
	DeclaredHazards   map[string][]string         `json:"declared_hazards"`
	Candidate         Candidate                   `json:"candidate"`
}
type Candidate struct {
	Obligation          string                       `json:"obligation"`
	Artifacts           map[string]string            `json:"artifacts"`
	Dependencies        map[string]map[string]string `json:"dependencies"`
	Inputs              map[string]string            `json:"inputs"`
	Intent              map[string]string            `json:"intent"`
	SelectedTestSources map[string]string            `json:"selected_test_sources"`
}
type Metadata struct {
	Protocol     string    `json:"protocol"`
	Before       Context   `json:"before"`
	After        Context   `json:"after"`
	Phase        string    `json:"phase"`
	RedKind      string    `json:"red_kind,omitempty"`
	Fault        string    `json:"fault,omitempty"`
	Started      string    `json:"started"`
	Completed    string    `json:"completed"`
	Runner       Runner    `json:"runner"`
	Execution    Execution `json:"execution"`
	ReportDigest string    `json:"report_digest"`
}
type Runner struct {
	Identity              string            `json:"identity"`
	EnvironmentIdentities map[string]string `json:"environment_identities"`
}
type Execution struct {
	Started        bool `json:"started"`
	Completed      bool `json:"completed"`
	ReportComplete bool `json:"report_complete"`
	ExitCode       *int `json:"exit_code"`
}
type Admission struct {
	ID, Result                                    string
	Metadata                                      Metadata
	Parsed                                        testevidence.Report
	ReportDigest, MetadataDigest, CandidateDigest string
	Compatibility                                 map[string]string
	Failed                                        []string
}

func Digest(b []byte) string  { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }
func digestJSON(v any) string { b, _ := json.Marshal(v); return Digest(b) }
func PairID(report, metadata []byte) string {
	return digestJSON(struct{ Report, Metadata string }{Digest(report), Digest(metadata)})
}
func qualified(s testevidence.SelectedTest) string { return s.Package + "::" + s.ID }

func BuildContext(planningRoot, repoRoot, planDir string, g *model.Graph, n *model.Node, by string, now time.Time) (Context, error) {
	if g == nil || n == nil {
		return Context{}, fmt.Errorf("evidence context: graph and node are required")
	}
	c := Context{
		Protocol: Protocol, Plan: filepath.Base(planDir), Node: n.ID, By: by,
		Selected: []testevidence.SelectedTest{}, DeclaredHazards: map[string][]string{},
		Candidate: Candidate{Artifacts: map[string]string{}, Dependencies: map[string]map[string]string{}, Inputs: map[string]string{}, Intent: map[string]string{}, SelectedTestSources: map[string]string{}},
	}
	if n.Gate.Evidence != model.EvidenceReportedV1 {
		return c, fmt.Errorf("evidence context: node %q does not require reported-v1 evidence", n.ID)
	}
	if p := model.ValidateEvidenceGate(n); len(p) > 0 {
		return c, fmt.Errorf("evidence context: invalid gate: %s", strings.Join(p, "; "))
	}
	if n.Claim == nil || n.Claim.Instance == "" || by == "" || n.Claim.By != by {
		return c, fmt.Errorf("evidence context: a live claim held by --by with an instance is required")
	}
	expires, e := time.Parse(time.RFC3339, n.Claim.LeaseExpires)
	if e != nil || !expires.After(now) {
		return c, fmt.Errorf("evidence context: claim has expired")
	}
	c.ClaimInstance = n.Claim.Instance
	c.WorkspaceIdentity = workspaceIdentity(n.Claim.Workspace)
	root := repoRoot
	if n.Claim.Workspace != "" {
		if filepath.IsAbs(n.Claim.Workspace) {
			root = n.Claim.Workspace
		} else {
			root = filepath.Join(repoRoot, filepath.FromSlash(n.Claim.Workspace))
		}
	}
	c.Candidate.Obligation = n.ProofSnapshot()
	for _, a := range n.Artifacts {
		h, err := safeRootFileDigest(root, a, planDir)
		if err != nil {
			return c, fmt.Errorf("evidence context: artifact %q: %w", a, err)
		}
		c.Candidate.Artifacts[a] = h
	}
	for _, dep := range n.Deps {
		dn := g.NodeByID(dep)
		if dn == nil {
			return c, fmt.Errorf("evidence context: dependency %q is missing", dep)
		}
		m := map[string]string{}
		for _, a := range dn.Artifacts {
			h, err := safeRootFileDigest(root, a, planDir)
			if err != nil {
				return c, fmt.Errorf("evidence context: dependency artifact %q: %w", a, err)
			}
			m[a] = h
		}
		c.Candidate.Dependencies[dep] = m
	}
	sources, e := gcompile.NewSources(planningRoot, repoRoot, filepath.Base(planDir))
	if e != nil {
		return c, e
	}
	intent := sources.IntentSnapshot().Hashes()
	for _, j := range n.Justifies {
		if intent[j] == "" {
			return c, fmt.Errorf("evidence context: cited intent %q is missing", j)
		}
		c.Candidate.Intent[j] = intent[j]
	}
	r := inputs.NewResolver(inputs.Roots{Repository: root, Planning: planningRoot})
	for _, in := range n.Inputs {
		inputRoot := planningRoot
		if in.Root == model.InputRootRepository {
			inputRoot = root
		}
		if _, err := resolveSafeRootFile(inputRoot, in.Path, planDir); err != nil {
			return c, fmt.Errorf("evidence context: input %s: %w", model.InputKey(in), err)
		}
		x, err := r.Resolve(in)
		if err != nil {
			return c, fmt.Errorf("evidence context: input %s: %w", model.InputKey(in), err)
		}
		c.Candidate.Inputs[model.InputKey(in)] = x.Digest
	}
	for _, t := range n.Gate.Tests {
		s := testevidence.SelectedTest{Package: t.Package, ID: t.ID, File: t.File}
		c.Selected = append(c.Selected, s)
		h, err := safeRootFileDigest(root, t.File, planDir)
		if err != nil {
			return c, fmt.Errorf("evidence context: selected test source %q: %w", t.File, err)
		}
		qid := qualified(s)
		c.Candidate.SelectedTestSources[qid] = h
		c.DeclaredHazards[qid] = append([]string(nil), t.Satisfies...)
		sort.Strings(c.DeclaredHazards[qid])
	}
	return c, nil
}

func safeRootFileDigest(root, rel, planDir string) (string, error) {
	p, err := resolveSafeRootFile(root, rel, planDir)
	if err != nil {
		return "", err
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("opened source is not a regular file")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func resolveSafeRootFile(root, rel, planDir string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || filepath.ToSlash(rel) != rel || strings.Contains(rel, ":") {
		return "", fmt.Errorf("unsafe root-relative path %q", rel)
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("unsafe root-relative path %q", rel)
		}
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	p, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	if !pathWithin(base, p) {
		return "", fmt.Errorf("path escapes root")
	}
	if err := rejectGraphOwnedPath(p, planDir, base); err != nil {
		return "", err
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		if info.IsDir() {
			return "", fmt.Errorf("directory sources are unsupported; reported-v1 is file-based")
		}
		return "", fmt.Errorf("path is not a regular file")
	}
	return p, nil
}

func rejectGraphOwnedPath(path, planDir, root string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	for _, part := range strings.Split(rel, "/") {
		if part == ".graph" || part == ".git" {
			return fmt.Errorf("path is graph-owned evidence or repository control state")
		}
	}
	plan := filepath.Base(planDir)
	graphSuffix := filepath.ToSlash(filepath.Join("Plans", plan, plan+"-Graph.json"))
	if rel == plan+"-Graph.json" || rel == graphSuffix || strings.HasSuffix(rel, "/"+graphSuffix) {
		return fmt.Errorf("path is the live plan graph")
	}
	return nil
}

func pathWithin(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func workspaceIdentity(s string) string {
	if s == "" {
		return "shared"
	}
	if filepath.IsAbs(s) {
		return "workspace:" + Digest([]byte(filepath.Clean(s)))
	}
	return "workspace:" + filepath.ToSlash(filepath.Clean(s))
}

func Validate(reportRaw, metadataRaw []byte, current Context, profile *model.ReportProfile, now time.Time) (*Admission, error) {
	if len(reportRaw) > MaxReportBytes {
		return nil, fmt.Errorf("reported evidence: native report exceeds 64 MiB")
	}
	if len(metadataRaw) > MaxMetadataBytes {
		return nil, fmt.Errorf("reported evidence: metadata exceeds 4 MiB")
	}
	if !utf8.Valid(metadataRaw) {
		return nil, fmt.Errorf("reported evidence: metadata is not valid UTF-8")
	}
	if profile == nil || profile.Format != "go-test-json-v1" {
		return nil, fmt.Errorf("reported evidence: report profile format must be go-test-json-v1")
	}
	var m Metadata
	if err := strictJSON(metadataRaw, &m); err != nil {
		return nil, fmt.Errorf("reported evidence: metadata: %w", err)
	}
	if m.Protocol != Protocol {
		return nil, fmt.Errorf("reported evidence: unsupported protocol %q", m.Protocol)
	}
	if m.Phase != "red" && m.Phase != "green" && m.Phase != "diagnostic" {
		return nil, fmt.Errorf("reported evidence: invalid phase")
	}
	if m.Phase == "red" {
		if m.RedKind != "baseline" && m.RedKind != "sensitivity" {
			return nil, fmt.Errorf("reported evidence: red requires baseline or sensitivity red_kind")
		}
		if (m.RedKind == "baseline" && m.Fault != "") || (m.RedKind == "sensitivity" && strings.TrimSpace(m.Fault) == "") {
			return nil, fmt.Errorf("reported evidence: red_kind and fault contradict")
		}
	} else if m.RedKind != "" || m.Fault != "" {
		return nil, fmt.Errorf("reported evidence: non-red metadata carries red fields")
	}
	start, e := time.Parse(time.RFC3339, m.Started)
	if e != nil {
		return nil, fmt.Errorf("reported evidence: invalid started")
	}
	end, e := time.Parse(time.RFC3339, m.Completed)
	if e != nil || end.Before(start) || end.After(now) {
		return nil, fmt.Errorf("reported evidence: invalid completed")
	}
	if !m.Execution.Started || !m.Execution.Completed || !m.Execution.ReportComplete || m.Execution.ExitCode == nil || *m.Execution.ExitCode < 0 {
		return nil, fmt.Errorf("reported evidence: execution is incomplete or exit_code is missing")
	}
	rd := Digest(reportRaw)
	if m.ReportDigest != rd {
		return nil, fmt.Errorf("reported evidence: report digest mismatch")
	}
	if profile == nil || m.Runner.Identity != profile.Runner {
		return nil, fmt.Errorf("reported evidence: runner identity mismatch")
	}
	if m.Runner.EnvironmentIdentities == nil || len(m.Runner.EnvironmentIdentities) != len(profile.EnvironmentKeys) {
		return nil, fmt.Errorf("reported evidence: environment identities do not exactly match requirements")
	}
	for _, k := range profile.EnvironmentKeys {
		if strings.TrimSpace(m.Runner.EnvironmentIdentities[k]) == "" {
			return nil, fmt.Errorf("reported evidence: environment identity %q is missing", k)
		}
	}
	if !reflect.DeepEqual(m.Before, m.After) || !reflect.DeepEqual(m.Before, current) {
		return nil, fmt.Errorf("reported evidence: before, after, and current contexts differ")
	}
	parsed, e := testevidence.ParseGoReport(reportRaw, current.Selected, *m.Execution.ExitCode)
	if e != nil {
		return nil, fmt.Errorf("reported evidence: %w", e)
	}
	if m.Phase == "diagnostic" {
		return nil, fmt.Errorf("reported evidence: diagnostic reports are never admitted")
	}
	if m.Phase == "green" && parsed.Result != model.ResultPass {
		return nil, fmt.Errorf("reported evidence: green requires all selected tests to pass")
	}
	if m.Phase == "red" && parsed.Result != model.ResultFail {
		return nil, fmt.Errorf("reported evidence: red requires actual selected-test failures")
	}
	compat := Compatibility(current, profile, m.Runner)
	var failed []string
	for _, t := range parsed.Tests {
		if t.Outcome == model.ResultFail {
			failed = append(failed, t.QualifiedID)
		}
	}
	md := Digest(metadataRaw)
	id := PairID(reportRaw, metadataRaw)
	return &Admission{ID: id, Result: parsed.Result, Metadata: m, Parsed: parsed, ReportDigest: rd, MetadataDigest: md, CandidateDigest: digestJSON(current.Candidate), Compatibility: compat, Failed: failed}, nil
}

func Compatibility(c Context, p *model.ReportProfile, r Runner) map[string]string {
	out := map[string]string{}
	sa, si := map[string]string{}, map[string]string{}
	for _, x := range p.TestSupportArtifacts {
		sa[x] = c.Candidate.Artifacts[x]
	}
	for _, x := range p.TestSupportInputs {
		si[x] = c.Candidate.Inputs[x]
	}
	for _, s := range c.Selected {
		qid := qualified(s)
		key := struct {
			Package, ID, File, Source       string
			Hazards                         []string
			Profile                         *model.ReportProfile
			Runner                          Runner
			SupportArtifacts, SupportInputs map[string]string
		}{s.Package, s.ID, s.File, c.Candidate.SelectedTestSources[qid], c.DeclaredHazards[qid], p, r, sa, si}
		out[qid] = digestJSON(key)
	}
	return out
}
func CompatibilityDigest(m map[string]string) string { return digestJSON(m) }

func strictJSON(raw []byte, dst any) error {
	if !json.Valid(raw) {
		return fmt.Errorf("invalid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := rejectDuplicates(dec); err != nil {
		return err
	}
	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func rejectDuplicates(dec *json.Decoder) error {
	tok, e := dec.Token()
	if e != nil {
		return e
	}
	return walkToken(dec, tok)
}
func walkToken(dec *json.Decoder, t json.Token) error {
	d, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch d {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			k, e := dec.Token()
			if e != nil {
				return e
			}
			s := k.(string)
			if seen[s] {
				return fmt.Errorf("duplicate field %q", s)
			}
			seen[s] = true
			v, e := dec.Token()
			if e != nil {
				return e
			}
			if e = walkToken(dec, v); e != nil {
				return e
			}
		}
		_, e := dec.Token()
		return e
	case '[':
		for dec.More() {
			v, e := dec.Token()
			if e != nil {
				return e
			}
			if e = walkToken(dec, v); e != nil {
				return e
			}
		}
		_, e := dec.Token()
		return e
	}
	return nil
}

func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
