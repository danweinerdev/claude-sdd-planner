package decisionview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"time"
)

var ErrForkRecoveryConflict = errors.New("decisionview: fork recovery state changed or is not operation-owned")

type forkRecoveryPoint string

const (
	forkRecoveryAfterIntermediateConfig forkRecoveryPoint = "after-intermediate-config"
	forkRecoveryAfterLedgerPublication  forkRecoveryPoint = "after-ledger-publication"
	forkRecoveryAfterFinalConfig        forkRecoveryPoint = "after-final-config"
	forkRecoveryAfterJournalWrite       forkRecoveryPoint = "after-journal-write"
	forkRecoveryAfterBarrierPublication forkRecoveryPoint = "after-barrier-publication"
)

var forkRecoveryFailpoint func(forkRecoveryPoint) error

type ForkRecoveryAction string

const (
	ForkRecoveryFinish         ForkRecoveryAction = "finish"
	ForkRecoveryRollback       ForkRecoveryAction = "rollback"
	ForkRecoveryDiscardStaging ForkRecoveryAction = "discard-staging"
)

type ForkRecoveryInspection struct {
	OperationID string
	Outcome     ForkTransactionOutcome
	Authority   Resolution
	Journal     string
	Barriers    map[CollectionID]string
}

type ForkRecoveryPreview struct {
	Version            SchemaVersion       `json:"version"`
	OperationID        string              `json:"operationId"`
	Action             ForkRecoveryAction  `json:"action"`
	Changes            []PreviewFileChange `json:"changes"`
	CurrentStateDigest string              `json:"currentStateDigest"`
	Digest             string              `json:"digest"`
}

type ForkRecoveryRequest struct {
	Repository     string
	Planning       string
	Preview        *ForkRecoveryPreview
	ApprovalDigest string
}

type forkRecoveryTarget struct {
	change PreviewFileChange
	before []byte
	after  []byte
}

type forkRecoverySession struct {
	repositoryRoot *os.Root
	store          *LocalStore
	journal        *ForkJournal
	capture        *ForkSelectorCapture
	targets        []forkRecoveryTarget
	barriers       map[CollectionID]*ForkBarrier
}

type forkRecoveryStateFile struct {
	Root   SourceRoot `json:"root"`
	Path   string     `json:"path"`
	Exists bool       `json:"exists"`
	Digest string     `json:"digest"`
}

type forkRecoveryStateBarrier struct {
	Collection CollectionID `json:"collectionId"`
	Exists     bool         `json:"exists"`
	Status     string       `json:"status,omitempty"`
	Digest     string       `json:"digest,omitempty"`
}

type forkRecoveryState struct {
	JournalDigest string                     `json:"journalDigest"`
	Barriers      []forkRecoveryStateBarrier `json:"barriers"`
	Files         []forkRecoveryStateFile    `json:"files"`
}

func InspectForkRecovery(repository, planning, operation string) (*ForkRecoveryInspection, error) {
	session, err := openForkRecoverySession(repository, planning, operation)
	if err != nil || session == nil {
		return nil, err
	}
	defer session.close()
	if _, err := session.currentState(); err != nil {
		return nil, err
	}
	inspection := &ForkRecoveryInspection{
		OperationID: operation,
		Outcome:     ForkTransactionRecoveryRequired,
		Authority:   ResolutionRecoveryNeeded,
		Journal:     forkJournalPath(session.journal.Collections[0], operation),
		Barriers:    map[CollectionID]string{},
	}
	for _, id := range session.journal.Collections {
		if barrier := session.barriers[id]; barrier != nil {
			inspection.Barriers[id] = barrier.Status
		} else {
			inspection.Barriers[id] = "missing"
		}
	}
	if session.journal.Status == "committed" && session.allBarriers("committed") {
		inspection.Outcome, inspection.Authority = ForkTransactionCommitted, ResolutionComplete
	} else if session.journal.Status == "rolled-back" && session.allBarriers("rolled-back") {
		inspection.Outcome, inspection.Authority = ForkTransactionRolledBack, ResolutionComplete
	}
	return inspection, nil
}

func PreviewForkRecovery(repository, planning, operation string, action ForkRecoveryAction) (*ForkRecoveryPreview, error) {
	session, err := openForkRecoverySession(repository, planning, operation)
	if err != nil || session == nil {
		return nil, err
	}
	defer session.close()
	return session.preview(action)
}

func RecoverForkTransaction(ctx context.Context, request ForkRecoveryRequest) (*ForkTransactionResult, error) {
	if ctx == nil {
		return nil, errors.New("decisionview: fork recovery context required")
	}
	if err := verifyForkRecoveryPreview(request.Preview, request.ApprovalDigest); err != nil {
		return nil, err
	}

	initial, err := loadForkRecoveryJournal(request.Repository, request.Planning, request.Preview.OperationID)
	if err != nil || initial == nil {
		return nil, err
	}
	tx := &ForkTransaction{request: ForkTransactionRequest{
		Repository:  request.Repository,
		Planning:    request.Planning,
		OwnerID:     initial.OwnerID,
		Collections: append([]CollectionID(nil), initial.Collections...),
		Preview:     &initial.Preview,
	}}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := tx.open(); err != nil {
		return nil, err
	}
	defer tx.close()
	if err := tx.acquireLocks(ctx); err != nil {
		return nil, err
	}

	session, err := loadForkRecoverySession(tx.repositoryRoot, tx.store, request.Preview.OperationID)
	if err != nil {
		return nil, err
	}
	if session.journal.OwnerID != initial.OwnerID || !reflect.DeepEqual(session.journal.Collections, initial.Collections) {
		return nil, ErrForkRecoveryConflict
	}
	fresh, err := session.preview(request.Preview.Action)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(fresh, request.Preview) {
		return nil, ErrForkRecoveryConflict
	}

	switch fresh.Action {
	case ForkRecoveryFinish:
		return session.finish()
	case ForkRecoveryRollback:
		return session.rollback()
	case ForkRecoveryDiscardStaging:
		return session.discardStaging()
	default:
		return nil, errors.New("decisionview: unsupported fork recovery action")
	}
}

func loadForkRecoveryJournal(repository, planning, operation string) (*ForkJournal, error) {
	if !journalOperationRe.MatchString(operation) {
		return nil, errors.New("decisionview: invalid fork recovery operation ID")
	}
	store, err := OpenLocalStore(repository, planning, "fork-recovery-journal")
	if err != nil {
		return nil, err
	}
	defer store.Close()
	journal, err := store.LoadForkJournal(operation)
	if err != nil {
		return nil, err
	}
	if journal == nil {
		return nil, errors.New("decisionview: fork recovery journal not found")
	}
	if _, err := store.LoadForkSelectorCapture(operation); err != nil {
		return nil, err
	}
	return journal, nil
}

func openForkRecoverySession(repository, planning, operation string) (*forkRecoverySession, error) {
	if !journalOperationRe.MatchString(operation) {
		return nil, errors.New("decisionview: invalid fork recovery operation ID")
	}
	repositoryName, err := canonicalTransactionRoot(repository)
	if err != nil {
		return nil, err
	}
	planningName, err := canonicalTransactionRoot(planning)
	if err != nil {
		return nil, err
	}
	store, err := OpenLocalStore(repositoryName, planningName, "fork-recovery-inspection")
	if err != nil {
		return nil, err
	}
	repositoryRoot, err := os.OpenRoot(repositoryName)
	if err != nil {
		store.Close()
		return nil, err
	}
	session, err := loadForkRecoverySession(repositoryRoot, store, operation)
	if err != nil {
		repositoryRoot.Close()
		store.Close()
		return nil, err
	}
	return session, nil
}

func loadForkRecoverySession(repositoryRoot *os.Root, store *LocalStore, operation string) (*forkRecoverySession, error) {
	journal, err := store.LoadForkJournal(operation)
	if err != nil {
		return nil, err
	}
	if journal == nil {
		return nil, errors.New("decisionview: fork recovery journal not found")
	}
	capture, err := store.LoadForkSelectorCapture(operation)
	if err != nil {
		return nil, err
	}
	if err := validateRecoveryOwner(journal.OwnerID, capture); err != nil {
		return nil, err
	}
	session := &forkRecoverySession{
		repositoryRoot: repositoryRoot,
		store:          store,
		journal:        journal,
		capture:        capture,
		barriers:       map[CollectionID]*ForkBarrier{},
	}
	for _, change := range journal.Preview.Changes {
		target := forkRecoveryTarget{change: change, before: []byte(change.Before), after: []byte(change.After)}
		if change.Root == SourceRootRepository && change.Path == "planning-config.json" {
			target.before = []byte(capture.Original)
			target.after = []byte(capture.Final)
		}
		session.targets = append(session.targets, target)
	}
	if journal.Preview.Operation == "rebind" {
		session.targets = append(session.targets, forkRecoveryTarget{
			change: PreviewFileChange{Root: SourceRootRepository, Path: "planning-config.json", BeforeExists: true, Before: capture.Original, After: capture.Final},
			before: []byte(capture.Original),
			after:  []byte(capture.Final),
		})
	}
	configTargets := 0
	for _, target := range session.targets {
		if target.change.Root == SourceRootRepository && target.change.Path == "planning-config.json" {
			configTargets++
		}
	}
	if configTargets != 1 {
		return nil, errors.New("decisionview: recovery journal lacks one exact selector target")
	}
	for _, id := range journal.Collections {
		barrier, err := store.InspectForkBarrier(id)
		if err != nil {
			return nil, err
		}
		if barrier != nil && (barrier.OperationID != journal.OperationID || barrier.OwnerID != journal.OwnerID) {
			return nil, fmt.Errorf("%w: collection %s is protected by foreign operation %s owner %s", ErrForkRecoveryConflict, id, barrier.OperationID, barrier.OwnerID)
		}
		session.barriers[id] = barrier
	}
	return session, nil
}

func (s *forkRecoverySession) close() {
	if s.repositoryRoot != nil {
		_ = s.repositoryRoot.Close()
	}
	if s.store != nil {
		_ = s.store.Close()
	}
}

func validateRecoveryOwner(owner OwnerID, capture *ForkSelectorCapture) error {
	if err := owner.Validate(); err != nil {
		return err
	}
	selectorFields := func(raw string) (map[string]json.RawMessage, error) {
		if err := selectionUniqueRootKeys([]byte(raw)); err != nil {
			return nil, ErrForkRecoveryConflict
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			return nil, ErrForkRecoveryConflict
		}
		return fields, nil
	}
	validateOwned := func(fields map[string]json.RawMessage) error {
		var found OwnerID
		if err := json.Unmarshal(fields["repositoryId"], &found); err != nil || found != owner {
			return errors.New("decisionview: recovery selector owner does not match its journal")
		}
		return nil
	}

	original, err := selectorFields(capture.Original)
	if err != nil {
		return err
	}
	if _, hasOwner := original["repositoryId"]; hasOwner {
		if err := validateOwned(original); err != nil {
			return err
		}
	} else if _, selected := original["decisionLog"]; selected {
		return errors.New("decisionview: recovery selector owner does not match its journal")
	}
	for _, raw := range []string{capture.Intermediate, capture.Final} {
		fields, err := selectorFields(raw)
		if err != nil {
			return err
		}
		if err := validateOwned(fields); err != nil {
			return err
		}
	}
	return nil
}

func (s *forkRecoverySession) read(root SourceRoot, relative string) (LocalSnapshot, error) {
	if err := validateRelativeLocator(relative); err != nil {
		return LocalSnapshot{}, err
	}
	r := s.store.root
	if root == SourceRootRepository {
		r = s.repositoryRoot
	} else if root != SourceRootPlanning {
		return LocalSnapshot{}, errors.New("decisionview: unsupported recovery root")
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

func (s *forkRecoverySession) currentState() (string, error) {
	journalRaw, err := json.Marshal(s.journal)
	if err != nil {
		return "", err
	}
	state := forkRecoveryState{JournalDigest: collectionDigest(journalRaw)}
	for _, id := range s.journal.Collections {
		item := forkRecoveryStateBarrier{Collection: id}
		if barrier := s.barriers[id]; barrier != nil {
			raw, err := json.Marshal(barrier)
			if err != nil {
				return "", err
			}
			item.Exists, item.Status, item.Digest = true, barrier.Status, collectionDigest(raw)
		}
		state.Barriers = append(state.Barriers, item)
	}

	locators := map[string]SourceLocator{}
	for _, source := range s.journal.Preview.Sources {
		locators[string(source.Root)+"\x00"+source.Path] = SourceLocator{Root: source.Root, Path: source.Path}
	}
	for _, target := range s.targets {
		locators[string(target.change.Root)+"\x00"+target.change.Path] = SourceLocator{Root: target.change.Root, Path: target.change.Path}
	}
	keys := make([]string, 0, len(locators))
	for key := range locators {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		locator := locators[key]
		snapshot, err := s.read(locator.Root, locator.Path)
		if err != nil {
			return "", err
		}
		state.Files = append(state.Files, forkRecoveryStateFile{Root: locator.Root, Path: locator.Path, Exists: snapshot.Exists, Digest: snapshot.Digest})
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return collectionDigest(raw), nil
}

func (s *forkRecoverySession) preview(action ForkRecoveryAction) (*ForkRecoveryPreview, error) {
	if action != ForkRecoveryFinish && action != ForkRecoveryRollback && action != ForkRecoveryDiscardStaging {
		return nil, errors.New("decisionview: unsupported fork recovery action")
	}
	if s.journal.Status == "rolled-back" && action == ForkRecoveryFinish {
		return nil, errors.New("decisionview: rolled-back recovery history cannot be finished")
	}
	if s.journal.Status == "committed" && action != ForkRecoveryFinish {
		return nil, errors.New("decisionview: committed recovery history cannot be rolled back or discarded")
	}
	if action == ForkRecoveryFinish && s.journal.Status != "committed" {
		if err := s.checkFinishSources(); err != nil {
			return nil, err
		}
	}

	changes := []PreviewFileChange{}
	for _, target := range s.targets {
		current, err := s.read(target.change.Root, target.change.Path)
		if err != nil {
			return nil, err
		}
		atBefore := current.Exists == target.change.BeforeExists && bytes.Equal(current.Bytes, target.before)
		atAfter := current.Exists && bytes.Equal(current.Bytes, target.after)
		atIntermediate := target.change.Root == SourceRootRepository && target.change.Path == "planning-config.json" && current.Exists && bytes.Equal(current.Bytes, []byte(s.capture.Intermediate))
		if !atBefore && !atAfter && !atIntermediate {
			return nil, ErrForkRecoveryConflict
		}
		if s.journal.Status == "committed" && !atAfter {
			return nil, fmt.Errorf("%w: committed journal target %s:%s does not equal exact approved after bytes", ErrForkRecoveryConflict, target.change.Root, target.change.Path)
		}
		if action == ForkRecoveryDiscardStaging && !atBefore {
			return nil, errors.New("decisionview: staged recovery has already activated authoritative bytes")
		}
		if s.journal.Status == "rolled-back" && !atBefore {
			return nil, errors.New("decisionview: rolled-back journal does not match exact old authoritative bytes")
		}
		wantExists, want := true, target.after
		if action != ForkRecoveryFinish {
			wantExists, want = target.change.BeforeExists, target.before
		}
		if current.Exists == wantExists && bytes.Equal(current.Bytes, want) {
			continue
		}
		changes = append(changes, PreviewFileChange{Root: target.change.Root, Path: target.change.Path, BeforeExists: current.Exists, Before: string(current.Bytes), After: string(want)})
	}
	stateDigest, err := s.currentState()
	if err != nil {
		return nil, err
	}
	preview := &ForkRecoveryPreview{Version: Version1, OperationID: s.journal.OperationID, Action: action, Changes: changes, CurrentStateDigest: stateDigest}
	digest, err := forkRecoveryPreviewDigest(preview)
	if err != nil {
		return nil, err
	}
	preview.Digest = digest
	return preview, nil
}

func (s *forkRecoverySession) checkFinishSources() error {
	changed := map[string]bool{}
	for _, target := range s.targets {
		changed[string(target.change.Root)+"\x00"+target.change.Path] = true
	}
	for _, source := range s.journal.Preview.Sources {
		current, err := s.read(source.Root, source.Path)
		if err != nil {
			return err
		}
		key := string(source.Root) + "\x00" + source.Path
		if changed[key] && source.Root == SourceRootRepository && source.Path == "planning-config.json" {
			owned := current.Exists && (bytes.Equal(current.Bytes, []byte(s.capture.Original)) || bytes.Equal(current.Bytes, []byte(s.capture.Intermediate)) || bytes.Equal(current.Bytes, []byte(s.capture.Final)))
			if !owned {
				return ErrForkRecoveryConflict
			}
			continue
		}
		if !current.Exists || current.Digest != source.Digest {
			return ErrForkRecoveryConflict
		}
	}
	return nil
}

func forkRecoveryPreviewDigest(preview *ForkRecoveryPreview) (string, error) {
	if preview == nil {
		return "", errors.New("decisionview: fork recovery preview required")
	}
	copy := *preview
	copy.Digest = ""
	raw, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	return collectionDigest(raw), nil
}

func verifyForkRecoveryPreview(preview *ForkRecoveryPreview, approval string) error {
	if preview == nil || preview.Version != Version1 || !journalOperationRe.MatchString(preview.OperationID) || preview.CurrentStateDigest == "" {
		return errors.New("decisionview: valid fork recovery preview required")
	}
	if approval == "" || approval != preview.Digest {
		return errors.New("decisionview: exact fork recovery approval digest required")
	}
	want, err := forkRecoveryPreviewDigest(preview)
	if err != nil {
		return err
	}
	if want != approval {
		return errors.New("decisionview: fork recovery request differs from approved bytes")
	}
	return nil
}

func (s *forkRecoverySession) finish() (*ForkTransactionResult, error) {
	if s.journal.Status == "committed" {
		if err := s.requireExactAfterAuthority(); err != nil {
			return nil, err
		}
		if s.allBarriers("committed") {
			return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionCommitted}, nil
		}
		if err := s.writeBarriers("committed"); err != nil {
			return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionRecoveryRequired}, err
		}
		return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionCommitted}, nil
	}
	// A recovery finish is a new publication attempt. Re-establish every owned
	// barrier before touching authority, even when the original transaction
	// crashed before publishing all of them.
	if err := s.writeBarriers("pending"); err != nil {
		return s.recoveryRequired(err)
	}
	if err := s.publishFinishTargets(); err != nil {
		return s.recoveryRequired(err)
	}
	s.journal.Status = "committed"
	if err := s.writeJournal(); err != nil {
		return s.recoveryRequired(err)
	}
	if err := s.writeBarriers("committed"); err != nil {
		return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionRecoveryRequired}, err
	}
	return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionCommitted}, nil
}

func (s *forkRecoverySession) requireExactAfterAuthority() error {
	for _, target := range s.targets {
		current, err := s.read(target.change.Root, target.change.Path)
		if err != nil {
			return err
		}
		if !current.Exists || !bytes.Equal(current.Bytes, target.after) {
			return fmt.Errorf("%w: committed journal target %s:%s does not equal exact approved after bytes", ErrForkRecoveryConflict, target.change.Root, target.change.Path)
		}
	}
	return nil
}

func (s *forkRecoverySession) rollback() (*ForkTransactionResult, error) {
	if s.journal.Status == "committed" {
		return nil, errors.New("decisionview: committed recovery history cannot be rolled back")
	}
	if s.journal.Status == "rolled-back" {
		if err := s.requireExactOldAuthority(); err != nil {
			return nil, err
		}
		if err := s.writeBarriers("rolled-back"); err != nil {
			return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionRecoveryRequired}, err
		}
		return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionRolledBack}, nil
	}
	if err := s.publishTargets(false); err != nil {
		return s.recoveryRequired(err)
	}
	s.journal.Status = "rolled-back"
	if err := s.writeJournal(); err != nil {
		return s.recoveryRequired(err)
	}
	if err := s.writeBarriers("rolled-back"); err != nil {
		return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionRecoveryRequired}, err
	}
	return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionRolledBack}, nil
}

func (s *forkRecoverySession) discardStaging() (*ForkTransactionResult, error) {
	if s.journal.Status == "committed" {
		return nil, errors.New("decisionview: committed recovery history cannot be discarded")
	}
	for _, target := range s.targets {
		current, err := s.read(target.change.Root, target.change.Path)
		if err != nil {
			return nil, err
		}
		if current.Exists != target.change.BeforeExists || !bytes.Equal(current.Bytes, target.before) {
			return nil, errors.New("decisionview: staged recovery has activated authoritative bytes")
		}
	}
	if s.journal.Status != "rolled-back" {
		s.journal.Status = "rolled-back"
		if err := s.writeJournal(); err != nil {
			return s.recoveryRequired(err)
		}
	}
	if err := s.writeBarriers("rolled-back"); err != nil {
		return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionRecoveryRequired}, err
	}
	return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionRolledBack}, nil
}

func (s *forkRecoverySession) requireExactOldAuthority() error {
	for _, target := range s.targets {
		current, err := s.read(target.change.Root, target.change.Path)
		if err != nil {
			return err
		}
		if current.Exists != target.change.BeforeExists || !bytes.Equal(current.Bytes, target.before) {
			return errors.New("decisionview: rolled-back journal does not match exact old authoritative bytes")
		}
	}
	return nil
}

func (s *forkRecoverySession) publishFinishTargets() error {
	var config *forkRecoveryTarget
	var ledgers []forkRecoveryTarget
	for i := range s.targets {
		target := &s.targets[i]
		if target.change.Root == SourceRootRepository && target.change.Path == "planning-config.json" {
			config = target
		} else {
			ledgers = append(ledgers, *target)
		}
	}
	if config == nil {
		return errors.New("decisionview: recovery finish lacks exact selector capture")
	}
	remainingLedger := false
	for _, target := range ledgers {
		current, err := s.read(target.change.Root, target.change.Path)
		if err != nil {
			return err
		}
		if !current.Exists || !bytes.Equal(current.Bytes, target.after) {
			remainingLedger = true
		}
	}
	if remainingLedger {
		current, err := s.read(config.change.Root, config.change.Path)
		if err != nil {
			return err
		}
		if !current.Exists || (!bytes.Equal(current.Bytes, config.before) && !bytes.Equal(current.Bytes, config.after) && !bytes.Equal(current.Bytes, []byte(s.capture.Intermediate))) {
			return ErrForkRecoveryConflict
		}
		if !bytes.Equal(current.Bytes, []byte(s.capture.Intermediate)) {
			if err := publishForkTransactionFile(SourceRootRepository, s.repositoryRoot, config.change.Path, []byte(s.capture.Intermediate), true); err != nil {
				return err
			}
			if err := s.inject(forkRecoveryAfterIntermediateConfig); err != nil {
				return err
			}
		}
	}
	for _, target := range ledgers {
		if err := s.publishOneTarget(target, true); err != nil {
			return err
		}
	}
	current, err := s.read(config.change.Root, config.change.Path)
	if err != nil {
		return err
	}
	if !current.Exists || (!bytes.Equal(current.Bytes, config.before) && !bytes.Equal(current.Bytes, config.after) && !bytes.Equal(current.Bytes, []byte(s.capture.Intermediate))) {
		return ErrForkRecoveryConflict
	}
	if !bytes.Equal(current.Bytes, config.after) {
		if err := publishForkTransactionFile(SourceRootRepository, s.repositoryRoot, config.change.Path, config.after, true); err != nil {
			return err
		}
		if err := s.inject(forkRecoveryAfterFinalConfig); err != nil {
			return err
		}
	}
	return nil
}

func (s *forkRecoverySession) publishTargets(after bool) error {
	targets := s.targets
	if !after {
		targets = append([]forkRecoveryTarget(nil), s.targets...)
		for left, right := 0, len(targets)-1; left < right; left, right = left+1, right-1 {
			targets[left], targets[right] = targets[right], targets[left]
		}
	}
	for _, target := range targets {
		if err := s.publishOneTarget(target, after); err != nil {
			return err
		}
	}
	return nil
}

func (s *forkRecoverySession) publishOneTarget(target forkRecoveryTarget, after bool) error {
	current, err := s.read(target.change.Root, target.change.Path)
	if err != nil {
		return err
	}
	wantExists, want := true, target.after
	if !after {
		wantExists, want = target.change.BeforeExists, target.before
	}
	if current.Exists == wantExists && bytes.Equal(current.Bytes, want) {
		return nil
	}
	owned := current.Exists && (bytes.Equal(current.Bytes, target.before) || bytes.Equal(current.Bytes, target.after))
	if target.change.Root == SourceRootRepository && target.change.Path == "planning-config.json" {
		owned = owned || current.Exists && bytes.Equal(current.Bytes, []byte(s.capture.Intermediate))
	}
	if !owned && !(current.Exists == target.change.BeforeExists && bytes.Equal(current.Bytes, target.before)) {
		return ErrForkRecoveryConflict
	}
	if wantExists {
		if err := publishForkTransactionFile(target.change.Root, s.rootFor(target.change.Root), target.change.Path, want, current.Exists); err != nil {
			return err
		}
	} else {
		if target.change.Root == SourceRootRepository {
			return errors.New("decisionview: recovery will not delete selector configuration")
		}
		if err := removeTransactionFile(s.rootFor(target.change.Root), target.change.Path); err != nil {
			return err
		}
	}
	point := forkRecoveryAfterLedgerPublication
	if target.change.Root == SourceRootRepository {
		if after {
			point = forkRecoveryAfterFinalConfig
		} else {
			point = forkRecoveryAfterIntermediateConfig
		}
	}
	return s.inject(point)
}

func (s *forkRecoverySession) rootFor(root SourceRoot) *os.Root {
	if root == SourceRootRepository {
		return s.repositoryRoot
	}
	return s.store.root
}

func (s *forkRecoverySession) writeJournal() error {
	raw, err := json.Marshal(s.journal)
	if err != nil {
		return err
	}
	if err := s.store.writePrivateJournalFile(forkJournalPath(s.journal.Collections[0], s.journal.OperationID), raw, true); err != nil {
		return err
	}
	return s.inject(forkRecoveryAfterJournalWrite)
}

func (s *forkRecoverySession) writeBarriers(status string) error {
	for _, id := range s.journal.Collections {
		barrier := ForkBarrier{Version: Version1, OperationID: s.journal.OperationID, OwnerID: s.journal.OwnerID, Collection: id, Journal: forkJournalPath(s.journal.Collections[0], s.journal.OperationID), Status: status}
		raw, err := json.Marshal(barrier)
		if err != nil {
			return err
		}
		if err := s.store.writePrivateJournalFile(forkBarrierPath(id), raw, s.barriers[id] != nil); err != nil {
			return err
		}
		s.barriers[id] = &barrier
		if err := s.inject(forkRecoveryAfterBarrierPublication); err != nil {
			return err
		}
	}
	return nil
}

func (s *forkRecoverySession) inject(point forkRecoveryPoint) error {
	if forkRecoveryFailpoint == nil {
		return nil
	}
	return forkRecoveryFailpoint(point)
}

func (s *forkRecoverySession) recoveryRequired(cause error) (*ForkTransactionResult, error) {
	if s.journal.Status != "committed" {
		s.journal.Status = "recovery-required"
		if err := s.writeJournal(); err != nil {
			cause = errors.Join(cause, err)
		} else if err := s.writeBarriers("recovery-required"); err != nil {
			cause = errors.Join(cause, err)
		}
	}
	return &ForkTransactionResult{OperationID: s.journal.OperationID, Outcome: ForkTransactionRecoveryRequired}, cause
}

func (s *forkRecoverySession) allBarriers(status string) bool {
	for _, id := range s.journal.Collections {
		if s.barriers[id] == nil || s.barriers[id].Status != status {
			return false
		}
	}
	return true
}

func (s *forkRecoverySession) String() string {
	return fmt.Sprintf("fork recovery %s", s.journal.OperationID)
}
