package decisionview

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"
)

var ErrForkTransactionConflict = errors.New("decisionview: fork transaction precondition changed")

type ForkTransactionOutcome string

const (
	ForkTransactionRolledBack       ForkTransactionOutcome = "rolled-back"
	ForkTransactionCommitted        ForkTransactionOutcome = "committed"
	ForkTransactionRecoveryRequired ForkTransactionOutcome = "recovery-required"
)

type ForkTransactionPoint string

const (
	ForkTransactionAfterJournal            ForkTransactionPoint = "after-journal"
	ForkTransactionAfterBarriers           ForkTransactionPoint = "after-barriers"
	ForkTransactionAfterIntermediateConfig ForkTransactionPoint = "after-intermediate-config"
	ForkTransactionBeforePlanningPublish   ForkTransactionPoint = "before-planning-publish"
	ForkTransactionAfterPlanningPublish    ForkTransactionPoint = "after-planning-publish"
	ForkTransactionAfterFinalConfig        ForkTransactionPoint = "after-final-config"
	ForkTransactionAfterCommitRecord       ForkTransactionPoint = "after-commit-record"
	ForkTransactionAfterBarrierRelease     ForkTransactionPoint = "after-barrier-release"
)

type ForkTransactionRequest struct {
	Repository     string
	Planning       string
	OwnerID        OwnerID
	Collections    []CollectionID
	Preview        *PreviewEnvelope
	ApprovalDigest string
}

type ForkTransactionResult struct {
	OperationID string
	Outcome     ForkTransactionOutcome
}

type ForkTransactionInspection struct {
	OperationID string
	Outcome     ForkTransactionOutcome
	Authority   Resolution
}

type ForkTransaction struct {
	request            ForkTransactionRequest
	afterInitialCheck  func()
	recheckSources     func() error
	failpoint          func(ForkTransactionPoint) error
	store              *LocalStore
	repositoryRoot     *os.Root
	held               []*os.File
	journal            *ForkJournal
	journalPath        string
	intermediate       []byte
	configChange       *PreviewFileChange
	planningChanges    []PreviewFileChange
	barrierCollections []CollectionID
}

func NewForkTransaction(request ForkTransactionRequest) (*ForkTransaction, error) {
	if request.Preview == nil {
		return nil, errors.New("decisionview: fork transaction requires a preview")
	}
	if err := VerifyPreviewEnvelope(request.Preview, request.ApprovalDigest); err != nil {
		return nil, err
	}
	for _, change := range request.Preview.Changes {
		if change.Root == SourceRootPlanning && reservedForkStatePath(change.Path) {
			return nil, errors.New("decisionview: canonical ledger destination uses reserved fork-state storage")
		}
	}
	if err := request.OwnerID.Validate(); err != nil {
		return nil, err
	}
	if len(request.Collections) == 0 {
		return nil, errors.New("decisionview: fork transaction requires a collection identity")
	}
	request.Collections = append([]CollectionID(nil), request.Collections...)
	sort.Slice(request.Collections, func(i, j int) bool { return request.Collections[i] < request.Collections[j] })
	for i, id := range request.Collections {
		if err := id.Validate(); err != nil {
			return nil, err
		}
		if i > 0 && request.Collections[i-1] == id {
			return nil, errors.New("decisionview: duplicate transaction collection")
		}
	}
	return &ForkTransaction{request: request}, nil
}

func reservedForkStatePath(relative string) bool {
	parts := strings.Split(strings.ReplaceAll(relative, "\\", "/"), "/")
	return len(parts) >= 2 && strings.EqualFold(parts[0], "Decisions") && strings.EqualFold(parts[1], ".fork-state")
}

func (t *ForkTransaction) Apply(ctx context.Context) (*ForkTransactionResult, error) {
	if ctx == nil {
		return nil, errors.New("decisionview: fork transaction context required")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := t.open(); err != nil {
		return nil, err
	}
	defer t.close()
	// Discover the complete semantic source graph before selecting the lock set.
	// Nothing is published from this first pass; the graph is captured again
	// under all target, source and ancestor locks below.
	if err := t.revalidateSemanticPreview(); err != nil {
		return nil, err
	}
	if err := t.acquireLocks(ctx); err != nil {
		return nil, err
	}
	if err := t.checkBarriers(); err != nil {
		return nil, err
	}
	if err := t.refuseExistingJournal(); err != nil {
		return nil, err
	}
	if err := t.revalidateSemanticPreview(); err != nil {
		return nil, err
	}
	if err := t.checkBarriers(); err != nil {
		return nil, err
	}
	if err := t.checkAllPreconditions(); err != nil {
		return nil, err
	}
	if t.recheckSources == nil {
		t.recheckSources = t.checkSources
	}
	if t.afterInitialCheck != nil {
		t.afterInitialCheck()
	}
	if err := t.recheckPublicationInputs(); err != nil {
		return &ForkTransactionResult{OperationID: t.request.Preview.OperationID, Outcome: ForkTransactionRolledBack}, err
	}
	journal, err := NewForkJournal(t.request.OwnerID, t.request.Collections, t.request.Preview)
	if err != nil {
		return nil, err
	}
	t.journal = journal
	t.journalPath = forkJournalPath(journal.Collections[0], journal.OperationID)
	if t.configChange == nil {
		return nil, errors.New("decisionview: selector capture requires a prepared config change")
	}
	capture, err := newForkSelectorCapture(&journal.Preview, *t.configChange, t.intermediate, t.journalPath)
	if err != nil {
		return nil, err
	}
	t.journal.SelectorCapture = capture
	if err := t.writeJournal(false); err != nil {
		return nil, err
	}
	if err := t.inject(ForkTransactionAfterJournal); err != nil {
		return t.fail(err)
	}
	if err := t.writeBarriers("pending"); err != nil {
		return t.requireRecovery(err)
	}
	if err := t.inject(ForkTransactionAfterBarriers); err != nil {
		return t.fail(err)
	}
	if t.configChange != nil {
		if err := t.recheckPublicationInputs(); err != nil {
			return t.fail(err)
		}
		if err := t.publishChange(*t.configChange, t.intermediate, []byte(t.configChange.Before), t.configChange.BeforeExists); err != nil {
			return t.fail(err)
		}
		if err := t.inject(ForkTransactionAfterIntermediateConfig); err != nil {
			return t.fail(err)
		}
	}
	for _, change := range t.planningChanges {
		if err := t.inject(ForkTransactionBeforePlanningPublish); err != nil {
			return t.fail(err)
		}
		if err := t.recheckPublicationInputs(); err != nil {
			return t.fail(err)
		}
		if err := t.publishChange(change, []byte(change.After), []byte(change.Before), change.BeforeExists); err != nil {
			return t.fail(err)
		}
		if err := t.inject(ForkTransactionAfterPlanningPublish); err != nil {
			return t.fail(err)
		}
	}
	if t.configChange != nil {
		if err := t.recheckPublicationInputs(); err != nil {
			return t.fail(err)
		}
		if err := t.publishChange(*t.configChange, []byte(t.configChange.After), t.intermediate, true); err != nil {
			return t.fail(err)
		}
		if err := t.inject(ForkTransactionAfterFinalConfig); err != nil {
			return t.fail(err)
		}
	}
	if err := t.checkBarriers(); err != nil {
		return t.fail(err)
	}
	if err := t.checkSources(); err != nil {
		return t.fail(err)
	}
	if err := t.checkAfterBytes(); err != nil {
		return t.requireRecovery(err)
	}
	t.journal.Status = "committed"
	if err := t.writeJournal(true); err != nil {
		return t.requireRecovery(err)
	}
	if err := t.inject(ForkTransactionAfterCommitRecord); err != nil {
		return &ForkTransactionResult{OperationID: t.journal.OperationID, Outcome: ForkTransactionRecoveryRequired}, err
	}
	if err := t.writeBarriers("committed"); err != nil {
		return &ForkTransactionResult{OperationID: t.journal.OperationID, Outcome: ForkTransactionRecoveryRequired}, err
	}
	result := &ForkTransactionResult{OperationID: t.journal.OperationID, Outcome: ForkTransactionCommitted}
	if err := t.inject(ForkTransactionAfterBarrierRelease); err != nil {
		return result, err
	}
	return result, nil
}

func (t *ForkTransaction) refuseExistingJournal() error {
	existing, err := t.store.LoadForkJournal(t.request.Preview.OperationID)
	if err != nil {
		return fmt.Errorf("decisionview: existing transaction journal prevents operation reuse: %w", err)
	}
	if existing != nil {
		return errors.New("decisionview: transaction operation identity already has a journal; explicit recovery is required")
	}
	return nil
}

func (t *ForkTransaction) open() error {
	repository, err := canonicalTransactionRoot(t.request.Repository)
	if err != nil {
		return fmt.Errorf("decisionview: repository root: %w", err)
	}
	planning, err := canonicalTransactionRoot(t.request.Planning)
	if err != nil {
		return fmt.Errorf("decisionview: planning root: %w", err)
	}
	t.request.Repository, t.request.Planning = repository, planning
	t.store, err = OpenLocalStore(repository, planning, "transaction-only")
	if err != nil {
		return err
	}
	t.repositoryRoot, err = os.OpenRoot(repository)
	if err != nil {
		t.store.Close()
		t.store = nil
		return err
	}
	return nil
}

func canonicalTransactionRoot(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("transaction root is not a directory")
	}
	return resolved, nil
}

func samePath(a, b string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func (t *ForkTransaction) close() {
	for i := len(t.held) - 1; i >= 0; i-- {
		_ = unlockLocalFile(t.held[i])
		_ = t.held[i].Close()
	}
	if t.repositoryRoot != nil {
		_ = t.repositoryRoot.Close()
	}
	if t.store != nil {
		_ = t.store.Close()
	}
}

func (t *ForkTransaction) acquireLocks(ctx context.Context) error {
	keys := make([]string, 0, len(t.request.Collections)+len(t.barrierCollections)+1)
	seen := map[string]bool{}
	for _, id := range t.request.Collections {
		key := string(id)
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	for _, id := range t.barrierCollections {
		key := string(id)
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	keys = append(keys, "config:"+string(t.request.OwnerID))
	sort.Strings(keys)
	for _, key := range keys {
		dir := fmt.Sprintf("Decisions/.fork-state/%x", sha256.Sum256([]byte(key)))
		if err := t.store.root.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		f, err := t.store.root.OpenFile(dir+"/writer.lock", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				f.Close()
				return err
			}
			if err := lockLocalFile(f); err == nil {
				break
			} else if !localLockBusy(err) {
				f.Close()
				return err
			}
			select {
			case <-ctx.Done():
				f.Close()
				return ctx.Err()
			case <-time.After(5 * time.Millisecond):
			}
		}
		t.held = append(t.held, f)
	}
	return nil
}

func (t *ForkTransaction) checkBarriers() error {
	ids := append([]CollectionID(nil), t.request.Collections...)
	ids = append(ids, t.barrierCollections...)
	seen := map[CollectionID]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		barrier, err := t.store.InspectForkBarrier(id)
		if err != nil {
			return err
		}
		if barrier != nil && barrier.Status != "committed" && barrier.Status != "rolled-back" && (barrier.OperationID != t.request.Preview.OperationID || barrier.OwnerID != t.request.OwnerID) {
			return ErrForkBarrierPending
		}
	}
	return nil
}

func (t *ForkTransaction) revalidateSemanticPreview() error {
	t.configChange = nil
	t.planningChanges = nil
	t.intermediate = nil
	t.barrierCollections = nil
	config, _, err := readCollectionFile(t.repositoryRoot, "planning-config.json")
	if err != nil {
		return err
	}
	if err := t.validateDeclaredPlanningRoot(config); err != nil {
		return err
	}
	var regenerated *PreviewEnvelope
	var local *Collection
	collections := map[CollectionID]*Collection{}
	switch t.request.Preview.Operation {
	case "adopt", "rebind":
		var proposal forkAdoptionRequest
		if err := modelStrictJSON(t.request.Preview.Request, &proposal); err != nil {
			return fmt.Errorf("decisionview: adoption transaction request: %w", err)
		}
		if proposal.Operation != t.request.Preview.Operation || proposal.OperationID != t.request.Preview.OperationID || proposal.RepositoryID != t.request.OwnerID {
			return errors.New("decisionview: transaction owner or operation does not match its semantic proposal")
		}
		if proposal.Operation == "rebind" {
			local, err = LoadCollection(Roots{Repository: t.request.Repository, Planning: t.request.Planning}, proposal.LedgerID, SourceLocator{Root: SourceRootPlanning, Path: proposal.Path})
			if err != nil {
				return err
			}
			if err := t.loadCollectionGraph(local, collections); err != nil {
				return err
			}
		}
		sourceID, err := adoptionPreviewSourceID(t.request.Preview, proposal)
		if err != nil {
			return err
		}
		source, err := LoadCollection(Roots{Repository: t.request.Repository, Planning: t.request.Planning}, sourceID, proposal.Source)
		if err != nil {
			return err
		}
		if err := t.loadCollectionGraph(source, collections); err != nil {
			return err
		}
		preview, err := PreviewForkAdoption(ForkAdoptionSnapshot{ConfigBefore: config, Local: local, Source: source, Collections: collections}, t.request.Preview.Request)
		if err != nil {
			return err
		}
		regenerated = preview.Envelope
	case "detach":
		var proposal struct {
			Operation   string `json:"operation"`
			OperationID string `json:"operationId"`
		}
		if err := json.Unmarshal(t.request.Preview.Request, &proposal); err != nil || proposal.Operation != "detach" || proposal.OperationID != t.request.Preview.OperationID {
			return errors.New("decisionview: detachment operation does not match its semantic proposal")
		}
		selection, err := selectedForkConfig(config)
		if err != nil {
			return err
		}
		local, err = LoadCollection(Roots{Repository: t.request.Repository, Planning: t.request.Planning}, selection.LedgerID, SourceLocator{Root: SourceRootPlanning, Path: selection.Path})
		if err != nil {
			return err
		}
		if local.Metadata == nil || local.Metadata.RepositoryID != t.request.OwnerID {
			return errors.New("decisionview: detachment owner differs from selected local authority")
		}
		if err := t.loadCollectionGraph(local, collections); err != nil {
			return err
		}
		preview, err := PreviewForkRestoration(ForkRestorationSnapshot{ConfigBefore: config, Local: local, Collections: collections}, t.request.Preview.Request)
		if err != nil {
			return err
		}
		regenerated = preview.Envelope
	default:
		return fmt.Errorf("decisionview: transaction operation %q is not supported", t.request.Preview.Operation)
	}
	if !reflect.DeepEqual(regenerated, t.request.Preview) {
		return errors.New("decisionview: approved envelope does not match the current semantic preview")
	}
	for id := range collections {
		t.barrierCollections = append(t.barrierCollections, id)
	}
	sort.Slice(t.barrierCollections, func(i, j int) bool { return t.barrierCollections[i] < t.barrierCollections[j] })
	if err := t.validateCollectionExclusion(local, collections); err != nil {
		return err
	}
	return t.prepareChanges(config)
}

func selectedForkConfig(config []byte) (*ForkConfig, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(config, &fields); err != nil {
		return nil, err
	}
	selection, err := DecodeForkConfig(fields["decisionLog"])
	if err != nil {
		return nil, err
	}
	if selection.Mode != "fork" || selection.Transaction != nil {
		return nil, errors.New("decisionview: operation requires a complete selected fork")
	}
	return selection, nil
}

func (t *ForkTransaction) loadCollectionGraph(collection *Collection, found map[CollectionID]*Collection) error {
	if collection == nil {
		return errors.New("decisionview: nil collection in transaction authority graph")
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
		next, err := LoadCollection(Roots{Repository: t.request.Repository, Planning: t.request.Planning}, binding.CollectionID, binding.Source)
		if err != nil {
			return err
		}
		if err := t.loadCollectionGraph(next, found); err != nil {
			return err
		}
	}
	return nil
}

func (t *ForkTransaction) validateCollectionExclusion(local *Collection, collections map[CollectionID]*Collection) error {
	expected := map[CollectionID]bool{}
	if t.request.Preview.Operation == "adopt" {
		var proposal forkAdoptionRequest
		if err := json.Unmarshal(t.request.Preview.Request, &proposal); err != nil {
			return err
		}
		expected[proposal.LedgerID] = true
	} else {
		if local == nil {
			return errors.New("decisionview: selected local collection required")
		}
		for id := range collections {
			expected[id] = true
		}
	}
	if len(expected) != len(t.request.Collections) {
		return errors.New("decisionview: transaction collection exclusion set is incomplete or excessive")
	}
	for _, id := range t.request.Collections {
		if !expected[id] {
			return errors.New("decisionview: transaction collection exclusion set does not match semantic authority")
		}
	}
	return nil
}

func (t *ForkTransaction) prepareChanges(config []byte) error {
	for i := range t.request.Preview.Changes {
		change := t.request.Preview.Changes[i]
		switch change.Root {
		case SourceRootRepository:
			if change.Path != "planning-config.json" || t.configChange != nil {
				return errors.New("decisionview: selection transaction may only change the represented planning config")
			}
			t.configChange = &t.request.Preview.Changes[i]
		case SourceRootPlanning:
			t.planningChanges = append(t.planningChanges, change)
		default:
			return errors.New("decisionview: unsupported transaction destination root")
		}
	}
	if len(t.planningChanges) != 1 {
		return errors.New("decisionview: selection transaction requires exactly one local-ledger change")
	}
	if t.request.Preview.Operation == "rebind" {
		if t.configChange != nil {
			return errors.New("decisionview: rebinding cannot permanently change the selector")
		}
		t.configChange = &PreviewFileChange{Root: SourceRootRepository, Path: "planning-config.json", BeforeExists: true, Before: string(config), After: string(config)}
	} else if t.configChange == nil {
		return errors.New("decisionview: adoption/detachment requires an approved config change")
	}
	intermediate, err := transactionPendingConfig([]byte(t.configChange.After), t.request.Preview.OperationID, forkJournalPath(t.request.Collections[0], t.request.Preview.OperationID))
	if err != nil {
		return err
	}
	t.intermediate = intermediate
	return t.rejectFileAliases()
}

func adoptionPreviewSourceID(preview *PreviewEnvelope, proposal forkAdoptionRequest) (CollectionID, error) {
	for _, change := range preview.Changes {
		if change.Root != SourceRootPlanning || change.Path != proposal.Path {
			continue
		}
		nodes, err := collectionFrontmatter([]byte(change.After))
		if err != nil || nodes["fork"] == nil {
			return "", errors.New("decisionview: adoption preview local bytes lack valid fork metadata")
		}
		var metadata ForkMetadata
		if err := metadata.UnmarshalYAML(nodes["fork"]); err != nil {
			return "", err
		}
		if metadata.LedgerID != proposal.LedgerID || metadata.RepositoryID != proposal.RepositoryID {
			return "", errors.New("decisionview: adoption preview local metadata has mismatched ownership")
		}
		for _, binding := range metadata.Bindings {
			if binding.ID == metadata.ParentBindingID && binding.OwnerID == proposal.SourceOwnerID && reflect.DeepEqual(binding.Source, proposal.Source) {
				return binding.CollectionID, nil
			}
		}
		return "", errors.New("decisionview: adoption preview does not bind the proposed source")
	}
	return "", errors.New("decisionview: adoption preview omits its local authority bytes")
}

func (t *ForkTransaction) validateDeclaredPlanningRoot(config []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(config, &fields); err != nil {
		return err
	}
	var declared string
	if err := json.Unmarshal(fields["planningRoot"], &declared); err != nil || declared == "" {
		return errors.New("decisionview: planningRoot must explicitly identify transaction storage")
	}
	if !filepath.IsAbs(declared) {
		declared = filepath.Join(t.request.Repository, declared)
	}
	resolved, err := filepath.EvalSymlinks(declared)
	if err != nil {
		return err
	}
	if !samePath(resolved, t.request.Planning) {
		return errors.New("decisionview: supplied planning root differs from the represented configuration")
	}
	return nil
}

func (t *ForkTransaction) rejectFileAliases() error {
	type namedInfo struct {
		name string
		info os.FileInfo
	}
	var seen []namedInfo
	for _, locator := range append(transactionSourceLocators(t.request.Preview), transactionChangeLocators(t.request.Preview)...) {
		root := t.store.root
		if locator.Root == SourceRootRepository {
			root = t.repositoryRoot
		}
		_, info, err := readCollectionFile(root, locator.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		name := string(locator.Root) + ":" + locator.Path
		for _, previous := range seen {
			if os.SameFile(previous.info, info) && previous.name != name {
				return fmt.Errorf("decisionview: transaction locators %s and %s alias the same file", previous.name, name)
			}
		}
		seen = append(seen, namedInfo{name, info})
	}
	return nil
}

func transactionSourceLocators(preview *PreviewEnvelope) []SourceLocator {
	out := make([]SourceLocator, 0, len(preview.Sources))
	for _, source := range preview.Sources {
		out = append(out, SourceLocator{Root: source.Root, Path: source.Path})
	}
	return out
}

func transactionChangeLocators(preview *PreviewEnvelope) []SourceLocator {
	out := make([]SourceLocator, 0, len(preview.Changes))
	for _, change := range preview.Changes {
		out = append(out, SourceLocator{Root: change.Root, Path: change.Path})
	}
	return out
}

func transactionPendingConfig(final []byte, operation, journal string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(final, &fields); err != nil {
		return nil, err
	}
	selection, err := DecodeForkConfig(fields["decisionLog"])
	if err != nil {
		return nil, err
	}
	selection.Transaction = &TransactionRef{ID: operation, Journal: journal}
	raw, err := json.Marshal(selection)
	if err != nil {
		return nil, err
	}
	fields["decisionLog"] = raw
	return json.Marshal(fields)
}

func (t *ForkTransaction) checkAllPreconditions() error {
	if err := t.checkSources(); err != nil {
		return err
	}
	for _, change := range t.request.Preview.Changes {
		snapshot, err := t.read(change.Root, change.Path)
		if err != nil {
			return err
		}
		if snapshot.Exists != change.BeforeExists || !bytes.Equal(snapshot.Bytes, []byte(change.Before)) {
			return ErrForkTransactionConflict
		}
	}
	return nil
}

func (t *ForkTransaction) checkSources() error {
	for _, source := range t.request.Preview.Sources {
		snapshot, err := t.read(source.Root, source.Path)
		if err != nil {
			return err
		}
		ownedSelectorBarrier := source.Root == SourceRootRepository && source.Path == "planning-config.json" && bytes.Equal(snapshot.Bytes, t.intermediate)
		if !snapshot.Exists || (snapshot.Digest != source.Digest && !ownedSelectorBarrier) {
			return ErrForkTransactionConflict
		}
	}
	return nil
}

func (t *ForkTransaction) recheckPublicationInputs() error {
	if err := t.checkBarriers(); err != nil {
		return err
	}
	return t.recheckSources()
}

func (t *ForkTransaction) checkAfterBytes() error {
	for _, change := range t.request.Preview.Changes {
		snapshot, err := t.read(change.Root, change.Path)
		if err != nil {
			return err
		}
		if !snapshot.Exists || !bytes.Equal(snapshot.Bytes, []byte(change.After)) {
			return ErrForkTransactionConflict
		}
	}
	return nil
}

func (t *ForkTransaction) read(root SourceRoot, relative string) (LocalSnapshot, error) {
	if err := validateRelativeLocator(relative); err != nil {
		return LocalSnapshot{}, err
	}
	r := t.store.root
	if root == SourceRootRepository {
		r = t.repositoryRoot
	} else if root != SourceRootPlanning {
		return LocalSnapshot{}, errors.New("decisionview: unsupported transaction root")
	}
	raw, _, err := readCollectionFile(r, relative)
	if os.IsNotExist(err) {
		return LocalSnapshot{}, nil
	}
	if err != nil {
		return LocalSnapshot{}, err
	}
	return LocalSnapshot{Exists: true, Bytes: raw, Digest: collectionDigest(raw)}, nil
}

func (t *ForkTransaction) publishChange(change PreviewFileChange, content, expected []byte, expectedExists bool) error {
	current, err := t.read(change.Root, change.Path)
	if err != nil {
		return err
	}
	if current.Exists != expectedExists || !bytes.Equal(current.Bytes, expected) {
		return ErrForkTransactionConflict
	}
	return publishForkTransactionFile(change.Root, t.rootFor(change.Root), change.Path, content, current.Exists)
}

func publishForkTransactionFile(rootKind SourceRoot, root *os.Root, relative string, content []byte, existing bool) error {
	if rootKind == SourceRootRepository && relative == "planning-config.json" {
		if !existing {
			return errors.New("decisionview: represented planning config replacement requires an existing regular file")
		}
		return publishConfigTransactionFile(root, relative, content)
	}
	return publishTransactionFile(root, relative, content, existing)
}

func publishTransactionFile(root *os.Root, relative string, content []byte, existing bool) error {
	if len(content) > maxCollectionFileBytes {
		return errors.New("decisionview: transaction content exceeds file limit")
	}
	parentName := path.Dir(relative)
	if err := root.MkdirAll(parentName, 0o755); err != nil {
		return err
	}
	parent, err := root.OpenRoot(parentName)
	if err != nil {
		return err
	}
	defer parent.Close()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	base := path.Base(relative)
	temp := fmt.Sprintf(".%s.transaction-%x", base, nonce)
	f, err := parent.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer parent.Remove(temp)
	mode := os.FileMode(0o644)
	if existing {
		info, err := parent.Lstat(base)
		if err != nil {
			f.Close()
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			f.Close()
			return errors.New("decisionview: transaction target is not a regular file")
		}
		mode = info.Mode().Perm()
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return publishLocalFile(parent, temp, base, existing)
}

func (t *ForkTransaction) inject(point ForkTransactionPoint) error {
	if t.failpoint == nil {
		return nil
	}
	return t.failpoint(point)
}

func (t *ForkTransaction) writeJournal(existing bool) error {
	raw, err := json.Marshal(t.journal)
	if err != nil {
		return err
	}
	return t.store.writePrivateJournalFile(t.journalPath, raw, existing)
}

func (t *ForkTransaction) writeBarriers(status string) error {
	for _, id := range t.request.Collections {
		barrier := ForkBarrier{Version: Version1, OperationID: t.journal.OperationID, OwnerID: t.request.OwnerID, Collection: id, Journal: t.journalPath, Status: status}
		raw, err := json.Marshal(barrier)
		if err != nil {
			return err
		}
		_, statErr := t.store.root.Stat(forkBarrierPath(id))
		existing := statErr == nil
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if err := t.store.writePrivateJournalFile(forkBarrierPath(id), raw, existing); err != nil {
			return err
		}
	}
	return nil
}

func (t *ForkTransaction) rollbackChanges() []PreviewFileChange {
	changes := append([]PreviewFileChange(nil), t.request.Preview.Changes...)
	if t.request.Preview.Operation == "rebind" && t.configChange != nil {
		changes = append(changes, *t.configChange)
	}
	return changes
}

func (t *ForkTransaction) fail(cause error) (*ForkTransactionResult, error) {
	if t.journal != nil && t.journal.Status == "committed" {
		return &ForkTransactionResult{OperationID: t.journal.OperationID, Outcome: ForkTransactionRecoveryRequired}, cause
	}
	clean := true
	changes := t.rollbackChanges()
	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		current, err := t.read(change.Root, change.Path)
		if err != nil {
			clean = false
			continue
		}
		owned := bytes.Equal(current.Bytes, []byte(change.After))
		if t.configChange != nil && change.Root == SourceRootRepository && change.Path == t.configChange.Path {
			owned = owned || bytes.Equal(current.Bytes, t.intermediate)
		}
		if current.Exists == change.BeforeExists && bytes.Equal(current.Bytes, []byte(change.Before)) {
			continue
		}
		if !owned {
			clean = false
			continue
		}
		if change.BeforeExists {
			if err := publishForkTransactionFile(change.Root, t.rootFor(change.Root), change.Path, []byte(change.Before), true); err != nil {
				clean = false
			}
		} else if err := removeTransactionFile(t.rootFor(change.Root), change.Path); err != nil {
			clean = false
		}
	}
	if !clean {
		return t.requireRecovery(cause)
	}
	t.journal.Status = "rolled-back"
	if err := t.writeJournal(true); err != nil {
		return t.requireRecovery(errors.Join(cause, err))
	}
	if err := t.writeBarriers("rolled-back"); err != nil {
		return t.requireRecovery(errors.Join(cause, err))
	}
	return &ForkTransactionResult{OperationID: t.journal.OperationID, Outcome: ForkTransactionRolledBack}, cause
}

func (t *ForkTransaction) requireRecovery(cause error) (*ForkTransactionResult, error) {
	if t.journal != nil && t.journal.Status != "committed" {
		t.journal.Status = "recovery-required"
		if err := t.writeJournal(true); err != nil {
			cause = errors.Join(cause, err)
		} else if err := t.writeBarriers("recovery-required"); err != nil {
			cause = errors.Join(cause, err)
		}
	}
	return &ForkTransactionResult{OperationID: t.request.Preview.OperationID, Outcome: ForkTransactionRecoveryRequired}, cause
}

func (t *ForkTransaction) rootFor(root SourceRoot) *os.Root {
	if root == SourceRootRepository {
		return t.repositoryRoot
	}
	return t.store.root
}

func removeTransactionFile(root *os.Root, relative string) error {
	parent, err := root.OpenRoot(path.Dir(relative))
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := parent.Remove(path.Base(relative)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func InspectForkTransaction(repository, planning, operation string, collection CollectionID) (*ForkTransactionInspection, error) {
	if !journalOperationRe.MatchString(operation) {
		return nil, errors.New("decisionview: invalid transaction operation ID")
	}
	if err := collection.Validate(); err != nil {
		return nil, err
	}
	store, err := OpenLocalStore(repository, planning, "transaction-inspection")
	if err != nil {
		return nil, err
	}
	defer store.Close()
	journal, err := store.LoadForkJournal(operation)
	if err != nil {
		return nil, err
	}
	if journal == nil {
		return nil, nil
	}
	member := false
	for _, id := range journal.Collections {
		member = member || id == collection
	}
	if !member {
		return nil, errors.New("decisionview: transaction journal does not cover requested collection")
	}
	barrier, err := store.InspectForkBarrier(collection)
	if err != nil {
		return nil, err
	}
	inspection := &ForkTransactionInspection{OperationID: operation, Outcome: ForkTransactionRecoveryRequired, Authority: ResolutionRecoveryNeeded}
	if journal.Status == "rolled-back" && barrier != nil && barrier.Status == "rolled-back" {
		inspection.Outcome, inspection.Authority = ForkTransactionRolledBack, ResolutionComplete
	} else if journal.Status == "committed" && barrier != nil && barrier.Status == "committed" {
		inspection.Outcome, inspection.Authority = ForkTransactionCommitted, ResolutionComplete
	}
	return inspection, nil
}
