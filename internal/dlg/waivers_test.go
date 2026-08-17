package dlg

import "testing"

// ledgerWithMeta builds a single-ledger fixture whose frontmatter carries the
// given meta — the waiver tests need `waivers:`, which ledgerWith omits.
func ledgerWithMeta(meta map[string]any, entries ...map[string]any) ([]*Ledger, map[string][]map[string]any) {
	l := &Ledger{Path: "Decisions/decisions.md", Source: "", Meta: meta}
	return []*Ledger{l}, map[string][]map[string]any{l.Path: entries}
}

func severities(ds []Diagnostic) map[string]Severity {
	out := map[string]Severity{}
	for _, d := range ds {
		out[d.Code] = d.Severity
	}
	return out
}

// A gap and an ordering defect are real, but a legacy ledger cannot repair
// them: append-only rules forbid the renumbering their fix would require. They
// are warnings so the ledger stays usable, and the exit status stays clean.
func TestSequencingFindingsAreWarnings(t *testing.T) {
	ledgers, entries := ledgerWithMeta(map[string]any{},
		entry(map[string]any{"id": "D-0003", "status": "accepted"}),
		entry(map[string]any{"id": "D-0001", "status": "accepted"}),
	)
	got := severities(ValidateCollection(ledgers, entries))
	for _, code := range []string{"DLG064", "DLG065"} {
		if got[code] != Warning {
			t.Errorf("%s severity = %q, want warning", code, got[code])
		}
		if got[code].Invalidating() {
			t.Errorf("%s must not invalidate the ledger", code)
		}
	}
}

// A reasoned waiver silences a warning, and the finding is still REPORTED as
// waived — "nothing found" and "found and excused" must never look the same.
func TestWaiverSilencesSequencingWarning(t *testing.T) {
	meta := map[string]any{"waivers": []any{
		map[string]any{"code": "DLG064", "reason": "gap predates this ledger's sequencing enforcement"},
	}}
	ledgers, entries := ledgerWithMeta(meta,
		entry(map[string]any{"id": "D-0003", "status": "accepted"}),
	)
	got := applyWaivers(ledgers, ValidateCollection(ledgers, entries))
	sev := severities(got)
	if sev["DLG064"] != Waived {
		t.Errorf("DLG064 severity = %q, want waived", sev["DLG064"])
	}
	found := false
	for _, d := range got {
		if d.Code == "DLG064" {
			found = true
			if d.Correction == "" || d.Correction[:7] != "waived:" {
				t.Errorf("a waived finding must carry its reason; got %q", d.Correction)
			}
		}
	}
	if !found {
		t.Error("a waived finding must still be reported, not dropped")
	}
}

// The guardrails: an unexplained waiver, and one aimed at a code whose
// condition means the ledger cannot be trusted, are both refused as errors.
func TestMalformedWaiverIsRefused(t *testing.T) {
	cases := []struct {
		name   string
		waiver map[string]any
	}{
		{"no reason", map[string]any{"code": "DLG064", "reason": ""}},
		{"placeholder reason", map[string]any{"code": "DLG064", "reason": "legacy"}},
		{"too short to be a reason", map[string]any{"code": "DLG064", "reason": "old ledger"}},
		{"unwaivable code", map[string]any{"code": "DLG010", "reason": "we do not want this check at all"}},
		{"no code", map[string]any{"reason": "a perfectly good reason for nothing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]any{"waivers": []any{tc.waiver}}
			ledgers, entries := ledgerWithMeta(meta,
				entry(map[string]any{"id": "D-0003", "status": "accepted"}))
			got := applyWaivers(ledgers, ValidateCollection(ledgers, entries))
			sev := severities(got)
			if sev["DLG078"] != Error {
				t.Errorf("a malformed waiver must be an error; severities=%v", sev)
			}
			if sev["DLG064"] == Waived {
				t.Error("a malformed waiver must not silence anything")
			}
		})
	}
}

// A waiver outliving the condition it excused silently disables a check, so
// it is reported — as a warning, since a stale exception is untidy, not unsafe.
func TestStaleWaiverIsReported(t *testing.T) {
	meta := map[string]any{"waivers": []any{
		map[string]any{"code": "DLG065", "reason": "inherited ordering we cannot rewrite in place"},
	}}
	// Sequential and in order: neither sequencing rule fires.
	ledgers, entries := ledgerWithMeta(meta,
		entry(map[string]any{"id": "D-0001", "status": "accepted"}),
		entry(map[string]any{"id": "D-0002", "status": "accepted"}),
	)
	got := applyWaivers(ledgers, ValidateCollection(ledgers, entries))
	sev := severities(got)
	if sev["DLG079"] != Warning {
		t.Errorf("a stale waiver must be reported as a warning; severities=%v", sev)
	}
}
