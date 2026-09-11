package decisionview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/dlg"
	"gopkg.in/yaml.v3"
)

var errCollectionHistory = errors.New("decisionview: collection history could not be checked")

// Check the store actually containing the collection, not the represented
// repository. Explicit member patterns prevent equal bare IDs in neighboring
// collections from being merged into one history namespace.
func checkCollectionHistory(base string, c *Collection) error {
	c.History = "unavailable"
	canonical, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(c.Locator.Path)))
	if err != nil {
		return err
	}
	root := filepath.Dir(canonical)
	for {
		if info, err := os.Lstat(filepath.Join(root, ".git")); err == nil {
			if info.IsDir() {
				if entries, readErr := os.ReadDir(filepath.Join(root, ".git")); readErr == nil && len(entries) == 0 {
					return nil // an empty marker is not an available Git repository
				}
			}
			break
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("%w: %v", errCollectionHistory, err)
		}
		parent := filepath.Dir(root)
		if parent == root {
			return nil
		}
		root = parent
	}
	head, err := collectionGit(root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		// Only an unborn branch means there is no committed baseline.
		if ref, refErr := collectionGit(root, "symbolic-ref", "HEAD"); refErr == nil {
			_, refErr = collectionGit(root, "show-ref", "--verify", "--quiet", strings.TrimSpace(string(ref)))
			var exit *exec.ExitError
			if errors.As(refErr, &exit) && exit.ExitCode() == 1 {
				c.History = "unborn"
				return nil
			}
		}
		return fmt.Errorf("%w: %v", errCollectionHistory, err)
	}
	headID := strings.TrimSpace(string(head))
	rel, err := filepath.Rel(root, canonical)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	directory := filepath.ToSlash(filepath.Dir(rel))
	headDirectory := directory
	indexDirectory := directory
	if filepath.Separator == '\\' {
		// ls-tree does not support icase pathspecs. Resolve each directory's
		// recorded spelling without enumerating the entire repository tree.
		headDirectory = ""
		if directory != "." {
			for _, segment := range strings.Split(directory, "/") {
				tree := headID
				if headDirectory != "" {
					tree += ":" + headDirectory
				}
				names, err := collectionGit(root, "ls-tree", "-d", "--name-only", "-z", tree)
				if err != nil {
					return fmt.Errorf("%w: %v", errCollectionHistory, err)
				}
				match := ""
				for _, name := range strings.Split(string(names), "\x00") {
					if samePath(name, segment) {
						if match != "" {
							return fmt.Errorf("%w: ambiguous Windows directory spelling", errCollectionHistory)
						}
						match = name
					}
				}
				if match == "" {
					headDirectory = directory // no matching committed directory
					break
				}
				headDirectory = filepath.ToSlash(filepath.Join(headDirectory, match))
			}
		} else {
			headDirectory = "."
		}
		indexDirectory = ":(icase,literal)" + directory
	}
	headPaths, err := collectionGit(root, "ls-tree", "-r", "--name-only", "-z", headID, "--", headDirectory)
	if err != nil {
		return fmt.Errorf("%w: %v", errCollectionHistory, err)
	}
	indexPaths, err := collectionGit(root, "ls-files", "-z", "--", indexDirectory)
	if err != nil {
		return fmt.Errorf("%w: %v", errCollectionHistory, err)
	}
	load := func(revision string, inventory []byte) (*Collection, error) {
		paths := strings.Split(string(inventory), "\x00")
		primary := ""
		for _, p := range paths {
			if p != "" && samePath(p, rel) {
				if primary != "" && primary != p {
					return nil, fmt.Errorf("ambiguous Windows ledger spelling")
				}
				primary = p
			}
		}
		if primary == "" {
			return nil, nil
		}
		raw, err := collectionGit(root, "show", revision+":"+primary)
		if err != nil {
			return nil, err
		}
		nodes, err := collectionFrontmatter(raw)
		if err != nil {
			return nil, err
		}
		old := &Collection{ID: c.ID, Locator: c.Locator, Entries: map[string]map[string]any{}}
		patterns := c.Locator.Archives
		if node := nodes["fork"]; node != nil {
			old.Metadata = &ForkMetadata{}
			if err := old.Metadata.UnmarshalYAML(node); err != nil {
				return nil, err
			}
			if old.Metadata.LedgerID != c.ID {
				return nil, fmt.Errorf("historical collection identity differs from selected identity")
			}
			patterns = old.Metadata.Archives
		}
		for _, pattern := range patterns {
			if err := validateRelativeLocator(pattern); err != nil || filepath.Dir(pattern) != filepath.Dir(c.Locator.Path) {
				return nil, fmt.Errorf("invalid historical archive locator %q", pattern)
			}
			if _, err := filepath.Match(filepath.Base(pattern), "probe"); err != nil {
				return nil, err
			}
		}
		members := []string{primary}
		for _, p := range paths {
			if p == "" || samePath(p, primary) || !samePath(filepath.Dir(p), filepath.Dir(primary)) {
				continue
			}
			for _, pattern := range patterns {
				pattern, name := filepath.Base(pattern), filepath.Base(p)
				if filepath.Separator == '\\' {
					pattern, name = strings.ToLower(pattern), strings.ToLower(name)
				}
				if match, _ := filepath.Match(pattern, name); match {
					members = append(members, p)
					break
				}
			}
		}
		sort.Strings(members)
		for _, member := range members {
			data := raw
			if member != primary {
				data, err = collectionGit(root, "show", revision+":"+member)
				if err != nil {
					return nil, err
				}
			}
			fields, err := collectionFrontmatter(data)
			if err != nil {
				return nil, err
			}
			if entries := fields["decisions"]; entries != nil {
				if entries.Kind != yaml.SequenceNode {
					return nil, fmt.Errorf("historical decisions in %s must be a sequence", member)
				}
				for _, node := range entries.Content {
					raw, err := modelYAMLJSON(node)
					if err != nil {
						return nil, err
					}
					var entry map[string]any
					decoder := json.NewDecoder(bytes.NewReader(raw))
					decoder.UseNumber()
					if err := decoder.Decode(&entry); err != nil {
						return nil, err
					}
					id, _ := entry["id"].(string)
					if id == "" || old.Entries[id] != nil {
						return nil, fmt.Errorf("duplicate or missing historical decision identity")
					}
					old.Entries[id] = entry
				}
			}
		}
		return old, nil
	}
	committed, err := load(headID, headPaths)
	if err != nil {
		return fmt.Errorf("%w: HEAD %s: %v", errCollectionHistory, rel, err)
	}
	staged, err := load("", indexPaths)
	if err != nil {
		return fmt.Errorf("%w: index %s: %v", errCollectionHistory, rel, err)
	}
	if committed == nil && staged == nil {
		c.History = "untracked"
		return nil
	}
	compare := func(old, next *Collection, surface string) error {
		if old == nil {
			return nil
		}
		if next == nil {
			c.Diagnostics = append(c.Diagnostics, dlg.Diagnostic{Code: "FDL010", Severity: dlg.Error, Path: c.Locator.Path, Line: 1, Message: "Retained collection is missing from " + surface, Correction: "Restore the collection's own retained history."})
			return nil
		}
		owner := OwnerID(c.ID)
		if old.Metadata != nil {
			owner = old.Metadata.RepositoryID
		}
		binding, err := BindCollection("git-history", owner, old)
		if err != nil {
			return err
		}
		current := *next
		current.Diagnostics = nil // do not feed findings from another surface back into validation
		diagnostics, err := CheckContinuity(binding, &current)
		if err != nil {
			return err
		}
		if old.Metadata != nil && next.Metadata != nil && !reflect.DeepEqual(old.Metadata.LegacyContexts, next.Metadata.LegacyContexts) {
			diagnostics = append(diagnostics, Diagnostic{Code: "FDL010", Severity: Error, Message: "Retained legacy citation contexts changed", Correction: "Preserve approved historical citation ownership."})
		}
		for _, d := range diagnostics {
			c.Diagnostics = append(c.Diagnostics, dlg.Diagnostic{Code: d.Code, Severity: dlg.Severity(d.Severity), Path: c.Locator.Path, Line: 1, Message: surface + ": " + d.Message, Correction: d.Correction})
		}
		return nil
	}
	pairs := []struct {
		old, next *Collection
		surface   string
	}{{committed, staged, "staged index"}, {committed, c, "worktree"}}
	if !reflect.DeepEqual(committed, staged) {
		pairs = append(pairs, struct {
			old, next *Collection
			surface   string
		}{staged, c, "worktree versus staged history"})
	}
	for _, pair := range pairs {
		if err := compare(pair.old, pair.next, pair.surface); err != nil {
			return fmt.Errorf("%w: %v", errCollectionHistory, err)
		}
	}
	c.History = "git-head-and-index"
	if committed == nil {
		c.History = "git-index-only"
	}
	return nil
}

func collectionGit(root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var stderr historyErrorBuffer
	cmd.Stderr = &stderr
	for _, value := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(value), "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(out, maxCollectionFileBytes+1))
	if readErr != nil || len(raw) > maxCollectionFileBytes {
		cancel()
		_ = cmd.Wait()
		return nil, errors.New("could not read bounded Git history")
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return raw, nil
}

type historyErrorBuffer struct{ bytes.Buffer }

func (b *historyErrorBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 4096 - b.Len(); remaining > 0 {
		_, _ = b.Buffer.Write(p[:min(len(p), remaining)])
	}
	return n, nil
}
