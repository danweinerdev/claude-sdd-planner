package decisionview

import (
	"errors"
	"fmt"
)

// ForkSelectorCapture is the separately versioned exact-byte selector state
// required to recover an authority-changing transaction.
type ForkSelectorCapture struct {
	Version      SchemaVersion `json:"version"`
	OperationID  string        `json:"operationId"`
	SourceDigest string        `json:"sourceDigest"`
	Original     string        `json:"original"`
	Intermediate string        `json:"intermediate"`
	Final        string        `json:"final"`
}

var ErrForkSelectorCaptureUnavailable = errors.New("decisionview: valid fork selector capture is unavailable")

func newForkSelectorCapture(preview *PreviewEnvelope, config PreviewFileChange, intermediate []byte, journalPath string) (*ForkSelectorCapture, error) {
	if preview == nil {
		return nil, ErrForkSelectorCaptureUnavailable
	}
	capture := &ForkSelectorCapture{
		Version:      Version1,
		OperationID:  preview.OperationID,
		SourceDigest: collectionDigest([]byte(config.Before)),
		Original:     config.Before,
		Intermediate: string(intermediate),
		Final:        config.After,
	}
	if err := validateForkSelectorCapture(preview, capture, journalPath); err != nil {
		return nil, err
	}
	return capture, nil
}

func validateForkSelectorCapture(preview *PreviewEnvelope, capture *ForkSelectorCapture, journalPath string) error {
	invalid := func(detail string) error {
		return fmt.Errorf("%w: %s", ErrForkSelectorCaptureUnavailable, detail)
	}
	if preview == nil || capture == nil {
		return invalid("capture is missing")
	}
	if capture.Version != Version1 {
		return invalid("capture version is unsupported")
	}
	if capture.OperationID != preview.OperationID || !journalOperationRe.MatchString(capture.OperationID) {
		return invalid("capture operation identity is unbound")
	}
	if capture.SourceDigest != collectionDigest([]byte(capture.Original)) {
		return invalid("capture source digest does not bind original bytes")
	}

	configChanges := 0
	for _, change := range preview.Changes {
		if change.Root != SourceRootRepository || change.Path != "planning-config.json" {
			continue
		}
		configChanges++
		if !change.BeforeExists || capture.Original != change.Before || capture.Final != change.After {
			return invalid("capture bytes do not match the approved config change")
		}
	}

	configSources := 0
	for _, source := range preview.Sources {
		if source.Root == SourceRootRepository && source.Path == "planning-config.json" {
			configSources++
			if source.Digest != capture.SourceDigest {
				return invalid("capture does not match the approved config source")
			}
		}
	}
	switch preview.Operation {
	case "adopt", "detach":
		if configChanges != 1 {
			return invalid("approved operation lacks one exact config change")
		}
	case "rebind":
		if configChanges != 0 || configSources != 1 || capture.Original != capture.Final {
			return invalid("rebind capture changes selector bytes")
		}
	default:
		return invalid("capture operation is unsupported")
	}

	wantIntermediate, err := transactionPendingConfig([]byte(capture.Final), capture.OperationID, journalPath)
	if err != nil || capture.Intermediate != string(wantIntermediate) {
		return invalid("capture intermediate bytes have the wrong pending marker")
	}
	return nil
}

// LoadForkSelectorCapture is the recovery-grade selector accessor. Unlike the
// legacy journal reader, it must not accept a journal without a bound capture.
func (s *LocalStore) LoadForkSelectorCapture(operation string) (*ForkSelectorCapture, error) {
	journal, err := s.LoadForkJournal(operation)
	if err != nil {
		return nil, err
	}
	if journal == nil || journal.SelectorCapture == nil {
		return nil, ErrForkSelectorCaptureUnavailable
	}
	journalPath := forkJournalPath(journal.Collections[0], journal.OperationID)
	if err := validateForkSelectorCapture(&journal.Preview, journal.SelectorCapture, journalPath); err != nil {
		return nil, err
	}
	capture := *journal.SelectorCapture
	return &capture, nil
}
