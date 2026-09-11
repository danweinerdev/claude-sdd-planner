package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"gopkg.in/yaml.v3"
)

type decideForkPreviewOpts struct {
	Operation string
	File      string
	JSON      bool
}

type decideForkApplyOpts struct {
	File           string
	ApprovalDigest string
	JSON           bool
}

type decideForkInspectOpts struct {
	Operation string
	JSON      bool
}

type decideForkRecoverOpts struct {
	Operation      string
	Action         string
	ApprovalDigest string
	JSON           bool
}

type forkWriteRequest struct {
	Version       decisionview.SchemaVersion `json:"version"`
	Operation     string                     `json:"operation"`
	OperationID   string                     `json:"operationId"`
	RepositoryID  decisionview.OwnerID       `json:"repositoryId"`
	LedgerID      decisionview.CollectionID  `json:"ledgerId"`
	Path          string                     `json:"path"`
	Source        decisionview.SourceLocator `json:"source"`
	SourceOwnerID decisionview.OwnerID       `json:"sourceOwnerId"`
}

type forkWriteContext struct {
	repository  string
	planning    string
	config      []byte
	selection   *decisionview.Selection
	local       *decisionview.Collection
	collections map[decisionview.CollectionID]*decisionview.Collection
}

type forkApplyOutput struct {
	Version     int    `json:"version"`
	OperationID string `json:"operationId"`
	Outcome     string `json:"outcome"`
	Replayed    bool   `json:"replayed,omitempty"`
}

func cmdDecideForkPreview(o decideForkPreviewOpts) error {
	proposal, err := readForkJSONFile(o.File)
	if err != nil {
		return fmt.Errorf("decide fork preview: %w", err)
	}
	if o.Operation == "archive" {
		return forkWriteRefusal("decide fork preview: archive is not implemented; preserve existing local and inherited history and migrate through a supported exact-approved operation")
	}
	var request forkWriteRequest
	if err := json.Unmarshal(proposal, &request); err != nil {
		return fmt.Errorf("decide fork preview: %w", err)
	}
	if request.Operation != o.Operation {
		return forkWriteRefusal("decide fork preview: --operation must exactly match the proposal operation")
	}
	ctx, err := captureForkWriteContext(request.Operation)
	if err != nil {
		return forkWriteRefusal("decide fork preview: " + err.Error())
	}
	var envelope *decisionview.PreviewEnvelope
	switch request.Operation {
	case "adopt", "rebind":
		source, err := loadForkProposalSource(ctx, request.Source)
		if err != nil {
			return forkWriteRefusal("decide fork preview: " + err.Error())
		}
		if err := loadForkAncestry(ctx, source); err != nil {
			return forkWriteRefusal("decide fork preview: " + err.Error())
		}
		preview, err := decisionview.PreviewForkAdoption(decisionview.ForkAdoptionSnapshot{ConfigBefore: ctx.config, Local: ctx.local, Source: source, Collections: ctx.collections}, proposal)
		if err != nil {
			return forkWriteRefusal("decide fork preview: " + err.Error())
		}
		envelope = preview.Envelope
	case "override", "reconcile":
		if err := requireSelectedForkContext(ctx); err != nil {
			return forkWriteRefusal("decide fork preview: " + err.Error())
		}
		preview, err := decisionview.PreviewForkOverride(decisionview.ForkOverrideSnapshot{Local: ctx.local, Collections: ctx.collections}, proposal)
		if err != nil {
			return forkWriteRefusal("decide fork preview: " + err.Error())
		}
		envelope = preview.Envelope
	case "restore", "detach":
		if err := requireSelectedForkContext(ctx); err != nil {
			return forkWriteRefusal("decide fork preview: " + err.Error())
		}
		preview, err := decisionview.PreviewForkRestoration(decisionview.ForkRestorationSnapshot{ConfigBefore: ctx.config, Local: ctx.local, Collections: ctx.collections}, proposal)
		if err != nil {
			return forkWriteRefusal("decide fork preview: " + err.Error())
		}
		envelope = preview.Envelope
	default:
		return forkWriteRefusal(fmt.Sprintf("decide fork preview: operation %q is not implemented; preserve existing authority and use a supported exact-approved operation", request.Operation))
	}
	if o.JSON {
		return writeJSON(envelope)
	}
	fmt.Printf("operation: %s\ndigest: %s\n", envelope.OperationID, envelope.Digest)
	for _, change := range envelope.Changes {
		fmt.Printf("change: %s:%s\n", change.Root, change.Path)
	}
	fmt.Println("approval: rerun with --json and save the complete exact bytes before approving or applying")
	return nil
}

func cmdDecideForkApply(o decideForkApplyOpts) error {
	if strings.TrimSpace(o.ApprovalDigest) == "" {
		return forkWriteRefusal("decide fork apply: exact --approval-digest is required")
	}
	raw, err := readForkJSONFile(o.File)
	if err != nil {
		return fmt.Errorf("decide fork apply: %w", err)
	}
	envelope, err := decisionview.DecodePreviewEnvelope(raw)
	if err != nil {
		return forkWriteRefusal("decide fork apply: " + err.Error())
	}
	if envelope.Digest != o.ApprovalDigest {
		return forkWriteRefusal("decide fork apply: exact approval digest does not match the saved envelope")
	}
	ctx, err := captureForkWriteContext(envelope.Operation)
	if err != nil {
		return forkWriteRefusal("decide fork apply: " + err.Error())
	}
	request, err := decodeForkWriteRequest(envelope.Request)
	if err != nil {
		return forkWriteRefusal("decide fork apply: " + err.Error())
	}
	var output *forkApplyOutput
	var applyErr error
	switch envelope.Operation {
	case "adopt", "rebind", "detach":
		collections, err := forkTransactionCollections(ctx, envelope, request)
		if err != nil {
			return forkWriteRefusal("decide fork apply: " + err.Error())
		}
		owner := request.RepositoryID
		if envelope.Operation == "detach" && ctx.selection != nil {
			owner = ctx.selection.RepositoryID
		}
		tx, err := decisionview.NewForkTransaction(decisionview.ForkTransactionRequest{Repository: ctx.repository, Planning: ctx.planning, OwnerID: owner, Collections: collections, Preview: envelope, ApprovalDigest: o.ApprovalDigest})
		if err != nil {
			return forkWriteRefusal("decide fork apply: " + err.Error())
		}
		result, err := tx.Apply(context.Background())
		applyErr = err
		if result != nil {
			output = &forkApplyOutput{Version: 1, OperationID: result.OperationID, Outcome: string(result.Outcome)}
		}
	case "override", "reconcile", "restore":
		if err := requireSelectedForkContext(ctx); err != nil {
			return forkWriteRefusal("decide fork apply: " + err.Error())
		}
		publication, err := decisionview.NewForkAuthorityPublication(decisionview.ForkAuthorityPublicationRequest{Repository: ctx.repository, Planning: ctx.planning, OwnerID: ctx.selection.RepositoryID, Collection: ctx.local.ID, Preview: envelope, ApprovalDigest: o.ApprovalDigest})
		if err != nil {
			return forkWriteRefusal("decide fork apply: " + err.Error())
		}
		result, err := publication.Apply(context.Background())
		applyErr = err
		if result != nil {
			output = &forkApplyOutput{Version: 1, OperationID: result.OperationID, Outcome: string(result.Outcome), Replayed: result.Replayed}
		}
	default:
		return forkWriteRefusal("decide fork apply: unsupported envelope operation; no files were changed")
	}
	if output == nil && applyErr != nil {
		return forkWriteRefusal("decide fork apply: " + applyErr.Error())
	}
	return emitForkOutcome("decide fork apply", output, applyErr, o.JSON)
}

func cmdDecideForkInspect(o decideForkInspectOpts) error {
	ctx, err := captureForkWriteContext("inspect")
	if err != nil {
		return fmt.Errorf("decide fork inspect: %w", err)
	}
	inspection, err := decisionview.InspectForkRecovery(ctx.repository, ctx.planning, o.Operation)
	if err != nil {
		return fmt.Errorf("decide fork inspect: %w", err)
	}
	out := struct {
		Version     int                                 `json:"version"`
		OperationID string                              `json:"operationId"`
		Outcome     decisionview.ForkTransactionOutcome `json:"outcome"`
		Authority   decisionview.Resolution             `json:"authority"`
		Journal     string                              `json:"journal,omitempty"`
	}{1, inspection.OperationID, inspection.Outcome, inspection.Authority, inspection.Journal}
	if o.JSON {
		return writeJSON(out)
	}
	fmt.Printf("%s: %s (%s)\n", out.OperationID, out.Outcome, out.Authority)
	return nil
}

func cmdDecideForkRecover(o decideForkRecoverOpts) error {
	ctx, err := captureForkWriteContext("recover")
	if err != nil {
		return fmt.Errorf("decide fork recover: %w", err)
	}
	action := decisionview.ForkRecoveryAction(o.Action)
	preview, err := decisionview.PreviewForkRecovery(ctx.repository, ctx.planning, o.Operation, action)
	if err != nil {
		return fmt.Errorf("decide fork recover: %w", err)
	}
	if o.ApprovalDigest == "" {
		if o.JSON {
			return writeJSON(preview)
		}
		fmt.Printf("operation: %s\naction: %s\ndigest: %s\n", preview.OperationID, preview.Action, preview.Digest)
		fmt.Println("approval: rerun with --json and save the complete exact bytes before approving recovery")
		return nil
	}
	if o.ApprovalDigest != preview.Digest {
		return forkWriteRefusal("decide fork recover: exact current-state-bound recovery approval digest is required")
	}
	result, recoverErr := decisionview.RecoverForkTransaction(context.Background(), decisionview.ForkRecoveryRequest{Repository: ctx.repository, Planning: ctx.planning, Preview: preview, ApprovalDigest: o.ApprovalDigest})
	var out *forkApplyOutput
	if result != nil {
		out = &forkApplyOutput{Version: 1, OperationID: result.OperationID, Outcome: string(result.Outcome)}
	}
	if out == nil && recoverErr != nil {
		return forkWriteRefusal("decide fork recover: " + recoverErr.Error())
	}
	return emitForkOutcome("decide fork recover", out, recoverErr, o.JSON)
}

func emitForkOutcome(command string, out *forkApplyOutput, operationErr error, jsonOut bool) error {
	var outputErr error
	if out != nil {
		if jsonOut {
			outputErr = writeJSON(out)
		} else {
			_, outputErr = fmt.Printf("%s: %s\n", out.OperationID, out.Outcome)
		}
	}
	return joinForkOutcomeErrors(command, out, operationErr, outputErr)
}

func joinForkOutcomeErrors(command string, out *forkApplyOutput, operationErr, outputErr error) error {
	if operationErr != nil && out != nil {
		operationErr = fmt.Errorf("%s: %s: %w", command, out.Outcome, operationErr)
	}
	if outputErr != nil && out != nil {
		outputErr = fmt.Errorf("%s: encode operation %s outcome %s: %w", command, out.OperationID, out.Outcome, outputErr)
	}
	return errors.Join(operationErr, outputErr)
}

func captureForkWriteContext(operation string) (*forkWriteContext, error) {
	repository, err := decisionRepositoryRoot()
	if err != nil {
		return nil, err
	}
	planning, err := store.FindPlanningRoot(repository)
	if err != nil {
		return nil, err
	}
	config, err := os.ReadFile(filepath.Join(repository, "planning-config.json"))
	if err != nil {
		return nil, err
	}
	ctx := &forkWriteContext{repository: repository, planning: planning, config: config, collections: map[decisionview.CollectionID]*decisionview.Collection{}}
	selection, selectionErr := decisionview.ReadSelection(repository)
	ctx.selection = selection
	if errors.Is(selectionErr, decisionview.ErrSelectionPending) && (operation == "inspect" || operation == "recover") {
		return ctx, nil
	}
	if selectionErr != nil {
		return nil, selectionErr
	}
	if selection == nil || !selection.Explicit {
		return ctx, nil
	}
	capture := decisionview.CaptureForRepository(repository)
	if capture.Selection == nil || capture.Selection.Config == nil {
		return nil, errors.New("selected fork authority is unavailable")
	}
	if operation != "reconcile" && operation != "restore" && (capture.View == nil || capture.View.Resolution != decisionview.ResolutionComplete || forkReadHasAuthorityFailure(capture.View)) {
		return nil, errors.New("selected fork authority is not complete; inspect/recover or reconcile it first")
	}
	ctx.planning = capture.PlanningRoot
	ctx.selection = capture.Selection
	ctx.collections = capture.Collections
	ctx.local = capture.Collections[capture.Selection.Config.LedgerID]
	// The shared consumer capture intentionally follows only effective ancestry.
	// Writers must additionally retain every archived binding because semantic
	// regeneration and transaction exclusion sets cover the complete authority
	// graph, not just the currently selected parent chain.
	if err := loadForkAncestry(ctx, ctx.local); err != nil {
		return nil, err
	}
	return ctx, nil
}

func requireSelectedForkContext(ctx *forkWriteContext) error {
	if ctx == nil || ctx.selection == nil || !ctx.selection.Explicit || ctx.selection.Config == nil || ctx.selection.Config.Mode != "fork" || ctx.local == nil {
		return errors.New("operation requires complete selected fork authority")
	}
	return nil
}

func decodeForkWriteRequest(raw json.RawMessage) (forkWriteRequest, error) {
	var request forkWriteRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return request, err
	}
	return request, nil
}

func forkTransactionCollections(ctx *forkWriteContext, envelope *decisionview.PreviewEnvelope, request forkWriteRequest) ([]decisionview.CollectionID, error) {
	set := map[decisionview.CollectionID]bool{}
	if envelope.Operation == "adopt" {
		set[request.LedgerID] = true
	} else {
		for id := range ctx.collections {
			set[id] = true
		}
	}
	if envelope.Operation == "rebind" {
		source, err := loadForkProposalSource(ctx, request.Source)
		if err != nil {
			return nil, err
		}
		if err := loadForkAncestry(ctx, source); err != nil {
			return nil, err
		}
		for id := range ctx.collections {
			set[id] = true
		}
	}
	ids := make([]decisionview.CollectionID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func loadForkProposalSource(ctx *forkWriteContext, locator decisionview.SourceLocator) (*decisionview.Collection, error) {
	if err := locator.Validate(); err != nil {
		return nil, err
	}
	metadata, err := readForkMetadata(decisionview.Roots{Repository: ctx.repository, Planning: ctx.planning}, locator)
	if err != nil {
		return nil, err
	}
	if metadata == nil {
		return nil, errors.New("CLI adoption source lacks explicit collection identity metadata")
	}
	collection, err := decisionview.LoadCollection(decisionview.Roots{Repository: ctx.repository, Planning: ctx.planning}, metadata.LedgerID, locator)
	if err != nil {
		return nil, err
	}
	ctx.collections[collection.ID] = collection
	return collection, nil
}

func loadForkAncestry(ctx *forkWriteContext, collection *decisionview.Collection) error {
	if collection == nil || collection.Metadata == nil {
		return nil
	}
	for _, binding := range collection.Metadata.Bindings {
		if ctx.collections[binding.CollectionID] != nil {
			continue
		}
		next, err := decisionview.LoadCollection(decisionview.Roots{Repository: ctx.repository, Planning: ctx.planning}, binding.CollectionID, binding.Source)
		if err != nil {
			return err
		}
		ctx.collections[next.ID] = next
		if err := loadForkAncestry(ctx, next); err != nil {
			return err
		}
	}
	return nil
}

func readForkMetadata(roots decisionview.Roots, locator decisionview.SourceLocator) (*decisionview.ForkMetadata, error) {
	if err := locator.Validate(); err != nil {
		return nil, err
	}
	base := roots.Planning
	if locator.Root == decisionview.SourceRootRepository {
		base = roots.Repository
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(locator.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 64<<20 {
		return nil, errors.New("decision source exceeds 64 MiB")
	}
	doc := artifact.Parse(string(raw))
	if !doc.HasFrontmatter {
		return nil, errors.New("decision source lacks frontmatter")
	}
	var frontmatter struct {
		Fork *decisionview.ForkMetadata `yaml:"fork"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(doc.FrontmatterRaw, "\n")), &frontmatter); err != nil {
		return nil, err
	}
	if frontmatter.Fork != nil {
		if err := frontmatter.Fork.Validate(); err != nil {
			return nil, err
		}
	}
	return frontmatter.Fork, nil
}

func readForkJSONFile(name string) ([]byte, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("--file is required")
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 16<<20 {
		return nil, errors.New("JSON input exceeds 16 MiB")
	}
	return raw, nil
}

func forkWriteRefusal(message string) error {
	return &refusedError{n: 1, msg: message}
}

func refuseUnmigratedForkMutation(name string) error {
	_, explicit, err := loadDecideForkView()
	if err != nil {
		return err
	}
	if explicit {
		return forkWriteRefusal("decide " + name + ": explicit fork authority is selected; this writer is not migrated—use `sdd decide fork preview` and preserve inherited history")
	}
	return fmt.Errorf("decide %s is not implemented for legacy decision discovery", name)
}
