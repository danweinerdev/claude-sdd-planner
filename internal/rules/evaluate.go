package rules

// Evaluation over an explicit rule list.
//
// Run and RunWithWaivers evaluate the global registry; these functions are the
// seam beneath them that takes the rule list as a parameter, so a test can
// count invocations or permute order without mutating the registry under
// parallel tests (Designs/TestSuiteReliability DD-6).
//
// One ordinary sweep per evaluation: every root rule runs once and every
// artifact rule once per artifact. The waiver-bookkeeping rules (SDD176/177)
// never sweep; their findings derive from that single result, scoped to the
// rule list actually evaluated — a waiver whose code was not evaluated here
// matched nothing here.

// runWith is Run over an explicit rule list: strict mode. Ordinary findings
// keep their severity (accepted exceptions are ignored), and the bookkeeping
// findings are appended from the same sweep.
func runWith(r *Root, rules []*Rule) ([]Diagnostic, error) {
	ordinary, err := evaluate(r, rules)
	if err != nil {
		return nil, err
	}
	out := append([]Diagnostic(nil), ordinary...)
	out = append(out, waiverFindings(r, ordinary, rules)...)
	sortStrict(out)
	return out, nil
}

// runWithWaiversWith is RunWithWaivers over an explicit rule list: reporting
// mode. Accepted exceptions re-tag matched errors as Waived, bookkeeping
// findings are appended, and findings on retired artifacts are demoted.
func runWithWaiversWith(r *Root, rules []*Rule) ([]Diagnostic, error) {
	diags, err := evaluate(r, rules)
	if err != nil {
		return nil, err
	}
	diags = append(diags, applyWaivers(r, diags)...)
	diags = demoteRetiredFindings(r, diags)
	SortDiagnostics(diags)
	return diags, nil
}

// evaluate is the ordinary sweep: every root rule once, every artifact rule
// once per artifact, waiver-bookkeeping rules excluded.
//
// After every callback the evaluation's operational collector is checked;
// the first recorded failure aborts the sweep before the next callback and
// discards every partial finding. A rule's own `continue` cannot erase it:
// the failure was recorded by the adapter, not reported by the rule.
func evaluate(r *Root, rules []*Rule) ([]Diagnostic, error) {
	if err := r.OperationalFailure(); err != nil {
		return nil, err
	}
	var out []Diagnostic
	emit := func(d Diagnostic) { out = append(out, d) }
	for _, rule := range rules {
		if rule.CheckRoot != nil && !waiverRuleCodes[rule.Code] {
			rule.CheckRoot(r, emit)
			if err := r.OperationalFailure(); err != nil {
				return nil, err
			}
		}
	}
	for _, a := range r.Artifacts {
		for _, rule := range rules {
			if rule.Check != nil && !waiverRuleCodes[rule.Code] {
				rule.Check(a, emit)
				if err := r.OperationalFailure(); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

// OperationalCode is the diagnostic code the unchecked entry points (Run,
// RunWithWaivers) emit when the evaluation could not complete. It is the
// only finding such a run reports, at Operational severity, so no consumer
// can mistake an aborted evaluation for a clean one.
const OperationalCode = "SDD198"

func operationalDiagnostic(err error) Diagnostic {
	return Diagnostic{
		Code: OperationalCode, Severity: Operational, Path: ".", Line: 1,
		Message: "Validation could not complete: " + err.Error() + ".",
		Correction: "Make the version-control executable runnable (PATH, permissions, deadline) " +
			"and rerun. No finding is authoritative until the evaluation completes.",
	}
}

func init() {
	Register(&Rule{
		Code: OperationalCode, Severity: Error, Native: true,
		What: "the evaluation could not complete because the VCS could not be consulted (reported at operational severity)",
		// The evaluator aborts before this could run; the check exists so a
		// direct invocation of the rule in isolation still reports a failure
		// already recorded on the Root.
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			if err := r.OperationalFailure(); err != nil {
				emit(operationalDiagnostic(err))
			}
		},
		UnexampledReason: "the example harness cannot remove git from the environment; " +
			"TestIgnoredRepoErrorAbortsEvaluation covers the emitted diagnostic directly",
	})
}

// waiverFindings derives the bookkeeping findings (for whichever of SDD176 and
// SDD177 are in the rule list) from an evaluation's ordinary result. It works
// on a copy: applyWaivers re-tags matched findings in place, and strict mode
// must keep reporting them as errors.
func waiverFindings(r *Root, ordinary []Diagnostic, rules []*Rule) []Diagnostic {
	wanted := map[string]bool{}
	for _, rule := range rules {
		if waiverRuleCodes[rule.Code] {
			wanted[rule.Code] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	scratch := append([]Diagnostic(nil), ordinary...)
	var out []Diagnostic
	for _, d := range applyWaivers(r, scratch) {
		if wanted[d.Code] {
			out = append(out, d)
		}
	}
	return out
}

// RunChecked is Run with the operational failure distinguished from the
// findings: when the VCS could not be consulted, it returns (nil, err)
// wrapping vcs.ErrOperational and no partial findings.
func RunChecked(r *Root) ([]Diagnostic, error) { return runWith(r, All()) }

// RunWithWaiversChecked is RunWithWaivers with the same distinction.
func RunWithWaiversChecked(r *Root) ([]Diagnostic, error) { return runWithWaiversWith(r, All()) }
