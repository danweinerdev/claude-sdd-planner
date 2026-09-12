package ops

// Acknowledge records the judgment that a citation's or input's text changed
// without changing the node's obligation (VerificationFreshness DD-3). It
// rebinds one compile-time anchor on the node to the current fingerprint and
// appends an acknowledgement record naming old and new hash and who judged.
// It writes no observation, so it can green nothing: GREEN still needs a
// pass whose own snapshot matches the current text.

import (
	"errors"
	"fmt"
	"path/filepath"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// AcknowledgeOptions is one anchor rebinding.
type AcknowledgeOptions struct {
	Root, RepoRoot, Plan, Node string
	// Exactly one of Citation or Input names the anchor. Input is the
	// declared input's key as `graph show` prints it (model.InputKey).
	Citation, Input string
	ExpectDigest    string
	By              string
	DryRun          bool
}

// AcknowledgeResult reports the rebinding.
type AcknowledgeResult struct {
	Record       model.AcknowledgementRecord `json:"record"`
	Applied      bool                        `json:"applied"`
	ExpectDigest string                      `json:"expect_digest"`
	NewDigest    string                      `json:"new_digest,omitempty"`
}

// Acknowledge performs the rebinding under a caller-supplied graph digest.
func Acknowledge(o AcknowledgeOptions) (*AcknowledgeResult, error) {
	if err := review.ValidatePlanName(o.Plan); err != nil {
		return nil, fmt.Errorf("graph acknowledge: %w", err)
	}
	if (o.Citation == "") == (o.Input == "") {
		return nil, errors.New("graph acknowledge: name exactly one of --citation or --input")
	}
	planDir := filepath.Join(o.Root, "Plans", o.Plan)
	graphPath := gstore.PathFor(planDir)
	art, err := istore.Read(graphPath)
	if err != nil {
		return nil, fmt.Errorf("graph acknowledge: reading graph: %w", err)
	}
	if !art.Exists {
		return nil, fmt.Errorf("graph acknowledge: %s does not exist", graphPath)
	}
	g, err := model.DecodeGraph([]byte(art.Source))
	if err != nil {
		return nil, fmt.Errorf("graph acknowledge: graph %s is not valid:\n%w", graphPath, err)
	}
	n := g.NodeByID(o.Node)
	if n == nil {
		return nil, fmt.Errorf("graph acknowledge: node %q does not exist", o.Node)
	}
	if n.Claim != nil && n.Claim.By != o.By {
		return nil, fmt.Errorf("graph acknowledge: %q is claimed by %q; the holder acknowledges, or releases the claim first", o.Node, n.Claim.By)
	}
	sources, err := gcompile.NewSources(o.Root, o.RepoRoot, o.Plan)
	if err != nil {
		return nil, err
	}

	record := model.AcknowledgementRecord{Node: o.Node, By: o.By}
	switch {
	case o.Citation != "":
		if !containsString(n.Justifies, o.Citation) {
			return nil, fmt.Errorf("graph acknowledge: %q does not cite %q; its citations are %v", o.Node, o.Citation, n.Justifies)
		}
		disposition := sources.ClassifyCitation(o.Citation)
		if disposition.Kind != gcompile.CitationFingerprintable {
			return nil, fmt.Errorf("graph acknowledge: %q no longer resolves to fingerprintable text; an anchor cannot point at nothing — rework or replan instead", o.Citation)
		}
		record.Kind, record.Key = "citation", o.Citation
		record.Old, record.New = n.IntentHashes[o.Citation], disposition.Item.Hash
	default:
		var spec *model.Input
		for i := range n.Inputs {
			if model.InputKey(n.Inputs[i]) == o.Input {
				spec = &n.Inputs[i]
			}
		}
		if spec == nil {
			return nil, fmt.Errorf("graph acknowledge: %q declares no input with key %q", o.Node, o.Input)
		}
		resolved, rerr := sources.InputResolver().Resolve(*spec)
		if rerr != nil {
			return nil, fmt.Errorf("graph acknowledge: input %s does not resolve: %v", o.Input, rerr)
		}
		record.Kind, record.Key = "input", o.Input
		record.Old, record.New = n.InputHashes[o.Input], resolved.Digest
	}
	if record.Old == record.New {
		return nil, fmt.Errorf("graph acknowledge: %s %q already anchors to the current text; nothing to acknowledge", record.Kind, record.Key)
	}
	res := &AcknowledgeResult{Record: record, ExpectDigest: art.Digest}
	if o.DryRun {
		return res, nil
	}
	if o.ExpectDigest == "" {
		return nil, fmt.Errorf("graph acknowledge: --expect-digest is required; preview with --dry-run and pass the digest it prints (%s)", art.Digest)
	}
	if !digestMatches(o.ExpectDigest, art.Digest) {
		return nil, fmt.Errorf("graph acknowledge: the graph changed since the preview (expected %s, now %s); re-preview", short(o.ExpectDigest), short(art.Digest))
	}
	g.SeqCounter++
	record.Seq = g.SeqCounter
	switch record.Kind {
	case "citation":
		if n.IntentHashes == nil {
			n.IntentHashes = map[string]string{}
		}
		n.IntentHashes[record.Key] = record.New
	case "input":
		if n.InputHashes == nil {
			n.InputHashes = map[string]string{}
		}
		n.InputHashes[record.Key] = record.New
	}
	g.Acknowledgements = append(g.Acknowledgements, record)
	out, err := g.Encode()
	if err != nil {
		return nil, err
	}
	if err := istore.WriteAtomicExpecting(graphPath, string(out), art.Digest); err != nil {
		var concurrent *istore.ErrConcurrentWrite
		if errors.As(err, &concurrent) {
			return nil, errors.New("graph acknowledge: another writer landed while this acknowledgement ran; re-preview against the current graph")
		}
		return nil, err
	}
	res.Record = record
	res.Applied = true
	res.NewDigest = istore.Digest(string(out))
	return res, nil
}
