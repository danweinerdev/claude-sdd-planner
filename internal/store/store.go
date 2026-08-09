// Package store handles reading and writing artifacts on disk: content digests
// for the FR-48 isolation precondition, atomic writes (FR-24), planning-root
// resolution, and artifact discovery.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
				return "", fmt.Errorf("%s: %w", cfgPath, err)
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
