package decisionview

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type PreviewFileChange struct {
	Root         SourceRoot `json:"root"`
	Path         string     `json:"path"`
	BeforeExists bool       `json:"beforeExists"`
	Before       string     `json:"before"`
	After        string     `json:"after"`
}
type PreviewSource struct {
	Root   SourceRoot `json:"root"`
	Path   string     `json:"path"`
	Digest string     `json:"digest"`
}
type AuthorityDelta struct {
	Before  []QualifiedID `json:"before"`
	After   []QualifiedID `json:"after"`
	Added   []QualifiedID `json:"added"`
	Removed []QualifiedID `json:"removed"`
}
type PreviewEnvelope struct {
	Version          int                 `json:"version"`
	Status           string              `json:"status"`
	RequiresApproval bool                `json:"requiresApproval"`
	Operation        string              `json:"operation"`
	OperationID      string              `json:"operationId"`
	Date             string              `json:"date"`
	Request          json.RawMessage     `json:"request"`
	Changes          []PreviewFileChange `json:"changes"`
	Sources          []PreviewSource     `json:"sources"`
	BeforeResolution Resolution          `json:"beforeResolution"`
	AfterResolution  Resolution          `json:"afterResolution"`
	Delta            AuthorityDelta      `json:"delta"`
	Diagnostics      []Diagnostic        `json:"diagnostics"`
	Digest           string              `json:"digest,omitempty"`
}

func NewPreviewEnvelope(operation, id, date string, request json.RawMessage, changes []PreviewFileChange, sources []PreviewSource, before, after *ResolvedView) (*PreviewEnvelope, error) {
	if before == nil || after == nil {
		return nil, fmt.Errorf("decisionview: before and after authority snapshots are required")
	}
	a, err := previewBindingIDs(before)
	if err != nil {
		return nil, err
	}
	b, err := previewBindingIDs(after)
	if err != nil {
		return nil, err
	}
	p := &PreviewEnvelope{Version: 1, Status: "proposal", RequiresApproval: true, Operation: operation, OperationID: id, Date: date, Request: append(json.RawMessage(nil), request...), Changes: append([]PreviewFileChange(nil), changes...), Sources: append([]PreviewSource(nil), sources...), BeforeResolution: before.Resolution, AfterResolution: after.Resolution, Delta: AuthorityDelta{Before: a, After: b, Added: previewDifference(b, a), Removed: previewDifference(a, b)}, Diagnostics: append(append([]Diagnostic(nil), before.Diagnostics...), after.Diagnostics...)}
	sort.Slice(p.Sources, func(i, j int) bool {
		if p.Sources[i].Root != p.Sources[j].Root {
			return p.Sources[i].Root < p.Sources[j].Root
		}
		return p.Sources[i].Path < p.Sources[j].Path
	})
	if err := validatePreviewShape(p); err != nil {
		return nil, err
	}
	digest, err := previewDigest(p)
	if err != nil {
		return nil, err
	}
	p.Digest = digest
	return p, nil
}
func DecodePreviewEnvelope(data []byte) (*PreviewEnvelope, error) {
	var p PreviewEnvelope
	if err := modelStrictJSON(data, &p); err != nil {
		return nil, err
	}
	if err := VerifyPreviewEnvelope(&p, p.Digest); err != nil {
		return nil, err
	}
	return &p, nil
}

// VerifyPreviewEnvelope verifies content binding, not human consent or semantic
// permission to write arbitrary files. The mutation coordinator must regenerate
// the operation from current snapshots, verify owned paths and cross-root source
// aliases, and require the user's explicit approval before applying changes.
func VerifyPreviewEnvelope(envelope *PreviewEnvelope, digest string) error {
	if err := validatePreviewShape(envelope); err != nil {
		return err
	}
	if digest == "" || digest != envelope.Digest {
		return fmt.Errorf("decisionview: exact proposal digest is required")
	}
	want, err := previewDigest(envelope)
	if err != nil {
		return err
	}
	if want != digest {
		return fmt.Errorf("decisionview: proposal differs from the exact displayed bytes")
	}
	return nil
}

var previewDigestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func validatePreviewShape(p *PreviewEnvelope) error {
	if p == nil || p.Version != 1 || p.Status != "proposal" || !p.RequiresApproval {
		return fmt.Errorf("decisionview: unsupported or self-approved preview envelope")
	}
	switch p.Operation {
	case "adopt", "override", "reconcile", "restore", "rebind", "detach", "archive", "add", "accept", "supersede":
	default:
		return fmt.Errorf("decisionview: unsupported preview operation %q", p.Operation)
	}
	if strings.TrimSpace(p.OperationID) == "" || strings.ContainsAny(p.OperationID, "\x00\r\n") {
		return fmt.Errorf("decisionview: explicit operation identity required")
	}
	if _, err := time.Parse("2006-01-02", p.Date); err != nil {
		return fmt.Errorf("decisionview: explicit ISO proposal date required")
	}
	var request map[string]any
	if err := modelStrictJSON(p.Request, &request); err != nil || request == nil {
		return fmt.Errorf("decisionview: operation request must be a valid object")
	}
	if !previewResolution(p.BeforeResolution) || !previewResolution(p.AfterResolution) {
		return fmt.Errorf("decisionview: invalid authority snapshot resolution")
	}
	if len(p.Changes) == 0 {
		return fmt.Errorf("decisionview: preview has no file changes")
	}
	reads := map[string]bool{}
	for _, s := range p.Sources {
		if err := previewLocator(s.Root, s.Path); err != nil {
			return err
		}
		if !previewDigestRe.MatchString(s.Digest) {
			return fmt.Errorf("decisionview: malformed source precondition digest")
		}
		key := string(s.Root) + "\x00" + s.Path
		if reads[key] {
			return fmt.Errorf("decisionview: duplicate source precondition")
		}
		reads[key] = true
	}
	writes := map[string]bool{}
	for _, change := range p.Changes {
		if err := previewLocator(change.Root, change.Path); err != nil {
			return err
		}
		if change.Root == SourceRootRepository && (change.Path != "planning-config.json" || !change.BeforeExists) {
			return fmt.Errorf("decisionview: repository writes are limited to its existing planning configuration")
		}
		key := string(change.Root) + "\x00" + change.Path
		if writes[key] {
			return fmt.Errorf("decisionview: duplicate changed path")
		}
		writes[key] = true
		if reads[key] {
			return fmt.Errorf("decisionview: a read-only source cannot be a changed file")
		}
		if !change.BeforeExists && change.Before != "" {
			return fmt.Errorf("decisionview: absent file cannot have prior bytes")
		}
		if !utf8.ValidString(change.Before) || !utf8.ValidString(change.After) {
			return fmt.Errorf("decisionview: preview text must be lossless UTF-8")
		}
	}
	for _, ids := range [][]QualifiedID{p.Delta.Before, p.Delta.After, p.Delta.Added, p.Delta.Removed} {
		for i, id := range ids {
			if err := id.Validate(); err != nil {
				return err
			}
			if i > 0 && ids[i-1] >= id {
				return fmt.Errorf("decisionview: authority IDs must be unique and sorted")
			}
		}
	}
	if !previewIDsEqual(p.Delta.Added, previewDifference(p.Delta.After, p.Delta.Before)) || !previewIDsEqual(p.Delta.Removed, previewDifference(p.Delta.Before, p.Delta.After)) {
		return fmt.Errorf("decisionview: authority delta does not match its independent before/after sets")
	}
	return nil
}
func previewLocator(root SourceRoot, p string) error {
	if root != SourceRootPlanning && root != SourceRootRepository {
		return fmt.Errorf("decisionview: invalid preview root")
	}
	return validateRelativeLocator(p)
}
func previewResolution(r Resolution) bool {
	switch r {
	case ResolutionComplete, ResolutionReconciliationNeeded, ResolutionInvalid, ResolutionRecoveryNeeded:
		return true
	}
	return false
}
func previewBindingIDs(v *ResolvedView) ([]QualifiedID, error) {
	var out []QualifiedID
	seen := map[QualifiedID]bool{}
	for _, r := range v.Records {
		if r.Applicability != "binding" {
			continue
		}
		if err := r.ID.Validate(); err != nil {
			return nil, err
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("decisionview: ambiguous binding identity")
		}
		seen[r.ID] = true
		out = append(out, r.ID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
func previewDifference(a, b []QualifiedID) []QualifiedID {
	set := map[QualifiedID]bool{}
	for _, id := range b {
		set[id] = true
	}
	var out []QualifiedID
	for _, id := range a {
		if !set[id] {
			out = append(out, id)
		}
	}
	return out
}
func previewIDsEqual(a, b []QualifiedID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func previewDigest(p *PreviewEnvelope) (string, error) {
	copy := *p
	copy.Digest = ""
	raw, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte("sdd-fork-preview-v1\x00"))
	h.Write(raw)
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}
