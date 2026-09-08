package decisionview

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/dlg"
	"gopkg.in/yaml.v3"
)

type Roots struct{ Repository, Planning string }
type CollectionFile struct {
	Path    string
	Source  []byte
	Archive bool
	info    os.FileInfo
}

var ErrInvalidCollection = errors.New("decisionview: invalid decision collection")
var ErrSourceChanged = errors.New("decisionview: source changed during snapshot read")

const maxCollectionFileBytes = 64 << 20

type Collection struct {
	ID          CollectionID
	Locator     SourceLocator
	Files       []CollectionFile
	Entries     map[string]map[string]any
	EntryPaths  map[string]string
	Metadata    *ForkMetadata
	Diagnostics []dlg.Diagnostic
	Digests     map[string]string
}

func LoadCollection(roots Roots, id CollectionID, locator SourceLocator) (*Collection, error) {
	if err := id.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCollection, err)
	}
	if err := locator.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCollection, err)
	}
	base := roots.Planning
	if locator.Root == SourceRootRepository {
		base = roots.Repository
	}
	if base == "" {
		return nil, fmt.Errorf("%w: source root %q was not supplied", ErrInvalidCollection, locator.Root)
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	archives, err := collectionArchives(r, locator)
	if err != nil {
		return nil, err
	}
	members := append([]string{locator.Path}, archives...)
	c := &Collection{ID: id, Locator: locator, Entries: map[string]map[string]any{}, EntryPaths: map[string]string{}, Digests: map[string]string{}}
	var ledgers []*dlg.Ledger
	for i, name := range members {
		raw, info, err := readCollectionFile(r, name)
		if err != nil {
			return c, err
		}
		for _, previous := range c.Files {
			if os.SameFile(previous.info, info) {
				return c, fmt.Errorf("%w: aliases select the same member twice", ErrInvalidCollection)
			}
		}
		nodes, err := collectionFrontmatter(raw)
		if err != nil {
			return c, err
		}
		// Retain the original physical locator for diagnostics only. The byte
		// parser and explicit-role validation never reopen it or discover peers.
		l, diags := dlg.ParseLedgerBytes(filepath.Join(base, filepath.FromSlash(name)), raw)
		c.Diagnostics = append(c.Diagnostics, diags...)
		if l == nil {
			return c, ErrInvalidCollection
		}
		ledgers = append(ledgers, l)
		c.Files = append(c.Files, CollectionFile{Path: name, Source: raw, Archive: i > 0, info: info})
		c.Digests[name] = collectionDigest(raw)
		if node := nodes["fork"]; node != nil {
			if i != 0 {
				return c, fmt.Errorf("%w: archive cannot select fork authority", ErrInvalidCollection)
			}
			var meta ForkMetadata
			if err := meta.UnmarshalYAML(node); err != nil {
				return c, fmt.Errorf("%w: fork metadata: %v", ErrInvalidCollection, err)
			}
			if meta.LedgerID != id {
				return c, fmt.Errorf("%w: bound source identity differs from metadata", ErrInvalidCollection)
			}
			c.Metadata = &meta
		}
		if entries := nodes["decisions"]; entries != nil && entries.Kind == yaml.SequenceNode {
			for _, node := range entries.Content {
				data, err := modelYAMLJSON(node)
				if err != nil {
					return c, fmt.Errorf("%w: decision fields: %v", ErrInvalidCollection, err)
				}
				var entry map[string]any
				dec := json.NewDecoder(bytes.NewReader(data))
				dec.UseNumber()
				if err := dec.Decode(&entry); err != nil {
					return c, fmt.Errorf("%w: decision must be a mapping", ErrInvalidCollection)
				}
				key, _ := entry["id"].(string)
				// Duplicate IDs are diagnosed by the existing collection rules;
				// never overwrite the first record in the explanatory snapshot.
				if _, exists := c.Entries[key]; !exists {
					c.Entries[key] = entry
					c.EntryPaths[key] = name
				}
			}
		}
	}
	more, _ := dlg.ValidateExplicitCollection(ledgers[0], ledgers[1:])
	c.Diagnostics = append(c.Diagnostics, more...)
	for _, d := range c.Diagnostics {
		if d.Severity.Invalidating() {
			return c, ErrInvalidCollection
		}
	}
	// Capture/recheck inventory as well as bytes; a newly matching archive is
	// a changed input even when all initially opened files stayed unchanged.
	after, err := collectionArchives(r, locator)
	if err != nil {
		return c, err
	}
	if strings.Join(archives, "\x00") != strings.Join(after, "\x00") {
		return c, ErrSourceChanged
	}
	for _, f := range c.Files {
		raw, _, err := readCollectionFile(r, f.Path)
		if err != nil {
			return c, err
		}
		if c.Digests[f.Path] != collectionDigest(raw) {
			return c, ErrSourceChanged
		}
	}
	return c, nil
}

func LoadSelectedCollection(selection *Selection) (*Collection, error) {
	if selection == nil || !selection.Explicit || selection.Config == nil {
		return nil, fmt.Errorf("%w: no explicit selection", ErrInvalidCollection)
	}
	if selection.Config.Transaction != nil {
		return nil, ErrSelectionPending
	}
	roots := Roots{Repository: selection.RepositoryRoot, Planning: selection.PlanningRoot}
	loc := SourceLocator{Root: SourceRootPlanning, Path: selection.Config.Path}
	r, err := os.OpenRoot(selection.PlanningRoot)
	if err != nil {
		return nil, err
	}
	raw, _, err := readCollectionFile(r, loc.Path)
	r.Close()
	if err != nil {
		return nil, err
	}
	nodes, err := collectionFrontmatter(raw)
	if err != nil {
		return nil, err
	}
	if nodes["fork"] == nil {
		return nil, fmt.Errorf("%w: selected file lacks fork metadata", ErrInvalidCollection)
	}
	var meta ForkMetadata
	if err := meta.UnmarshalYAML(nodes["fork"]); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCollection, err)
	}
	if err := selection.ValidateOwner(&meta); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCollection, err)
	}
	loc.Archives = append([]string(nil), meta.Archives...)
	c, err := LoadCollection(roots, selection.Config.LedgerID, loc)
	if err != nil {
		return c, err
	}
	if c.Digests[loc.Path] != collectionDigest(raw) {
		return c, ErrSourceChanged
	}
	if err := selection.ValidateOwner(c.Metadata); err != nil {
		return c, fmt.Errorf("%w: %v", ErrInvalidCollection, err)
	}
	return c, nil
}

func collectionDigest(raw []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) }

func readCollectionFile(root *os.Root, name string) ([]byte, os.FileInfo, error) {
	f, err := openCollectionMember(root, filepath.FromSlash(name))
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%w: member %q is not a regular file", ErrInvalidCollection, name)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxCollectionFileBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(b) > maxCollectionFileBytes {
		return nil, nil, fmt.Errorf("decisionview: member %q exceeds the 64 MiB read limit", name)
	}
	return b, info, nil
}

func collectionArchives(root *os.Root, loc SourceLocator) ([]string, error) {
	seen := map[string]bool{loc.Path: true}
	var result []string
	for _, pattern := range loc.Archives {
		if err := validateRelativeLocator(pattern); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCollection, err)
		}
		dir, base := path.Dir(pattern), path.Base(pattern)
		if dir != path.Dir(loc.Path) {
			return nil, fmt.Errorf("%w: archive must be a declared sibling of its canonical ledger", ErrInvalidCollection)
		}
		if _, err := path.Match(base, "probe"); err != nil {
			return nil, fmt.Errorf("%w: invalid archive pattern", ErrInvalidCollection)
		}
		var matches []string
		if !strings.ContainsAny(base, "*?[") {
			matches = []string{pattern}
		} else {
			f, err := root.Open(filepath.FromSlash(dir))
			if err != nil {
				return nil, err
			}
			entries, err := f.ReadDir(-1)
			f.Close()
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if ok, _ := path.Match(base, e.Name()); ok {
					matches = append(matches, path.Join(dir, e.Name()))
				}
			}
		}
		sort.Strings(matches)
		for _, name := range matches {
			if seen[name] {
				return nil, fmt.Errorf("%w: duplicate member %q", ErrInvalidCollection, name)
			}
			seen[name] = true
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result, nil
}

// Validate the node graph before the legacy parser traverses it: malformed
// nested aliases/duplicate keys must not recurse forever or collapse silently.
func collectionFrontmatter(raw []byte) (map[string]*yaml.Node, error) {
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("%w: missing frontmatter", ErrInvalidCollection)
	}
	end := 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "---" {
		end++
	}
	if end == len(lines) {
		return nil, fmt.Errorf("%w: missing closing frontmatter", ErrInvalidCollection)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCollection, err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: frontmatter must be a mapping", ErrInvalidCollection)
	}
	active := map[*yaml.Node]bool{}
	var visit func(*yaml.Node, int) error
	visit = func(n *yaml.Node, depth int) error {
		if n == nil || depth > 256 || active[n] {
			return fmt.Errorf("%w: cyclic or excessively nested YAML", ErrInvalidCollection)
		}
		active[n] = true
		defer delete(active, n)
		if n.Kind == yaml.AliasNode {
			return visit(n.Alias, depth+1)
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				key := n.Content[i]
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || seen[key.Value] {
					return fmt.Errorf("%w: duplicate or invalid YAML key %q", ErrInvalidCollection, key.Value)
				}
				seen[key.Value] = true
				if err := visit(n.Content[i+1], depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		for _, child := range n.Content {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(doc.Content[0], 0); err != nil {
		return nil, err
	}
	out := map[string]*yaml.Node{}
	for i := 0; i < len(doc.Content[0].Content); i += 2 {
		out[doc.Content[0].Content[i].Value] = doc.Content[0].Content[i+1]
	}
	return out, nil
}
