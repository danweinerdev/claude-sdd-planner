package compile

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// AuthorityError reports that declared decision authority could not be used.
// Diagnostics are retained in their shared resolver order instead of reducing
// an unresolved ancestry to a misleading missing-citation finding.
type AuthorityError struct {
	Diagnostics []decisionview.Diagnostic
	Operational bool
}

func (e *AuthorityError) Error() string {
	kind := "invalid"
	if e.Operational {
		kind = "operationally unavailable"
	}
	parts := make([]string, 0, len(e.Diagnostics))
	for _, diagnostic := range e.Diagnostics {
		parts = append(parts, fmt.Sprintf("%s %s: %s", diagnostic.Code, diagnostic.Severity, diagnostic.Message))
	}
	return "decision authority is " + kind + ": " + strings.Join(parts, "; ")
}

type forkDecisionIntent struct {
	capture *decisionview.ConsumerCapture
	legacy  *decisionview.LegacyContext
}

func newForkDecisionIntent(root *rules.Root, artifactRel string) (*forkDecisionIntent, error) {
	capture := root.DecisionView
	if capture == nil {
		return nil, nil
	}
	var invalid []decisionview.Diagnostic
	operational := false
	for _, diagnostic := range capture.Diagnostics {
		if diagnostic.Severity != decisionview.Error && diagnostic.Severity != decisionview.Operational {
			continue
		}
		invalid = append(invalid, diagnostic)
		operational = operational || diagnostic.Severity == decisionview.Operational
	}
	if capture.View == nil || capture.View.Resolution != decisionview.ResolutionComplete {
		if len(invalid) == 0 {
			invalid = append(invalid, decisionview.Diagnostic{Code: "FDL020", Severity: decisionview.Error, Message: "Declared decision authority did not resolve completely"})
		}
	}
	if len(invalid) > 0 {
		return nil, &AuthorityError{Diagnostics: invalid, Operational: operational}
	}
	context := forkLegacyContext(capture.LegacyContexts, artifactRel)
	return &forkDecisionIntent{capture: capture, legacy: context}, nil
}

func forkLegacyContext(contexts []decisionview.LegacyContext, artifactRel string) *decisionview.LegacyContext {
	artifactRel = filepath.ToSlash(artifactRel)
	for i := range contexts {
		if contexts[i].Root == decisionview.SourceRootPlanning && filepath.ToSlash(contexts[i].Path) == artifactRel {
			return &contexts[i]
		}
	}
	return nil
}

func (f *forkDecisionIntent) lookup(cited string) (decisionview.CitationResult, bool) {
	if f == nil {
		return decisionview.CitationResult{}, false
	}
	legacy := (*decisionview.LegacyContext)(nil)
	if _, _, qualified := decisionview.ParseQualifiedID(cited); !qualified {
		legacy = f.legacy
	}
	result, err := decisionview.LookupReference(f.capture.View, cited, legacy)
	return result, err == nil
}

func (f *forkDecisionIntent) accepted(cited string) bool {
	result, ok := f.lookup(cited)
	return ok && result.Effective != nil && result.Effective.OriginalStatus == "accepted"
}

func (f *forkDecisionIntent) disposition(cited string) (status string, effective, found bool) {
	result, ok := f.lookup(cited)
	if !ok {
		return "", false, false
	}
	if result.Effective != nil {
		return result.Effective.OriginalStatus, true, true
	}
	return result.Original.OriginalStatus, false, true

}

func (f *forkDecisionIntent) ambiguous(cited string) []string {
	if f == nil || f.legacy != nil {
		return nil
	}
	if _, _, qualified := decisionview.ParseQualifiedID(cited); qualified {
		return nil
	}
	var suggestions []string
	for _, record := range f.capture.View.Records {
		_, id, ok := decisionview.ParseQualifiedID(string(record.ID))
		if ok && id == cited {
			suggestions = append(suggestions, string(record.ID))
		}
	}
	sort.Strings(suggestions)
	if len(suggestions) < 2 {
		return nil
	}
	return suggestions
}
