package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/dlg"
)

// `sdd decide validate` runs the decision-ledger validator on its own (FR-02).
//
// The same checks run inside `sdd validate`, folded in with the artifact
// rules. This entry point exists because a ledger is often edited on its own —
// and because a ledger may live in a repository that has no planning root at
// all, which `sdd validate` cannot be pointed at.
type decideValidateOpts struct {
	Format    string
	JSON      bool
	NoHistory bool
}

func cmdDecideValidate(ledgerArg string, o decideValidateOpts) error {
	asJSON := o.JSON
	switch o.Format {
	case "", "text":
	case "json":
		asJSON = true
	default:
		return fmt.Errorf("decide validate: --format must be text or json, got %q", o.Format)
	}
	if repository, err := decisionRepositoryRoot(); err == nil {
		capture := decisionview.CaptureForRepository(repository)
		if capture.Declared || len(capture.Diagnostics) > 0 {
			return reportForkValidation(capture, asJSON)
		}
	} else if !errors.Is(err, errNoPlanningConfig) {
		return fmt.Errorf("decide validate: resolving represented repository: %w", err)
	}

	path := ledgerArg
	if path == "" {
		resolved, err := ledgerPath()
		if err != nil {
			return fmt.Errorf("decide validate: %w", err)
		}
		path = resolved
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("decide validate: %s is not a readable ledger file", path)
	}

	diagnostics := dlg.Validate(path, !o.NoHistory)

	if asJSON {
		out, err := json.MarshalIndent(map[string]any{
			"path":        path,
			"diagnostics": diagnostics,
		}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
	} else {
		blocking := 0
		for _, d := range diagnostics {
			fmt.Printf("%s %s %s:%d: %s\n",
				severityLabel(d.Severity), d.Code, d.Path, d.Line, d.Message)
			fmt.Printf("  fix: %s\n", d.Correction)
			if d.Severity.Invalidating() {
				blocking++
			}
		}
		switch {
		case len(diagnostics) == 0:
			fmt.Printf("Valid: %s\n", path)
		case blocking == 0:
			// The compiler distinction: warnings and waived findings are
			// reported, the ledger is still valid, and the exit status says
			// so. Saying "Valid" only when nothing was reported at all would
			// make a warning indistinguishable from a failure.
			fmt.Printf("Valid: %s (%d non-blocking finding(s))\n", path, len(diagnostics))
		}
	}

	// Only errors and operational failures invalidate. A candidate is a signal
	// for a human to judge, a warning is a real defect that cannot threaten
	// correctness (and may be unrepairable inherited history), and a waived
	// finding is one a human explicitly excepted. None sets a failing status.
	for _, d := range diagnostics {
		if d.Severity.Invalidating() {
			return &refusedError{n: countInvalidating(diagnostics)}
		}
	}
	return nil
}

func reportForkValidation(capture *decisionview.ConsumerCapture, asJSON bool) error {
	path := "planning-config.json"
	if capture.Selection != nil && capture.Selection.Config != nil {
		path = filepath.Join(capture.Selection.PlanningRoot, filepath.FromSlash(capture.Selection.Config.Path))
	}
	ids := []string{}
	if capture.View != nil {
		for _, record := range capture.View.Records {
			if record.Applicability == "binding" || record.Applicability == "unresolved" {
				ids = append(ids, string(record.ID))
			}
		}
	}
	sort.Strings(ids)
	if asJSON {
		if err := printJSON(map[string]any{
			"path": path, "effective_decision_ids": ids, "diagnostics": capture.Diagnostics,
			"valid": !forkCaptureInvalid(capture),
		}); err != nil {
			return err
		}
	} else {
		for _, d := range capture.Diagnostics {
			fmt.Printf("%s %s %s:%d: %s\n", strings.ToUpper(string(d.Severity)), d.Code, d.Path, d.Line, d.Message)
			fmt.Printf("  fix: %s\n", d.Correction)
		}
		if !forkCaptureInvalid(capture) {
			fmt.Printf("Valid: %s\n", path)
		}
	}
	if forkCaptureOperational(capture) {
		return fmt.Errorf("decide validate: decision authority could not be captured")
	}
	if forkCaptureInvalid(capture) {
		return &refusedError{n: forkCaptureInvalidCount(capture)}
	}
	return nil
}

func forkCaptureInvalid(capture *decisionview.ConsumerCapture) bool {
	return forkCaptureInvalidCount(capture) > 0
}

func forkCaptureInvalidCount(capture *decisionview.ConsumerCapture) int {
	n := 0
	for _, d := range capture.Diagnostics {
		if d.Severity == decisionview.Error || d.Severity == decisionview.Operational {
			n++
		}
	}
	return n
}

func forkCaptureOperational(capture *decisionview.ConsumerCapture) bool {
	for _, d := range capture.Diagnostics {
		if d.Severity == decisionview.Operational {
			return true
		}
	}
	return false
}

func countInvalidating(diags []dlg.Diagnostic) int {
	n := 0
	for _, d := range diags {
		if d.Severity.Invalidating() {
			n++
		}
	}
	return n
}

func severityLabel(s dlg.Severity) string {
	switch s {
	case dlg.Error:
		return "ERROR"
	case dlg.Warning:
		return "WARNING"
	case dlg.Waived:
		return "WAIVED"
	case dlg.Candidate:
		return "CANDIDATE"
	default:
		return "OPERATIONAL"
	}
}
