package decisionview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Selection is a location/identity descriptor, not an effective authority view.
// Legacy descriptors leave ledger discovery to the caller's unchanged legacy
// path. Collection loading must check metadata before using an explicit fork.
type Selection struct {
	Explicit       bool
	RepositoryRoot string
	PlanningRoot   string
	LedgerPath     string
	RepositoryID   OwnerID
	Config         *ForkConfig
	RawConfig      []byte
}

// ErrSelectionPending marks an explicitly selected, incomplete transaction.
// Callers may inspect the returned descriptor but must not rely on authority.
var ErrSelectionPending = errors.New("decisionview: selected authority requires transaction recovery")

// ReadSelection reads only the represented repository's explicit configuration.
// It does not choose a fork by filename, create paths, load a parent collection,
// or substitute an external planning repository's own configuration.
func ReadSelection(repositoryRoot string) (*Selection, error) {
	if strings.TrimSpace(repositoryRoot) == "" {
		return nil, fmt.Errorf("decisionview: represented repository must be explicit")
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("decisionview: represented repository is not a directory")
	}
	selected := &Selection{RepositoryRoot: root, PlanningRoot: root}
	raw, err := os.ReadFile(filepath.Join(root, "planning-config.json"))
	if os.IsNotExist(err) {
		return selected, nil
	}
	if err != nil {
		return nil, err
	}
	selected.RawConfig = raw
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("decisionview: planning-config.json: %w", err)
	}
	if fields == nil {
		return nil, fmt.Errorf("decisionview: planning configuration must be an object")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key != "decisionLog" && strings.EqualFold(key, "decisionLog") {
			return nil, fmt.Errorf("decisionview: selector key must be exactly decisionLog, not %q", key)
		}
	}
	declaration, present := fields["decisionLog"]
	if !present {
		// Retain a valid declared logical identity for the collection/history
		// layer's removed-adoption check, without changing legacy discovery.
		var owner string
		if json.Unmarshal(fields["repositoryId"], &owner) == nil && OwnerID(owner).Validate() == nil {
			selected.RepositoryID = OwnerID(owner)
		}
		return selected, nil
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("decisionview: fork configuration must be UTF-8")
	}
	if err := selectionUniqueRootKeys(raw); err != nil {
		return nil, err
	}
	config, err := DecodeForkConfig(declaration)
	if err != nil {
		return nil, fmt.Errorf("decisionview: decisionLog: %w", err)
	}
	owner, err := selectionStringField(fields, "repositoryId")
	if err != nil {
		return nil, err
	}
	if err := OwnerID(owner).Validate(); err != nil {
		return nil, err
	}
	planning, err := selectionStringField(fields, "planningRoot")
	if err != nil {
		return nil, err
	}
	if planning == "" {
		return nil, fmt.Errorf("decisionview: planningRoot is required for explicit selection")
	}
	if !filepath.IsAbs(planning) {
		planning = filepath.Join(root, planning)
	}
	planning, err = filepath.EvalSymlinks(planning)
	if err != nil {
		return nil, fmt.Errorf("decisionview: planning root: %w", err)
	}
	info, err = os.Stat(planning)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("decisionview: planning root is not a directory")
	}
	ledger, err := selectionContainedPath(planning, config.Path)
	if err != nil {
		return nil, err
	}
	selected.Explicit = true
	selected.RepositoryID = OwnerID(owner)
	selected.PlanningRoot = planning
	selected.LedgerPath = ledger
	selected.Config = config
	if config.Transaction != nil {
		if _, err := selectionContainedPath(planning, config.Transaction.Journal); err != nil {
			return nil, err
		}
		return selected, ErrSelectionPending
	}
	return selected, nil
}

func (s *Selection) ValidateOwner(metadata *ForkMetadata) error {
	if s == nil || !s.Explicit || s.Config == nil {
		return fmt.Errorf("decisionview: legacy selection carries no fork authority")
	}
	if s.Config.Transaction != nil {
		return ErrSelectionPending
	}
	if err := s.Config.Validate(); err != nil {
		return err
	}
	if err := s.RepositoryID.Validate(); err != nil {
		return err
	}
	if metadata == nil {
		return fmt.Errorf("decisionview: selected ledger lacks fork metadata")
	}
	if err := metadata.Validate(); err != nil {
		return err
	}
	if metadata.LedgerID != s.Config.LedgerID {
		return fmt.Errorf("decisionview: selected ledger identity does not match decisionLog.ledgerId")
	}
	if metadata.RepositoryID != s.RepositoryID {
		return fmt.Errorf("decisionview: selected ledger belongs to a different represented repository")
	}
	return nil
}

func selectionStringField(fields map[string]json.RawMessage, key string) (string, error) {
	raw, present := fields[key]
	if !present || len(raw) == 0 || raw[0] != '"' {
		return "", fmt.Errorf("decisionview: %s must be a string", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}

// Validate the config envelope without imposing the fork model's scalar or
// unknown-key policy on unrelated extension values (which may include floats).
// The owned decisionLog sub-object receives its own recursive strict decoder.
func selectionUniqueRootKeys(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil {
		return err
	}
	if start != json.Delim('{') {
		return fmt.Errorf("decisionview: config must be an object")
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return fmt.Errorf("decisionview: config key must be a string")
		}
		if seen[name] {
			return fmt.Errorf("decisionview: duplicate config key %q", name)
		}
		for _, reserved := range []string{"decisionLog", "repositoryId", "planningRoot"} {
			if name != reserved && strings.EqualFold(name, reserved) {
				return fmt.Errorf("decisionview: config key must be exactly %s, not %q", reserved, name)
			}
		}
		seen[name] = true
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return err
		}
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decisionview: trailing config value")
		}
		return err
	}
	return nil
}

// A path preflight is not a race-proof open. Collection loading must recheck
// containment using the opened file identity before consuming source bytes.
// Missing final files are permitted for preview/adoption, but an existing
// symlink parent must not redirect even a not-yet-created file outside root.
func selectionContainedPath(root, relative string) (string, error) {
	if err := validateRelativeLocator(relative); err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	current := target
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			rel, err := filepath.Rel(root, resolved)
			if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("decisionview: selected path escapes planning root: %q", relative)
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return "", err
			}
			if current != target && !info.IsDir() {
				return "", fmt.Errorf("decisionview: selected path parent is not a directory")
			}
			if current == target && !info.Mode().IsRegular() {
				return "", fmt.Errorf("decisionview: selected path is not a regular file")
			}
			return target, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("decisionview: cannot resolve selected path")
		}
		current = parent
	}
}
