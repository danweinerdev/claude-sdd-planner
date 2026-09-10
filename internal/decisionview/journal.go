package decisionview

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrForkBarrierPending = errors.New("decisionview: fork journal barrier is pending")
)

// ForkJournal is private recovery state for one approved multi-file preview.
// It is stored below the planning root and is never canonical decision data.
type ForkJournal struct {
	Version         SchemaVersion        `json:"version"`
	OperationID     string               `json:"operationId"`
	OwnerID         OwnerID              `json:"ownerId"`
	Collections     []CollectionID       `json:"collections"`
	Status          string               `json:"status"`
	Preview         PreviewEnvelope      `json:"preview"`
	SelectorCapture *ForkSelectorCapture `json:"selectorCapture,omitempty"`
}

// ForkBarrier makes a pending journal visible to every owner of a shared
// collection before any authoritative file is published.
type ForkBarrier struct {
	Version     SchemaVersion `json:"version"`
	OperationID string        `json:"operationId"`
	OwnerID     OwnerID       `json:"ownerId"`
	Collection  CollectionID  `json:"collectionId"`
	Journal     string        `json:"journal"`
	Status      string        `json:"status"`
}

func NewForkJournal(owner OwnerID, collections []CollectionID, preview *PreviewEnvelope) (*ForkJournal, error) {
	if err := owner.Validate(); err != nil {
		return nil, err
	}
	if preview == nil || strings.TrimSpace(preview.OperationID) == "" {
		return nil, fmt.Errorf("decisionview: journal requires an exact preview identity")
	}
	if err := VerifyPreviewEnvelope(preview, preview.Digest); err != nil {
		return nil, err
	}
	if len(collections) == 0 {
		return nil, fmt.Errorf("decisionview: journal requires at least one collection")
	}
	ids := append([]CollectionID(nil), collections...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for i, id := range ids {
		if err := id.Validate(); err != nil {
			return nil, err
		}
		if i > 0 && ids[i-1] == id {
			return nil, fmt.Errorf("decisionview: duplicate journal collection %s", id)
		}
	}
	raw, err := json.Marshal(preview)
	if err != nil {
		return nil, err
	}
	copy, err := DecodePreviewEnvelope(raw)
	if err != nil {
		return nil, err
	}
	journal := &ForkJournal{
		Version:     Version1,
		OperationID: preview.OperationID,
		OwnerID:     owner,
		Collections: ids,
		Status:      "pending",
		Preview:     *copy,
	}
	if err := validateForkJournal(journal); err != nil {
		return nil, err
	}
	return journal, nil
}

// SaveForkJournal publishes a complete private journal before any barrier.
// Partial barrier publication stays pending and blocks reliance; it is never
// reported as a clean authoritative transaction. No public ledger is written.
func (s *LocalStore) SaveForkJournal(ctx context.Context, journal *ForkJournal) error {
	if err := validateForkJournal(journal); err != nil {
		return err
	}
	if journal.Status != "pending" {
		return fmt.Errorf("decisionview: initial journal must be pending")
	}
	if ctx == nil {
		return fmt.Errorf("decisionview: journal context required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var held []*os.File
	defer func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = unlockLocalFile(held[i])
			_ = held[i].Close()
		}
	}()
	for _, id := range journal.Collections {
		lockDir := fmt.Sprintf("Decisions/.fork-state/%x", sha256.Sum256([]byte(id)))
		if err := s.root.MkdirAll(lockDir, 0o700); err != nil {
			return err
		}
		f, err := s.root.OpenFile(lockDir+"/writer.lock", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				f.Close()
				return err
			}
			if err := s.tryLock(f); err == nil {
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
		held = append(held, f)
	}
	for _, id := range journal.Collections {
		barrier, err := s.InspectForkBarrier(id)
		if err != nil {
			return err
		}
		if barrier != nil && barrier.Status != "committed" && barrier.Status != "rolled-back" {
			if barrier.OwnerID != journal.OwnerID || barrier.OperationID != journal.OperationID {
				return ErrForkBarrierPending
			}
		}
	}
	if s.afterCheck != nil {
		s.afterCheck()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	journalPath := forkJournalPath(journal.Collections[0], journal.OperationID)
	if old, err := s.LoadForkJournal(journal.OperationID); err != nil {
		return err
	} else if old != nil {
		before, _ := json.Marshal(old)
		after, _ := json.Marshal(journal)
		if string(before) != string(after) {
			return fmt.Errorf("decisionview: journal operation identity cannot be reused")
		}
	} else {
		raw, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		if err := s.writePrivateJournalFile(journalPath, raw, false); err != nil {
			return err
		}
	}
	for _, id := range journal.Collections {
		barrier := ForkBarrier{Version: Version1, OperationID: journal.OperationID, OwnerID: journal.OwnerID, Collection: id, Journal: journalPath, Status: "pending"}
		raw, err := json.Marshal(barrier)
		if err != nil {
			return err
		}
		_, statErr := s.root.Stat(forkBarrierPath(id))
		existing := statErr == nil
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if err := s.writePrivateJournalFile(forkBarrierPath(id), raw, existing); err != nil {
			return fmt.Errorf("decisionview: journal exists but barrier publication needs recovery: %w", err)
		}
	}
	return nil
}

// LoadForkJournal is a side-effect-free read of private recovery state.
func (s *LocalStore) LoadForkJournal(operation string) (*ForkJournal, error) {
	if !journalOperationRe.MatchString(operation) {
		return nil, fmt.Errorf("decisionview: invalid journal operation ID")
	}
	dir, err := s.root.Open("Decisions/.fork-state")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var found *ForkJournal
	for _, entry := range entries {
		if !entry.IsDir() || !uuidRe.MatchString(entry.Name()) {
			continue
		}
		raw, _, err := readCollectionFile(s.root, forkJournalPath(CollectionID(entry.Name()), operation))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var journal ForkJournal
		if err := modelStrictJSON(raw, &journal); err != nil {
			return nil, err
		}
		if err := validateForkJournal(&journal); err != nil {
			return nil, err
		}
		if journal.OperationID != operation || journal.Collections[0] != CollectionID(entry.Name()) {
			return nil, fmt.Errorf("decisionview: journal locator/identity mismatch")
		}
		if found != nil {
			return nil, fmt.Errorf("decisionview: ambiguous journal operation identity")
		}
		found = &journal
	}
	return found, nil
}

// InspectForkBarrier is a side-effect-free read of pending collection state.
func (s *LocalStore) InspectForkBarrier(id CollectionID) (*ForkBarrier, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	raw, _, err := readCollectionFile(s.root, forkBarrierPath(id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var barrier ForkBarrier
	if err := modelStrictJSON(raw, &barrier); err != nil {
		return nil, err
	}
	if barrier.Version != Version1 || barrier.Collection != id || barrier.OwnerID.Validate() != nil || !journalOperationRe.MatchString(barrier.OperationID) || !journalStatus(barrier.Status) || validateRelativeLocator(barrier.Journal) != nil {
		return nil, fmt.Errorf("decisionview: invalid shared barrier")
	}
	journalRaw, _, err := readCollectionFile(s.root, barrier.Journal)
	if err != nil {
		return nil, fmt.Errorf("decisionview: barrier journal cannot be checked: %w", err)
	}
	var journal ForkJournal
	if err := modelStrictJSON(journalRaw, &journal); err != nil {
		return nil, err
	}
	if err := validateForkJournal(&journal); err != nil {
		return nil, err
	}
	member := false
	for _, candidate := range journal.Collections {
		if candidate == id {
			member = true
		}
	}
	if !member || journal.OwnerID != barrier.OwnerID || journal.OperationID != barrier.OperationID || barrier.Journal != forkJournalPath(journal.Collections[0], journal.OperationID) {
		return nil, fmt.Errorf("decisionview: barrier/journal identity mismatch")
	}
	if (barrier.Status == "committed" || barrier.Status == "rolled-back") && barrier.Status != journal.Status {
		return nil, fmt.Errorf("decisionview: barrier prematurely claims a terminal journal state")
	}
	return &barrier, nil
}

var journalOperationRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func forkJournalPath(id CollectionID, operation string) string {
	return "Decisions/.fork-state/" + string(id) + "/journals/" + operation + ".json"
}
func forkBarrierPath(id CollectionID) string {
	return "Decisions/.fork-state/" + string(id) + "/pending.json"
}
func journalStatus(status string) bool {
	switch status {
	case "pending", "committed", "rolled-back", "recovery-required":
		return true
	}
	return false
}
func validateForkJournal(j *ForkJournal) error {
	if j == nil || j.Version != Version1 || j.OwnerID.Validate() != nil || !journalOperationRe.MatchString(j.OperationID) || !journalStatus(j.Status) || len(j.Collections) == 0 {
		return fmt.Errorf("decisionview: invalid fork journal")
	}
	if j.OperationID != j.Preview.OperationID {
		return fmt.Errorf("decisionview: journal/preview operation mismatch")
	}
	if err := VerifyPreviewEnvelope(&j.Preview, j.Preview.Digest); err != nil {
		return err
	}
	// Legacy journals may omit selector capture. Once present, however, it is
	// recovery authority and journal reads deliberately fail closed unless its
	// exact bytes remain bound to the approved preview and pending marker.
	if j.SelectorCapture != nil {
		journalPath := forkJournalPath(j.Collections[0], j.OperationID)
		if err := validateForkSelectorCapture(&j.Preview, j.SelectorCapture, journalPath); err != nil {
			return err
		}
	}
	for i, id := range j.Collections {
		if id.Validate() != nil || (i > 0 && j.Collections[i-1] >= id) {
			return fmt.Errorf("decisionview: journal collections must be valid, sorted and unique")
		}
	}
	return nil
}

// The cooperative journal guard is already held. Avoid reacquiring the same
// lock through WriteExpected, and publish sensitive before/after bytes at 0600.
func (s *LocalStore) writePrivateJournalFile(relative string, raw []byte, existing bool) error {
	if err := validateRelativeLocator(relative); err != nil {
		return err
	}
	parentName := path.Dir(relative)
	if err := s.root.MkdirAll(parentName, 0o700); err != nil {
		return err
	}
	parent, err := s.root.OpenRoot(parentName)
	if err != nil {
		return err
	}
	defer parent.Close()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temp := fmt.Sprintf(".journal-%x", nonce)
	f, err := parent.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer parent.Remove(temp)
	if _, err := f.Write(raw); err != nil {
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
	return publishLocalFile(parent, temp, path.Base(relative), existing)
}
