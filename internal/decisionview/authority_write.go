package decisionview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"time"
)

var (
	ErrForkAuthorityConflict = errors.New("decisionview: fork authority precondition changed")
	ErrForkAuthorityReplay   = errors.New("decisionview: recorded fork authority operation is not the current exact state")
	ErrForkAuthorityUnknown  = errors.New("decisionview: fork authority publication outcome is unknown")
)

type ForkAuthorityPublicationOutcome string

const (
	ForkAuthorityCommitted      ForkAuthorityPublicationOutcome = "committed"
	ForkAuthorityReplayed       ForkAuthorityPublicationOutcome = "replayed"
	ForkAuthorityOutcomeUnknown ForkAuthorityPublicationOutcome = "outcome-unknown"
)

// ForkAuthorityPublicationRequest describes an approved single-file authority
// publication. The collection identity is also the writer-exclusion identity.
type ForkAuthorityPublicationRequest struct {
	Repository     string
	Planning       string
	OwnerID        OwnerID
	Collection     CollectionID
	Preview        *PreviewEnvelope
	ApprovalDigest string
}

type ForkAuthorityPublicationResult struct {
	OperationID string
	Replayed    bool
	Outcome     ForkAuthorityPublicationOutcome
}

type ForkAuthorityPublication struct {
	request           ForkAuthorityPublicationRequest
	afterInitialCheck func()
	tryLock           func(*os.File) error
	publish           func(*os.Root, string, []byte, bool) error
	afterPublish      func() error
	store             *LocalStore
	repositoryRoot    *os.Root
	held              []*os.File
}

func NewForkAuthorityPublication(request ForkAuthorityPublicationRequest) (*ForkAuthorityPublication, error) {
	if request.Preview == nil {
		return nil, errors.New("decisionview: fork authority publication requires a preview")
	}
	if err := VerifyPreviewEnvelope(request.Preview, request.ApprovalDigest); err != nil {
		return nil, err
	}
	if err := request.OwnerID.Validate(); err != nil {
		return nil, err
	}
	if err := request.Collection.Validate(); err != nil {
		return nil, err
	}
	if request.Preview.Operation != "override" && request.Preview.Operation != "reconcile" && request.Preview.Operation != "restore" {
		return nil, errors.New("decisionview: operation does not use single-file authority publication")
	}
	if len(request.Preview.Changes) != 1 {
		return nil, errors.New("decisionview: single-file authority publication requires exactly one change")
	}
	change := request.Preview.Changes[0]
	if change.Root != SourceRootPlanning || !change.BeforeExists || reservedForkStatePath(change.Path) {
		return nil, errors.New("decisionview: single-file authority publication requires an existing canonical planning ledger")
	}
	return &ForkAuthorityPublication{request: request, tryLock: lockLocalFile, publish: publishTransactionFile}, nil
}

func (p *ForkAuthorityPublication) Apply(ctx context.Context) (*ForkAuthorityPublicationResult, error) {
	if ctx == nil {
		return nil, errors.New("decisionview: fork authority publication context required")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := p.open(); err != nil {
		return nil, err
	}
	defer p.close()
	selected, err := p.captureSelected()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrForkAuthorityConflict, err)
	}
	if operationRetained(selected.local.Metadata, p.request.Preview.OperationID) {
		ids := []CollectionID{selected.local.ID}
		if err := p.acquireLocks(ctx, ids); err != nil {
			return nil, err
		}
		if err := p.checkBarriers(ids); err != nil {
			return nil, err
		}
		current, err := p.captureSelected()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrForkAuthorityConflict, err)
		}
		if !operationRetained(current.local.Metadata, p.request.Preview.OperationID) {
			return nil, ErrForkAuthorityConflict
		}
		if !bytes.Equal(current.local.Files[0].Source, []byte(p.request.Preview.Changes[0].After)) {
			return nil, ErrForkAuthorityReplay
		}
		return &ForkAuthorityPublicationResult{OperationID: p.request.Preview.OperationID, Replayed: true, Outcome: ForkAuthorityReplayed}, nil
	}
	initial, err := p.capture()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrForkAuthorityConflict, err)
	}
	ids := make([]CollectionID, 0, len(initial.collections))
	for id := range initial.collections {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if err := p.acquireLocks(ctx, ids); err != nil {
		return nil, err
	}
	if err := p.checkBarriers(ids); err != nil {
		return nil, err
	}
	current, err := p.capture()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrForkAuthorityConflict, err)
	}
	if !sameCollectionSet(initial.collections, current.collections) {
		return nil, ErrForkAuthorityConflict
	}
	if err := p.rejectAliases(current); err != nil {
		return nil, err
	}
	replayed, err := p.validateCurrent(current)
	if err != nil {
		return nil, err
	}
	if replayed {
		return &ForkAuthorityPublicationResult{OperationID: p.request.Preview.OperationID, Replayed: true, Outcome: ForkAuthorityReplayed}, nil
	}
	if p.afterInitialCheck != nil {
		p.afterInitialCheck()
	}
	current, err = p.capture()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrForkAuthorityConflict, err)
	}
	if !sameCollectionSet(initial.collections, current.collections) {
		return nil, ErrForkAuthorityConflict
	}
	replayed, err = p.validateCurrent(current)
	if err != nil {
		return nil, err
	}
	if replayed {
		return &ForkAuthorityPublicationResult{OperationID: p.request.Preview.OperationID, Replayed: true, Outcome: ForkAuthorityReplayed}, nil
	}
	change := p.request.Preview.Changes[0]
	if !bytes.Equal(current.local.Files[0].Source, []byte(change.Before)) {
		return nil, ErrForkAuthorityConflict
	}
	before, _, err := readCollectionFile(p.store.root, change.Path)
	if err != nil || !bytes.Equal(before, []byte(change.Before)) {
		return nil, ErrForkAuthorityConflict
	}
	if err := p.publish(p.store.root, change.Path, []byte(change.After), true); err != nil {
		return p.unknown(err)
	}
	if p.afterPublish != nil {
		if err := p.afterPublish(); err != nil {
			return p.unknown(err)
		}
	}
	raw, _, err := readCollectionFile(p.store.root, change.Path)
	if err != nil || !bytes.Equal(raw, []byte(change.After)) {
		return p.unknown(errors.Join(err, errors.New("published bytes differ from approved authority")))
	}
	return &ForkAuthorityPublicationResult{OperationID: p.request.Preview.OperationID, Outcome: ForkAuthorityCommitted}, nil
}

func (p *ForkAuthorityPublication) unknown(cause error) (*ForkAuthorityPublicationResult, error) {
	result := &ForkAuthorityPublicationResult{OperationID: p.request.Preview.OperationID, Outcome: ForkAuthorityOutcomeUnknown}
	return result, fmt.Errorf("%w: %v", ErrForkAuthorityUnknown, cause)
}

type forkAuthorityCapture struct {
	config      []byte
	selection   *ForkConfig
	local       *Collection
	collections map[CollectionID]*Collection
}

func (p *ForkAuthorityPublication) open() error {
	repository, err := canonicalTransactionRoot(p.request.Repository)
	if err != nil {
		return fmt.Errorf("decisionview: repository root: %w", err)
	}
	planning, err := canonicalTransactionRoot(p.request.Planning)
	if err != nil {
		return fmt.Errorf("decisionview: planning root: %w", err)
	}
	p.request.Repository, p.request.Planning = repository, planning
	p.store, err = OpenLocalStore(repository, planning, "single-authority-publication")
	if err != nil {
		return err
	}
	p.repositoryRoot, err = os.OpenRoot(repository)
	if err != nil {
		p.store.Close()
		p.store = nil
		return err
	}
	return nil
}

func (p *ForkAuthorityPublication) close() {
	for i := len(p.held) - 1; i >= 0; i-- {
		_ = unlockLocalFile(p.held[i])
		_ = p.held[i].Close()
	}
	if p.repositoryRoot != nil {
		_ = p.repositoryRoot.Close()
	}
	if p.store != nil {
		_ = p.store.Close()
	}
}

func (p *ForkAuthorityPublication) capture() (*forkAuthorityCapture, error) {
	capture, err := p.captureSelected()
	if err != nil {
		return nil, err
	}
	if err := p.loadCollectionGraph(capture.local, capture.collections); err != nil {
		return nil, err
	}
	return capture, nil
}

func (p *ForkAuthorityPublication) captureSelected() (*forkAuthorityCapture, error) {
	selection, err := ReadSelection(p.request.Repository)
	if err != nil {
		return nil, err
	}
	if selection == nil || !selection.Explicit || selection.Config == nil || selection.Config.Mode != "fork" {
		return nil, errors.New("decisionview: operation requires a complete selected fork")
	}
	if !samePath(selection.PlanningRoot, p.request.Planning) {
		return nil, errors.New("decisionview: supplied planning root differs from selected planning root")
	}
	if selection.RepositoryID != p.request.OwnerID {
		return nil, errors.New("decisionview: selected repository owner does not match publication owner")
	}
	if selection.Config.LedgerID != p.request.Collection || selection.Config.Path != p.request.Preview.Changes[0].Path || reservedForkStatePath(selection.Config.Path) {
		return nil, errors.New("decisionview: approved target is not the selected canonical fork ledger")
	}
	local, err := LoadSelectedCollection(selection)
	if err != nil {
		return nil, err
	}
	if local.Metadata == nil || local.Metadata.RepositoryID != p.request.OwnerID {
		return nil, errors.New("decisionview: selected authority owner does not match publication owner")
	}
	return &forkAuthorityCapture{config: append([]byte(nil), selection.RawConfig...), selection: selection.Config, local: local, collections: map[CollectionID]*Collection{}}, nil
}

func (p *ForkAuthorityPublication) loadCollectionGraph(collection *Collection, found map[CollectionID]*Collection) error {
	if collection == nil {
		return errors.New("decisionview: nil collection in authority graph")
	}
	if old := found[collection.ID]; old != nil {
		if !reflect.DeepEqual(old.Locator, collection.Locator) {
			return errors.New("decisionview: collection identity has multiple source locators")
		}
		return nil
	}
	found[collection.ID] = collection
	if collection.Metadata == nil {
		return nil
	}
	for _, binding := range collection.Metadata.Bindings {
		next, err := LoadCollection(Roots{Repository: p.request.Repository, Planning: p.request.Planning}, binding.CollectionID, binding.Source)
		if err != nil {
			return err
		}
		if err := p.loadCollectionGraph(next, found); err != nil {
			return err
		}
	}
	return nil
}

func (p *ForkAuthorityPublication) acquireLocks(ctx context.Context, ids []CollectionID) error {
	for _, id := range ids {
		dir := fmt.Sprintf("Decisions/.fork-state/%x", sha256.Sum256([]byte(id)))
		if err := p.store.root.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		file, err := p.store.root.OpenFile(dir+"/writer.lock", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				file.Close()
				return err
			}
			if err := p.tryLock(file); err == nil {
				break
			} else if !localLockBusy(err) {
				file.Close()
				return err
			}
			select {
			case <-ctx.Done():
				file.Close()
				return ctx.Err()
			case <-time.After(5 * time.Millisecond):
			}
		}
		p.held = append(p.held, file)
	}
	return nil
}

func (p *ForkAuthorityPublication) checkBarriers(ids []CollectionID) error {
	for _, id := range ids {
		barrier, err := p.store.InspectForkBarrier(id)
		if err != nil {
			return err
		}
		if barrier != nil && barrier.Status != "committed" && barrier.Status != "rolled-back" {
			return ErrForkBarrierPending
		}
	}
	return nil
}

func (p *ForkAuthorityPublication) validateCurrent(current *forkAuthorityCapture) (bool, error) {
	change := p.request.Preview.Changes[0]
	if operationRetained(current.local.Metadata, p.request.Preview.OperationID) {
		if bytes.Equal(current.local.Files[0].Source, []byte(change.After)) {
			return true, nil
		}
		return false, ErrForkAuthorityReplay
	}
	var regenerated *PreviewEnvelope
	var err error
	switch p.request.Preview.Operation {
	case "override", "reconcile":
		preview, previewErr := PreviewForkOverride(ForkOverrideSnapshot{Local: current.local, Collections: current.collections}, p.request.Preview.Request)
		if previewErr == nil {
			regenerated = preview.Envelope
		}
		err = previewErr
	case "restore":
		preview, previewErr := PreviewForkRestoration(ForkRestorationSnapshot{ConfigBefore: current.config, Local: current.local, Collections: current.collections}, p.request.Preview.Request)
		if previewErr == nil {
			regenerated = preview.Envelope
		}
		err = previewErr
	}
	if err != nil || !reflect.DeepEqual(regenerated, p.request.Preview) {
		return false, fmt.Errorf("%w: approved envelope does not match current semantic authority", ErrForkAuthorityConflict)
	}
	return false, nil
}

func operationRetained(metadata *ForkMetadata, operationID string) bool {
	if metadata == nil {
		return false
	}
	for _, id := range metadata.OperationIDs {
		if id == operationID {
			return true
		}
	}
	return false
}

func (p *ForkAuthorityPublication) rejectAliases(current *forkAuthorityCapture) error {
	type fileIdentity struct {
		name string
		info os.FileInfo
	}
	var seen []fileIdentity
	locators := []SourceLocator{{Root: SourceRootRepository, Path: "planning-config.json"}}
	for _, collection := range current.collections {
		for _, file := range collection.Files {
			locators = append(locators, SourceLocator{Root: collection.Locator.Root, Path: file.Path})
		}
	}
	for _, locator := range locators {
		if err := validateRelativeLocator(locator.Path); err != nil {
			return err
		}
		root := p.store.root
		if locator.Root == SourceRootRepository {
			root = p.repositoryRoot
		} else if locator.Root != SourceRootPlanning {
			return errors.New("decisionview: unsupported authority source root")
		}
		_, info, err := readCollectionFile(root, locator.Path)
		if err != nil {
			return err
		}
		name := string(locator.Root) + ":" + locator.Path
		for _, previous := range seen {
			if os.SameFile(previous.info, info) && previous.name != name {
				return fmt.Errorf("decisionview: authority locators %s and %s alias the same file", previous.name, name)
			}
		}
		seen = append(seen, fileIdentity{name: name, info: info})
	}
	return nil
}

func sameCollectionSet(a, b map[CollectionID]*Collection) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if b[id] == nil {
			return false
		}
	}
	return true
}
