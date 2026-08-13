package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/danweinerdev/claude-sdd-planner/internal/dlg"
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
		for _, d := range diagnostics {
			fmt.Printf("%s %s %s:%d: %s\n",
				severityLabel(d.Severity), d.Code, d.Path, d.Line, d.Message)
			fmt.Printf("  fix: %s\n", d.Correction)
		}
		if len(diagnostics) == 0 {
			fmt.Printf("Valid: %s\n", path)
		}
	}

	// Candidate and operational findings are advisory: a candidate is a signal
	// for a human to judge, and an operational one reports that a check could
	// not run rather than that the ledger is wrong. Neither makes the ledger
	// invalid, so neither sets a failing exit status.
	for _, d := range diagnostics {
		if d.Severity == dlg.Error {
			return &refusedError{n: len(diagnostics)}
		}
	}
	return nil
}

func severityLabel(s dlg.Severity) string {
	switch s {
	case dlg.Error:
		return "ERROR"
	case dlg.Candidate:
		return "CANDIDATE"
	default:
		return "OPERATIONAL"
	}
}
