package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

type decideForkReadOutput struct {
	Version      int                             `json:"version"`
	View         string                          `json:"view"`
	Effective    bool                            `json:"effective"`
	Resolution   decisionview.Resolution         `json:"resolution"`
	RepositoryID decisionview.OwnerID            `json:"repository_id,omitempty"`
	LedgerID     decisionview.CollectionID       `json:"ledger_id,omitempty"`
	Decisions    []decisionview.ResolvedDecision `json:"decisions"`
	Diagnostics  []decisionview.Diagnostic       `json:"diagnostics"`
	Original     *decisionview.ResolvedDecision  `json:"original,omitempty"`
	Current      *decisionview.ResolvedDecision  `json:"current,omitempty"`
}

func cmdDecideForkRead(view, lookup, status, term string, jsonOut bool) error {
	resolved, explicit, err := loadDecideForkView()
	if err != nil {
		return fmt.Errorf("decide %s: %w", view, err)
	}
	if !explicit {
		if view == "list" || view == "search" {
			return errLegacyDecisionRead
		}
		return fmt.Errorf("repository uses legacy decision discovery; use `sdd decide list` or `sdd decide search`, or adopt explicit fork authority before using %s", view)
	}

	effective := view != "history"
	out := decideForkReadOutput{
		Version: 1, View: view, Effective: effective, Resolution: resolved.Resolution,
		RepositoryID: resolved.OwnerID, LedgerID: resolved.LocalID,
		Decisions:   []decisionview.ResolvedDecision{},
		Diagnostics: append([]decisionview.Diagnostic(nil), resolved.Diagnostics...),
	}
	if view == "lookup" {
		result, lookupErr := decisionview.LookupReference(resolved, lookup, nil)
		if lookupErr != nil && !forkReadHasAuthorityFailure(resolved) {
			return fmt.Errorf("decide lookup: %w", lookupErr)
		}
		if lookupErr == nil {
			out.Original = &result.Original
			out.Current = result.Effective
		}
	} else {
		out.Decisions = filterForkDecisions(resolved.Records, effective, status, term)
	}

	if jsonOut {
		if err := writeJSON(out); err != nil {
			return err
		}
	} else {
		if out.Original != nil {
			statement, _ := out.Original.Original["statement"].(string)
			fmt.Printf("original: %s (%s, source %s:%s)\n", out.Original.ID, out.Original.Applicability, out.Original.Source.Root, out.Original.Source.Path)
			fmt.Printf("statement: %s\n", statement)
			if out.Current != nil {
				fmt.Printf("current authority: %s (%s)\n", out.Current.ID, out.Current.Applicability)
			} else {
				fmt.Println("current authority: none")
			}
		} else {
			for _, record := range out.Decisions {
				statement, _ := record.Original["statement"].(string)
				fmt.Printf("%-52s %-12s %s\n", record.ID, record.Applicability, statement)
			}
		}
		for _, diagnostic := range out.Diagnostics {
			fmt.Fprintf(os.Stderr, "%s %s: %s\n", diagnostic.Severity, diagnostic.Code, diagnostic.Message)
		}
	}
	for _, diagnostic := range resolved.Diagnostics {
		if diagnostic.Severity == decisionview.Operational {
			return fmt.Errorf("decide %s: decision authority could not be captured", view)
		}
	}
	if forkReadHasAuthorityFailure(resolved) {
		return &refusedError{n: forkReadFailureCount(resolved)}
	}
	if view == "list" && status != "" && status != "accepted" {
		return &refusedError{n: 1, msg: "decide list: --status " + status + " is non-effective history; use `sdd decide history`"}
	}
	return nil
}

var errLegacyDecisionRead = errors.New("legacy decision read")

func cmdDecideCapabilities(jsonOut bool) error {
	out := struct {
		Version       int `json:"version"`
		DecisionForks struct {
			Schema                int      `json:"schema"`
			Canonicalization      []string `json:"canonicalization"`
			ReadViews             []string `json:"read_views"`
			WriteOperations       []string `json:"write_operations"`
			Transactions          int      `json:"transactions"`
			TransactionOperations []string `json:"transaction_operations"`
			Partial               bool     `json:"partial"`
		} `json:"decision_forks"`
	}{Version: 1}
	out.DecisionForks.Schema = 1
	out.DecisionForks.Canonicalization = []string{decisionview.CanonicalVersion}
	out.DecisionForks.ReadViews = []string{"effective", "history", "lookup", "list", "search"}
	out.DecisionForks.WriteOperations = []string{"adopt", "rebind", "detach", "override", "reconcile", "restore"}
	out.DecisionForks.Transactions = 1
	out.DecisionForks.TransactionOperations = []string{"preview", "apply", "inspect", "recover"}
	out.DecisionForks.Partial = true
	if jsonOut {
		return writeJSON(out)
	}
	fmt.Println("decision_forks: schema 1; effective/history/lookup/list/search, exact fork writes, and transaction preview/apply/inspect/recover; partial (add/accept/supersede/archive refuse unless expressed by a supported fork operation)")
	return nil
}

func loadDecideForkView() (*decisionview.ResolvedView, bool, error) {
	repository, err := decisionRepositoryRoot()
	if err != nil {
		return nil, false, err
	}
	selection, err := decisionview.ReadSelection(repository)
	if errors.Is(err, decisionview.ErrSelectionPending) && selection != nil {
		return forkReadFailureView(selection, decisionview.ResolutionRecoveryNeeded, "FDL022", "Selected decision authority has a pending transaction"), true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !selection.Explicit {
		if selection.RepositoryID != "" {
			return forkReadFailureView(selection, decisionview.ResolutionInvalid, "FDL020", "Repository decision identity remains but its declared authority selection was removed"), true, nil
		}
		return nil, false, nil
	}

	// Use the same stable, rechecked capture as validation and hooks rather than
	// maintaining a second resolver without publication-window protection.
	capture := decisionview.CaptureForRepository(repository)
	resolved := capture.View
	if resolved == nil {
		resolved = &decisionview.ResolvedView{Version: 1, OwnerID: selection.RepositoryID, LocalID: selection.Config.LedgerID, Resolution: decisionview.ResolutionInvalid, Records: []decisionview.ResolvedDecision{}}
	}
	resolved.Diagnostics = append([]decisionview.Diagnostic(nil), capture.Diagnostics...)
	for _, diagnostic := range resolved.Diagnostics {
		if diagnostic.Code == "FDL022" {
			resolved.Resolution = decisionview.ResolutionRecoveryNeeded
		} else if (diagnostic.Severity == decisionview.Error || diagnostic.Severity == decisionview.Operational) && resolved.Resolution == decisionview.ResolutionComplete {
			resolved.Resolution = decisionview.ResolutionInvalid
		}
	}
	return resolved, true, nil
}

func decisionRepositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return decisionRepositoryRootFrom(dir)
}

var errNoPlanningConfig = errors.New("no planning-config.json found")

func decisionRepositoryRootFrom(dir string) (string, error) {
	var err error
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		configPath := filepath.Join(dir, "planning-config.json")
		if info, statErr := os.Stat(configPath); statErr == nil {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("%s is not a regular file", configPath)
			}
			return dir, nil
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w at or above %s", errNoPlanningConfig, dir)
		}
		dir = parent
	}
}

func forkReadFailureView(selection *decisionview.Selection, resolution decisionview.Resolution, code, message string) *decisionview.ResolvedView {
	view := &decisionview.ResolvedView{Version: 1, Resolution: resolution, Records: []decisionview.ResolvedDecision{}, Diagnostics: []decisionview.Diagnostic{forkReadDiagnostic(code, "planning-config.json", message)}}
	if selection != nil {
		view.OwnerID = selection.RepositoryID
		if selection.Config != nil {
			view.LocalID = selection.Config.LedgerID
		}
	}
	return view
}

func forkReadDiagnostic(code, path, message string) decisionview.Diagnostic {
	return decisionview.Diagnostic{Severity: decisionview.Error, Code: code, Path: path, Message: message, Correction: "Inspect and explicitly recover or reconcile the declared decision authority before relying on it."}
}

func filterForkDecisions(records []decisionview.ResolvedDecision, effective bool, status, term string) []decisionview.ResolvedDecision {
	out := make([]decisionview.ResolvedDecision, 0, len(records))
	term = strings.ToLower(term)
	for _, record := range records {
		if effective && record.Applicability != "binding" {
			continue
		}
		if status != "" && record.OriginalStatus != status {
			continue
		}
		if term != "" && !forkEntryMatches(record.Original, term) {
			continue
		}
		out = append(out, record)
	}
	return out
}

func forkEntryMatches(entry map[string]any, term string) bool {
	for _, key := range []string{"statement", "rationale", "question", "confirmation"} {
		if value, _ := entry[key].(string); strings.Contains(strings.ToLower(value), term) {
			return true
		}
	}
	for _, key := range []string{"tags", "scope"} {
		switch values := entry[key].(type) {
		case []any:
			for _, value := range values {
				if text, _ := value.(string); strings.Contains(strings.ToLower(text), term) {
					return true
				}
			}
		case []string:
			for _, text := range values {
				if strings.Contains(strings.ToLower(text), term) {
					return true
				}
			}
		}
	}
	return false
}

func forkReadHasAuthorityFailure(view *decisionview.ResolvedView) bool {
	if view.Resolution != decisionview.ResolutionComplete {
		return true
	}
	for _, diagnostic := range view.Diagnostics {
		if diagnostic.Severity == decisionview.Error || diagnostic.Severity == decisionview.Operational {
			return true
		}
	}
	return false
}

func forkReadFailureCount(view *decisionview.ResolvedView) int {
	count := 0
	for _, diagnostic := range view.Diagnostics {
		if diagnostic.Severity == decisionview.Error || diagnostic.Severity == decisionview.Operational {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}

func refuseUnsafeForkWrite() error {
	_, explicit, err := loadDecideForkView()
	if err != nil {
		return err
	}
	if explicit {
		return &refusedError{n: 1, msg: "decide add: explicit fork authority is selected; this legacy writer is not fork-safe—use `sdd decide fork preview` and preserve inherited history"}
	}
	return nil
}
