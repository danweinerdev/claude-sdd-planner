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
func runWith(r *Root, rules []*Rule) []Diagnostic {
	ordinary := evaluate(r, rules)
	out := append([]Diagnostic(nil), ordinary...)
	out = append(out, waiverFindings(r, ordinary, rules)...)
	sortStrict(out)
	return out
}

// runWithWaiversWith is RunWithWaivers over an explicit rule list: reporting
// mode. Accepted exceptions re-tag matched errors as Waived, bookkeeping
// findings are appended, and findings on retired artifacts are demoted.
func runWithWaiversWith(r *Root, rules []*Rule) []Diagnostic {
	diags := evaluate(r, rules)
	diags = append(diags, applyWaivers(r, diags)...)
	diags = demoteRetiredFindings(r, diags)
	SortDiagnostics(diags)
	return diags
}

// evaluate is the ordinary sweep: every root rule once, every artifact rule
// once per artifact, waiver-bookkeeping rules excluded.
func evaluate(r *Root, rules []*Rule) []Diagnostic {
	var out []Diagnostic
	emit := func(d Diagnostic) { out = append(out, d) }
	for _, rule := range rules {
		if rule.CheckRoot != nil && !waiverRuleCodes[rule.Code] {
			rule.CheckRoot(r, emit)
		}
	}
	for _, a := range r.Artifacts {
		for _, rule := range rules {
			if rule.Check != nil && !waiverRuleCodes[rule.Code] {
				rule.Check(a, emit)
			}
		}
	}
	return out
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
