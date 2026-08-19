// Package store handles reading and writing artifacts on disk: content digests
// for the FR-48 isolation precondition, atomic writes (FR-24), planning-root
// resolution, and artifact discovery.
package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Digest is the content digest used for the FR-48 read-time precondition.
// Full-length hex: this is a correctness check, not a display string.
func Digest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Artifact is a file on disk plus the digest of its current bytes.
type Artifact struct {
	Path   string
	Source string
	Digest string
	Exists bool
}

// Read loads an artifact. A missing file is not an error: the caller may be
// creating it.
func Read(path string) (*Artifact, error) {
	// Accept planning-root-relative paths (`Specs/X/README.md`) alongside
	// working-directory-relative ones. Read is the choke point every
	// path-taking command flows through, so resolving here fixes them all at
	// once; the returned Artifact carries the resolved path so callers report
	// the location they actually read.
	path = ResolveArtifactPath(path)

	// Hold a shared lock across the read so a concurrent writer's rename
	// cannot land mid-read. Readers do not block each other, and a reader
	// that cannot acquire within the retry window proceeds anyway — see the
	// rationale on acquireShared.
	if l := acquireShared(path); l != nil {
		defer l.Release()
	}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Artifact{Path: path}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	src := string(b)
	return &Artifact{Path: path, Source: src, Digest: Digest(src), Exists: true}, nil
}

// WriteAtomic writes content via a temporary file in the same directory
// followed by a rename, so a crash or interruption never leaves a partial
// artifact (FR-24). Same-directory placement keeps the rename on one volume.
func WriteAtomic(path, content string) error {
	return writeAtomicChecked(path, content, "", false)
}

// WriteAtomicExpecting writes content only if the artifact's current digest is
// still expectDigest once the exclusive lock is held. An empty expectDigest
// means the artifact is expected not to exist.
//
// This is the concurrency contract in one call: take the exclusive lock, then
// re-check that the world the content was derived from still holds. Acquiring
// the lock is not permission to write — it is permission to check whether
// writing is still valid. A writer that waited behind another writer finds the
// digest changed and returns *ErrConcurrentWrite, forcing the caller back to a
// fresh read rather than silently overwriting the result it waited for.
func WriteAtomicExpecting(path, content, expectDigest string) error {
	return writeAtomicChecked(path, content, expectDigest, true)
}

func writeAtomicChecked(path, content, expectDigest string, check bool) error {
	// The exclusive lock spans the digest re-check, the temp write, and the
	// rename, so no reader observes a torn state and no second writer can
	// interleave between the check and the swap. That span is what makes the
	// pair a compare-and-swap rather than two independent operations.
	lock, err := acquireExclusive(path)
	if err != nil {
		return err
	}
	defer lock.Release()

	if check {
		current := ""
		if b, readErr := os.ReadFile(path); readErr == nil {
			current = Digest(string(b))
		} else if !os.IsNotExist(readErr) {
			return fmt.Errorf("re-reading %s under the write lock: %w", path, readErr)
		}
		if current != expectDigest {
			return &ErrConcurrentWrite{Path: path, Expected: expectDigest, Found: current}
		}
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".sdd-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing temp file: %w", err)
	}
	// Preserve the original mode when replacing an existing artifact.
	if fi, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmpName, fi.Mode().Perm())
	} else {
		_ = os.Chmod(tmpName, 0o644)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// Config is the subset of planning-config.json this tool needs.
type Config struct {
	PlanningRoot string `json:"planningRoot"`
}

// DescribeJSONError renders a JSON decode error with the line and column of
// the offending byte when the error carries an offset. encoding/json's bare
// "invalid character '}' looking for beginning of object key string" names no
// location, so a trailing comma in planning-config.json sent authors hunting
// the whole file (G-4).
func DescribeJSONError(raw []byte, err error) string {
	var off int64
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		off = syn.Offset
	case errors.As(err, &typ):
		off = typ.Offset
	}
	if off <= 0 || off > int64(len(raw)) {
		return err.Error()
	}
	prefix := raw[:off]
	line := 1 + bytes.Count(prefix, []byte("\n"))
	col := int(off)
	if nl := bytes.LastIndexByte(prefix, '\n'); nl >= 0 {
		col = int(off) - nl - 1
	}
	return fmt.Sprintf("line %d, column %d: %s", line, col, err.Error())
}

// FindPlanningRoot walks upward from start looking for planning-config.json and
// resolves the planning root it declares. `planningRoot` may be ".", a relative
// subdirectory, or an absolute path.
func FindPlanningRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		cfgPath := filepath.Join(dir, "planning-config.json")
		if b, err := os.ReadFile(cfgPath); err == nil {
			var c Config
			if err := json.Unmarshal(b, &c); err != nil {
				return "", fmt.Errorf("%s: %s", cfgPath, DescribeJSONError(b, err))
			}
			if c.PlanningRoot == "" {
				return "", fmt.Errorf("%s: planningRoot is empty", cfgPath)
			}
			if filepath.IsAbs(c.PlanningRoot) {
				return filepath.Clean(c.PlanningRoot), nil
			}
			return filepath.Clean(filepath.Join(dir, c.PlanningRoot)), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no planning-config.json found at or above %s", start)
		}
		dir = parent
	}
}

// typeDirs maps an artifact type to the directory holding it and whether its
// documents live in a per-item subdirectory (README.md) or flat.
var typeDirs = map[string]struct {
	Dir    string
	Nested bool
}{
	"spec":     {"Specs", true},
	"design":   {"Designs", true},
	"plan":     {"Plans", true},
	"research": {"Research", false},
}

// List returns planning-root-relative paths of every artifact of a type, using
// forward slashes on every platform (FR-10).
func List(root, artifactType string) ([]string, error) {
	td, ok := typeDirs[artifactType]
	if !ok {
		return nil, fmt.Errorf("unknown artifact type %q", artifactType)
	}
	base := filepath.Join(root, td.Dir)
	ents, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", base, err)
	}
	var out []string
	for _, e := range ents {
		if td.Nested {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(base, e.Name(), "README.md")
			if _, err := os.Stat(p); err != nil {
				continue
			}
			out = append(out, rel(root, p))
			continue
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, rel(root, filepath.Join(base, e.Name())))
	}
	return out, nil
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

// ResolveArtifactPath accepts the two spellings an author naturally reaches
// for and returns the one that exists on disk:
//
//	.plans/Specs/Feature/README.md   working-directory-relative
//	Specs/Feature/README.md          planning-root-relative
//
// Field feedback: `sdd show Specs/...` failed while `.plans/Specs/...`
// worked. Every artifact identifier the tool prints — validator diagnostics,
// `list` output, `next` suggestions, `related` frontmatter — is planning-root
// relative, so copying one back into a command was the obvious move and the
// one that did not work.
//
// Resolution order is deliberate: the literal path wins when it exists, so a
// real file is never shadowed by a same-named artifact under the planning
// root. Only when the literal path is absent is the planning-root form tried.
// Absolute paths and paths that exist are returned untouched, so this is
// additive — no previously working invocation changes meaning.
func ResolveArtifactPath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	root, err := FindPlanningRoot(".")
	if err != nil {
		return path // no config: nothing to resolve against; report the original
	}
	candidate := filepath.Join(root, path)
	if _, err := os.Stat(candidate); err == nil {
		// Prefer the relative spelling when the planning root is inside the
		// working directory, so what the tool echoes back stays
		// copy-pasteable. Rel is computed from the absolute cwd rather than
		// ".", because a cwd reached through a symlink (the common case for
		// temp dirs, and for $HOME on some systems) makes Rel(".", abs)
		// return an absolute-looking result.
		if wd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(wd, candidate); err == nil && !strings.HasPrefix(rel, "..") {
				return rel
			}
		}
		return candidate
	}
	return path
}
