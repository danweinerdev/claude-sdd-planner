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
	"sort"
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
// or unreadable paths return "" — the distinct value the staleness rules
// treat as "cannot match any recorded digest". A directory artifact (graphs
// declare them with a trailing slash, e.g. "crates/") fingerprints via Dir.
func (d *Digester) Artifact(rel string) string {
	key := filepath.Clean(rel)
	if v, ok := d.cache[key]; ok {
		return v
	}
	full := filepath.Join(d.root, key)
	var v string
	var err error
	if fi, statErr := os.Stat(full); statErr == nil && fi.IsDir() {
		v, err = Dir(full)
	} else {
		v, err = File(full)
	}
	if err != nil {
		v = ""
	}
	d.cache[key] = v
	return v
}

// Dir fingerprints a directory tree deterministically: every regular file
// under it, sorted by forward-slash relative path, contributes one manifest
// line "<relpath>\x00<file digest>\n" to a single SHA-256. Forward slashes
// keep the digest identical across Windows and POSIX, and identical between
// the isolated workspace where sync records it and the shared tree where a
// derive pass recomputes it. A missing or unreadable tree is an error — the
// caller decides what absence means, exactly as with File.
func Dir(path string) (string, error) {
	type entry struct{ rel, digest string }
	var entries []entry
	err := filepath.WalkDir(path, func(p string, de os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !de.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		fd, err := File(p)
		if err != nil {
			return err
		}
		entries = append(entries, entry{rel: filepath.ToSlash(rel), digest: fd})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x00%s\n", e.rel, e.digest)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// Bytes fingerprints an in-memory payload (reports, diffs) with the same
// spelling as File.
func Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", sum)
}
