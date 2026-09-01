// Package digest computes the content fingerprints that anchor observations
// (Designs/SddGraph DD-6): SHA-256 over a file's bytes, spelled
// `sha256:<hex>` so a future algorithm change is a visible migration. Sync
// records these at observation time; derived states recompute them on read —
// a mismatch is digest staleness, the trigger that catches silent edits no
// sync ever saw, uniformly across git, p4, and plain trees.
package digest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// File fingerprints one file's current content. A missing or unreadable file
// returns an error — the caller decides whether absence means stale
// (a recorded artifact vanished) or simply unrecorded.
func File(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Digester memoizes File within one invocation, keyed by cleaned path. A
// derive pass may consult the same artifact from many nodes; reading it once
// per run is both faster and consistent (every node sees the same bytes even
// if the file changes mid-pass).
type Digester struct {
	root  string
	cache map[string]string
}

// New returns a Digester resolving repo-relative artifact paths against
// root.
func New(root string) *Digester {
	return &Digester{root: root, cache: map[string]string{}}
}

// Artifact fingerprints one declared artifact path (repo-relative). Missing
// or unreadable files return "" — the distinct value the staleness rules
// treat as "cannot match any recorded digest".
func (d *Digester) Artifact(rel string) string {
	key := filepath.Clean(rel)
	if v, ok := d.cache[key]; ok {
		return v
	}
	v, err := File(filepath.Join(d.root, key))
	if err != nil {
		v = ""
	}
	d.cache[key] = v
	return v
}

// Bytes fingerprints an in-memory payload (reports, diffs) with the same
// spelling as File.
func Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", sum)
}
