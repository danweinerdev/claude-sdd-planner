package ops

// Revision remapping records Git's rewrite identity without rewriting any
// observation. The graph remains an append-only account of what was actually
// observed; lineage says only which commit Git produced from that commit.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

const (
	RemapRecorded        = "recorded"
	RemapAlreadyRecorded = "already-recorded"
	RemapUnchanged       = "unchanged"
	RemapUnreferenced    = "unreferenced"
)

const lineageDisclaimer = "revision lineage records Git rewrite identity only; it does not prove the rewritten code"

// RemapOptions is one fenced revision-lineage update.
type RemapOptions struct {
	Root, RepoRoot, Plan string
	Mapping              []byte
	ExpectDigest         string
	DryRun               bool
}

// RemapOutcome reports the disposition of one unique input row.
type RemapOutcome struct {
	Old    string `json:"old"`
	New    string `json:"new"`
	Status string `json:"status"`
}

// RemapResult reports both changes and deliberately skipped/no-op rows.
type RemapResult struct {
	Applied      bool              `json:"applied"`
	DryRun       bool              `json:"dry_run,omitempty"`
	ExpectDigest string            `json:"expect_digest"`
	NewDigest    string            `json:"new_digest,omitempty"`
	Outcomes     []RemapOutcome    `json:"outcomes"`
	Lineage      map[string]string `json:"revision_lineage,omitempty"`
	Note         string            `json:"note"`
}

type remapPair struct {
	old, new string
}

type graphWrite func(path, content, expect string) error

// RemapRevisions records relevant rewrite pairs under one caller-supplied
// graph digest. It never retries a stale evaluation.
func RemapRevisions(o RemapOptions) (*RemapResult, error) {
	return remapRevisionsWithWrite(o, istore.WriteAtomicExpecting)
}

func remapRevisionsWithWrite(o RemapOptions, write graphWrite) (*RemapResult, error) {
	if err := review.ValidatePlanName(o.Plan); err != nil {
		return nil, fmt.Errorf("graph remap-revisions: %w", err)
	}
	planDir := filepath.Join(o.Root, "Plans", o.Plan)
	graphPath := gstore.PathFor(planDir)
	art, err := istore.Read(graphPath)
	if err != nil {
		return nil, fmt.Errorf("graph remap-revisions: reading graph: %w", err)
	}
	if !art.Exists {
		return nil, fmt.Errorf("graph remap-revisions: %s does not exist", graphPath)
	}
	g, err := model.DecodeGraph([]byte(art.Source))
	if err != nil {
		return nil, fmt.Errorf("graph remap-revisions: graph %s is not valid:\n%w", graphPath, err)
	}

	pairs, err := parseRemapPairs(o.Mapping)
	if err != nil {
		return nil, fmt.Errorf("graph remap-revisions: %w", err)
	}
	loaded, err := rules.LoadRootRepo(o.Root, o.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("graph remap-revisions: loading planning root: %w", err)
	}
	planRel := filepath.ToSlash(filepath.Join("Plans", o.Plan, "README.md"))
	if loaded.ByPath[planRel] == nil {
		return nil, fmt.Errorf("graph remap-revisions: %s does not exist", planRel)
	}
	target := loaded.RepoForArtifact(planRel)
	// Detection that could not run is never the answer "not a Git
	// repository": the two are different facts, and the second would send
	// the operator to fix a plan mapping that is already correct (FR-16,
	// DD-10).
	repo, err := vcs.DetectChecked(target)
	if err != nil {
		return nil, fmt.Errorf("graph remap-revisions: plan %s targets %s, whose VCS could not be determined: %w", o.Plan, target, err)
	}
	if repo.Kind() != vcs.Git && repo.Kind() != vcs.GitWorktree && repo.Kind() != vcs.GitBare {
		return nil, fmt.Errorf("graph remap-revisions: plan %s targets %s, which is not a Git repository", o.Plan, target)
	}
	// Syntax is checked for every row (a regex); existence is probed only
	// for revisions the graph can reference — a large rebase capture is
	// mostly rows about commits no observation names, and each probe is a
	// git subprocess.
	for _, pair := range pairs {
		for _, rev := range []string{pair.old, pair.new} {
			if !repo.RevisionSyntaxValid(rev) {
				return nil, fmt.Errorf("graph remap-revisions: %q is not a full 40-character Git commit ID", rev)
			}
		}
	}

	existing := cloneRevisionLineage(g.RevisionLineage)
	existingNormalized := normalizedLineage(existing)
	validation := cloneRevisionLineage(existing)
	if validation == nil {
		validation = map[string]string{}
	}
	for _, pair := range pairs {
		if pair.old == pair.new {
			continue
		}
		if prior, ok := existingNormalized[pair.old]; ok && prior != pair.new {
			return nil, &RefusedError{Reasons: []string{fmt.Sprintf("graph remap-revisions: changed binding refused: %s is already mapped to %s, not %s", pair.old, prior, pair.new)}}
		}
		if _, ok := existingNormalized[pair.old]; !ok {
			validation[pair.old] = pair.new
		}
	}
	if err := validateRemapLineage(validation); err != nil {
		return nil, &RefusedError{Reasons: []string{"graph remap-revisions: " + err.Error()}}
	}

	reachable := map[string]bool{}
	for i := range g.Nodes {
		v := g.Nodes[i].Verification
		if v != nil && v.Provenance != nil && (v.Provenance.Kind == "git" || v.Provenance.Kind == "git-worktree") && len(v.Provenance.Revision) == 40 {
			reachable[strings.ToLower(v.Provenance.Revision)] = true
		}
	}
	for oldRev, newRev := range existing {
		reachable[strings.ToLower(oldRev)], reachable[strings.ToLower(newRev)] = true, true
	}
	// Input order is not semantic. Grow relevance to a fixed point so a
	// multi-step old->mid->new chain works in either row order.
	for changed := true; changed; {
		changed = false
		for _, pair := range pairs {
			if pair.old != pair.new && reachable[pair.old] && !reachable[pair.new] {
				reachable[pair.new] = true
				changed = true
			}
		}
	}
	probed := map[string]bool{}
	for _, pair := range pairs {
		if pair.old == pair.new || !reachable[pair.old] {
			continue
		}
		for _, rev := range []string{pair.old, pair.new} {
			if probed[rev] {
				continue
			}
			probed[rev] = true
			exists, checkErr := repo.RevisionExists(rev)
			if checkErr != nil || !exists {
				return nil, fmt.Errorf("graph remap-revisions: %s is not an available commit in plan target %s", rev, target)
			}
		}
	}

	final := cloneRevisionLineage(existing)
	if final == nil {
		final = map[string]string{}
	}
	res := &RemapResult{DryRun: o.DryRun, ExpectDigest: art.Digest, Note: lineageDisclaimer}
	relevant := 0
	for _, pair := range pairs {
		outcome := RemapOutcome{Old: pair.old, New: pair.new}
		switch {
		case pair.old == pair.new:
			outcome.Status = RemapUnchanged
			if reachable[pair.old] {
				relevant++
			}
		case existingNormalized[pair.old] == pair.new:
			outcome.Status = RemapAlreadyRecorded
			relevant++
		case reachable[pair.old]:
			outcome.Status = RemapRecorded
			final[pair.old] = pair.new
			relevant++
		default:
			outcome.Status = RemapUnreferenced
		}
		res.Outcomes = append(res.Outcomes, outcome)
	}
	if relevant == 0 {
		return nil, &RefusedError{Reasons: []string{"no relevant mappings: no row starts at a Git observation revision or an existing revision-lineage chain"}}
	}
	if len(final) == 0 {
		final = nil
	}
	res.Lineage = cloneRevisionLineage(final)

	if !o.DryRun {
		if o.ExpectDigest == "" {
			return nil, fmt.Errorf("graph remap-revisions: --expect-digest is required; preview with --dry-run and pass the digest it prints (%s)", art.Digest)
		}
		if !digestMatches(o.ExpectDigest, art.Digest) {
			return nil, &RefusedError{Reasons: []string{fmt.Sprintf("graph remap-revisions: the graph changed since the preview (expected %s, now %s); re-preview against the current graph, never blind-retry", short(o.ExpectDigest), short(art.Digest))}}
		}
	}

	changed := !mapsEqual(existing, final)
	g.RevisionLineage = final
	out, err := g.Encode()
	if err != nil {
		return nil, err
	}
	res.NewDigest = istore.Digest(string(out))
	if o.DryRun || !changed {
		if !changed {
			res.NewDigest = art.Digest
		}
		return res, nil
	}
	// Exactly one caller-fenced write. A collision invalidates the evaluated
	// mapping and is surfaced; there is deliberately no blind retry.
	if err := write(graphPath, string(out), art.Digest); err != nil {
		var concurrent *istore.ErrConcurrentWrite
		if errors.As(err, &concurrent) {
			return nil, &RefusedError{Reasons: []string{"graph remap-revisions: another writer landed while the remap ran; re-preview against the current graph"}}
		}
		return nil, err
	}
	res.Applied = true
	return res, nil
}

func parseRemapPairs(raw []byte) ([]remapPair, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	byOld := map[string]string{}
	var pairs []remapPair
	for line := 1; scanner.Scan(); line++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, fmt.Errorf("mapping line %d must contain exactly two columns: OLD NEW", line)
		}
		oldRev, newRev := strings.ToLower(fields[0]), strings.ToLower(fields[1])
		if prior, ok := byOld[oldRev]; ok {
			if prior != newRev {
				return nil, fmt.Errorf("mapping line %d has conflicting mappings for %s: %s and %s", line, oldRev, prior, newRev)
			}
			continue // duplicate same pair is idempotent
		}
		byOld[oldRev] = newRev
		pairs = append(pairs, remapPair{old: oldRev, new: newRev})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading mapping: %w", err)
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("mapping file contains no two-column revision rows")
	}
	return pairs, nil
}

func validateRemapLineage(lineage map[string]string) error {
	lineage = normalizedLineage(lineage)
	reverse := map[string]string{}
	for _, oldRev := range sortedMapKeys(lineage) {
		newRev := lineage[oldRev]
		if oldRev == newRev {
			return fmt.Errorf("self mapping %s is not stored", oldRev)
		}
		if prior, ok := reverse[newRev]; ok && prior != oldRev {
			return fmt.Errorf("many-to-one mapping refused: %s and %s both map to %s", prior, oldRev, newRev)
		}
		reverse[newRev] = oldRev
	}
	for _, start := range sortedMapKeys(lineage) {
		seen := map[string]bool{}
		for cur := start; ; {
			if seen[cur] {
				return fmt.Errorf("cycle refused in revision mappings at %s", cur)
			}
			seen[cur] = true
			next, ok := lineage[cur]
			if !ok {
				break
			}
			cur = next
		}
	}
	return nil
}

func normalizedLineage(lineage map[string]string) map[string]string {
	out := make(map[string]string, len(lineage))
	for oldRev, newRev := range lineage {
		out[strings.ToLower(oldRev)] = strings.ToLower(newRev)
	}
	return out
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
