package decisionview

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// ConsumerCapture is one immutable, repository-root-bound authority read for
// validators and other in-process consumers. A declared fork never degrades to
// legacy discovery: failures remain attached to the capture as diagnostics.
type ConsumerCapture struct {
	RepositoryRoot string
	PlanningRoot   string
	Declared       bool
	Selection      *Selection
	Collections    map[CollectionID]*Collection
	View           *ResolvedView
	Diagnostics    []Diagnostic
	LegacyContexts []LegacyContext
}

// CaptureForRepository reads an explicitly represented repository's selector,
// selected collection, and complete ancestor chain. It performs no writes and
// does not inspect another repository's selector when the planning root is
// external.
func CaptureForRepository(repositoryRoot string) *ConsumerCapture {
	c := &ConsumerCapture{RepositoryRoot: repositoryRoot, Collections: map[CollectionID]*Collection{}}
	declared, declarationErr := consumerDeclaration(repositoryRoot)
	c.Declared = declared
	if declarationErr != nil {
		c.issue("FDL020", Operational, "Cannot read decision authority selector: "+declarationErr.Error())
		return c
	}

	selection, err := ReadSelection(repositoryRoot)
	c.Selection = selection
	if selection != nil {
		c.RepositoryRoot = selection.RepositoryRoot
		c.PlanningRoot = selection.PlanningRoot
		c.Declared = c.Declared || selection.Explicit
	}
	if err != nil {
		if c.Declared {
			severity := Error
			if consumerOperational(err) {
				severity = Operational
			}
			c.issue("FDL020", severity, "Decision authority selector is invalid: "+err.Error())
		}
		return c
	}
	if selection == nil || !selection.Explicit {
		if c.Declared || (selection != nil && selection.RepositoryID != "") {
			c.Declared = true
			c.issue("FDL020", Error, "Decision authority selector was removed or is missing while repository staging or identity history proves prior fork authority")
		}
		return c
	}
	store, storeErr := OpenLocalStore(selection.RepositoryRoot, selection.PlanningRoot, selection.Config.LedgerID.String())
	if storeErr != nil {
		c.issue("FDL022", Operational, "Shared transaction barriers cannot be inspected: "+storeErr.Error())
		return c
	}
	defer store.Close()
	checkBarrier := func(id CollectionID, source string) bool {
		barrier, err := store.InspectForkBarrier(id)
		if err != nil {
			severity := Error
			if consumerOperational(err) {
				severity = Operational
			}
			c.issue("FDL022", severity, "Shared transaction barrier for "+source+" cannot be validated: "+err.Error())
			return false
		}
		if barrier != nil && barrier.Status != "committed" && barrier.Status != "rolled-back" {
			c.issue("FDL022", Error, "Shared transaction barrier is "+barrier.Status+" for collection "+id.String())
			return false
		}
		return true
	}
	if !checkBarrier(selection.Config.LedgerID, selection.Config.Path) {
		return c
	}

	local, err := LoadSelectedCollection(selection)
	if local != nil {
		c.Collections[local.ID] = local
	}
	if err != nil {
		if local != nil {
			c.collectionDiagnostics(local)
		}
		severity := Error
		if consumerOperational(err) {
			severity = Operational
		}
		c.issue("FDL020", severity, "Decision authority owner or selected source is invalid: "+err.Error())
		return c
	}
	c.LegacyContexts = append(c.LegacyContexts, local.Metadata.LegacyContexts...)

	roots := Roots{Repository: selection.RepositoryRoot, Planning: selection.PlanningRoot}
	seen := map[CollectionID]bool{local.ID: true}
	locators := map[CollectionID]SourceLocator{local.ID: local.Locator}
	current := local
	for depth := 0; current != nil && current.Metadata != nil && current.Metadata.ParentBindingID != ""; depth++ {
		if depth >= 32 {
			c.issue("FDL020", Error, "Decision source inheritance exceeds the supported depth")
			break
		}
		var parent *Binding
		for i := range current.Metadata.Bindings {
			if current.Metadata.Bindings[i].ID == current.Metadata.ParentBindingID {
				parent = &current.Metadata.Bindings[i]
				break
			}
		}
		if parent == nil {
			// Compose owns the structural diagnostic and retains explanatory
			// records from every source loaded so far.
			break
		}
		if seen[parent.CollectionID] {
			c.issue("FDL020", Error, "Decision source inheritance cycle detected")
			break
		}
		seen[parent.CollectionID] = true
		if !checkBarrier(parent.CollectionID, parent.Source.Path) {
			return c
		}
		next, loadErr := LoadCollection(roots, parent.CollectionID, parent.Source)
		if next != nil {
			c.Collections[next.ID] = next
		}
		if loadErr != nil {
			if next != nil {
				c.collectionDiagnostics(next)
			}
			severity := Error
			if consumerOperational(loadErr) {
				severity = Operational
			}
			c.issue("FDL020", severity, "Cannot read declared decision source: "+loadErr.Error())
			break
		}
		if next.Metadata != nil {
			c.LegacyContexts = append(c.LegacyContexts, next.Metadata.LegacyContexts...)
		}
		locators[next.ID] = next.Locator
		current = next
	}

	view, composeErr := Compose(local.ID, selection.Config.Mode, c.Collections)
	if composeErr != nil {
		c.issue("FDL020", Error, "Cannot compose declared decision authority: "+composeErr.Error())
		return c
	}
	// A writer publishes barriers before authority bytes. Recheck every
	// collection after composition, then re-read the captured inputs, so a
	// transaction or source change overlapping this read cannot be reported as
	// complete authority.
	ids := make([]CollectionID, 0, len(c.Collections))
	for id := range c.Collections {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if !checkBarrier(id, locators[id].Path) {
			return c
		}
	}
	selectionAfter, selectionErr := ReadSelection(selection.RepositoryRoot)
	if errors.Is(selectionErr, ErrSelectionPending) {
		c.issue("FDL022", Error, "Selected decision authority gained a pending transaction while being captured")
		return c
	}
	if selectionErr != nil {
		severity := Error
		if consumerOperational(selectionErr) {
			severity = Operational
		}
		c.issue("FDL022", severity, "Decision selector cannot be revalidated: "+selectionErr.Error())
		return c
	}
	if selectionAfter == nil || !bytes.Equal(selection.RawConfig, selectionAfter.RawConfig) {
		c.issue("FDL022", Operational, "Decision selector changed while authority was being captured")
		return c
	}
	for _, id := range ids {
		var after *Collection
		var reloadErr error
		if id == local.ID {
			after, reloadErr = LoadSelectedCollection(selectionAfter)
		} else {
			after, reloadErr = LoadCollection(roots, id, locators[id])
		}
		if reloadErr != nil || after == nil || !reflect.DeepEqual(c.Collections[id].Digests, after.Digests) {
			c.issue("FDL022", Operational, "Decision source changed while authority was being captured")
			return c
		}
	}
	c.View = view
	for _, d := range view.Diagnostics {
		// Compose's qualified collision pass owns semantic candidates. The
		// source collection validator necessarily found the same shape before
		// authority composition, but retaining both would report one condition
		// twice and could include a candidate from non-effective source history.
		if (d.Code == "DLG060" || d.Code == "DLG061" || d.Code == "DLG062" || d.Code == "DLG063") &&
			strings.HasPrefix(d.Message, "Source history ") {
			continue
		}
		c.Diagnostics = append(c.Diagnostics, d)
	}
	c.sortDiagnostics()
	return c
}

func consumerDeclaration(repositoryRoot string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot, "planning-config.json"))
	if os.IsNotExist(err) {
		return repositoryForkEvidence(repositoryRoot), nil
	}
	if err != nil {
		return false, err
	}
	return configDeclaresFork(raw), nil
}

func configDeclaresFork(raw []byte) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		// A malformed declaration must not silently become legacy merely because
		// the enclosing JSON could not be decoded.
		return bytes.Contains(bytes.ToLower(raw), []byte(`"decisionlog"`))
	}
	_, declared := fields["decisionLog"]
	if !declared {
		for key := range fields {
			if strings.EqualFold(key, "decisionLog") {
				declared = true
				break
			}
		}
	}
	return declared
}

func repositoryForkEvidence(repositoryRoot string) bool {
	dir, err := os.Open(repositoryRoot)
	if err == nil {
		entries, readErr := dir.ReadDir(-1)
		_ = dir.Close()
		if readErr == nil {
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".planning-config.json.config-") && entry.Type().IsRegular() {
					return true
				}
			}
		}
	}
	// History is consulted only when the represented root advertises local Git
	// metadata. Do not turn a history-free legacy read into network VCS probing.
	if _, err := os.Lstat(filepath.Join(repositoryRoot, ".git")); err != nil {
		return false
	}
	repo := vcs.Detect(repositoryRoot)
	head, err := repo.Head()
	if err != nil {
		return false
	}
	raw, err := repo.FileAt(head, "planning-config.json")
	return err == nil && configDeclaresFork(raw)
}

func consumerOperational(err error) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr) || errors.Is(err, ErrSourceChanged)
}

func (c *ConsumerCapture) issue(code string, severity Severity, message string) {
	c.Diagnostics = append(c.Diagnostics, Diagnostic{
		Code: code, Severity: severity, Path: "planning-config.json", Line: 1, Message: message,
		Correction: "Restore readable, owner-matched source authority and reconcile it before relying on decisions.",
	})
	c.sortDiagnostics()
}

func (c *ConsumerCapture) collectionDiagnostics(collection *Collection) {
	for _, d := range collection.Diagnostics {
		c.Diagnostics = append(c.Diagnostics, Diagnostic{
			Code: d.Code, Severity: Severity(d.Severity), Path: d.Path, Line: d.Line,
			Message: d.Message, Correction: d.Correction,
		})
	}
}

func (c *ConsumerCapture) sortDiagnostics() {
	sort.SliceStable(c.Diagnostics, func(i, j int) bool {
		a, b := c.Diagnostics[i], c.Diagnostics[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	unique := c.Diagnostics[:0]
	for _, d := range c.Diagnostics {
		if len(unique) == 0 || unique[len(unique)-1] != d {
			unique = append(unique, d)
		}
	}
	c.Diagnostics = unique
}
