package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The waiver mechanism is the one place in the validator where a human can
// switch a gate off. These tests exist to pin the ways that must NOT be
// possible, more than the happy path: every assertion below is a hole someone
// could otherwise open by accident.

// researchArtifact builds a research artifact with the given waivers block.
// When complete is false it omits the required sections, which makes SDD020
// fire — the finding these tests waive.
func researchArtifact(complete bool, waivers string) string {
	s := "---\n" +
		"title: \"Topic\"\ntype: research\nstatus: draft\n" +
		"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\n" +
		"tags: [\"a\"]\nrelated: []\n"
	if waivers != "" {
		s += waivers
	}
	s += "---\n\n# Topic\n\n## Summary\n\nText.\n"
	if complete {
		for _, h := range []string{"Context", "Findings", "Analysis", "Open Questions"} {
			s += "\n## " + h + "\n\nText.\n"
		}
	}
	return s
}

// findDiag returns the first diagnostic with the given code, or nil.
func findDiag(ds []Diagnostic, code string) *Diagnostic {
	for i := range ds {
		if ds[i].Code == code {
			return &ds[i]
		}
	}
	return nil
}

func countSeverity(ds []Diagnostic, s Severity) int {
	n := 0
	for _, d := range ds {
		if d.Severity == s {
			n++
		}
	}
	return n
}

// rootFrom writes files into a temp planning root and loads it. It reuses the
// example harness's fixture semantics so waiver tests and rule examples agree
// on what a planning root looks like.
func rootFrom(t *testing.T, files map[string]string) *Root {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := LoadRoot(dir)
	if err != nil {
		t.Fatalf("LoadRoot: %v", err)
	}
	return r
}

func TestWaiverExcusesMatchingFinding(t *testing.T) {
	// SDD020 fires on a missing required section. With a reasoned waiver it must still be
	// reported — as waived, with the reason — and must not invalidate.
	waiver := "waivers:\n  - code: SDD020\n    reason: \"This legacy research note predates the required-section layout.\"\n"
	r := rootFrom(t, map[string]string{"Research/topic.md": researchArtifact(false, waiver)})

	strict := Run(r)
	if d := findDiag(strict, "SDD020"); d == nil || d.Severity != Error {
		t.Fatalf("precondition: SDD030 must be an error without waivers, got %v", strict)
	}

	got := RunWithWaivers(r)
	d := findDiag(got, "SDD020")
	if d == nil {
		t.Fatal("waived finding disappeared; it must still be reported")
	}
	if d.Severity != Waived {
		t.Errorf("severity = %q, want %q", d.Severity, Waived)
	}
	if !strings.Contains(d.WaivedReason, "This legacy research") {
		t.Errorf("waived reason not carried on the diagnostic: %q", d.WaivedReason)
	}
	if n := countSeverity(got, Error); n != 0 {
		t.Errorf("waived root still reports %d error(s): %v", n, got)
	}
}

func TestWaiverDoesNotSuppressOtherCodes(t *testing.T) {
	// A waiver is per-code. Waiving one finding must not quiet an unrelated one
	// on the same artifact.
	waiver := "waivers:\n  - code: SDD020\n    reason: \"This legacy research note predates the required-section layout.\"\n"
	// A second artifact with an unwaived finding of a different code.
	r := rootFrom(t, map[string]string{
		"Research/topic.md": researchArtifact(false, waiver),
		"Research/other.md": researchArtifact(false, ""),
	})

	got := RunWithWaivers(r)
	if countSeverity(got, Error) == 0 {
		t.Fatalf("waiving SDD020 also silenced unrelated findings: %v", got)
	}
}

func TestWaiverIsScopedToItsArtifact(t *testing.T) {
	// An exception written in one document must not reach another. Blast radius
	// should be visible from the file the waiver lives in.
	waiver := "waivers:\n  - code: SDD020\n    reason: \"This legacy research note predates the required-section layout.\"\n"
	r := rootFrom(t, map[string]string{
		"Research/waived.md": researchArtifact(false, waiver),
		"Research/other.md":  researchArtifact(false, ""),
	})

	got := RunWithWaivers(r)
	var otherStillErrors bool
	for _, d := range got {
		if d.Path == "Research/other.md" && d.Code == "SDD020" && d.Severity == Error {
			otherStillErrors = true
		}
		if d.Path == "Research/other.md" && d.Severity == Waived {
			t.Errorf("a waiver in another artifact excused %s here", d.Code)
		}
	}
	if !otherStillErrors {
		t.Error("the unwaived artifact's finding was excused by a foreign waiver")
	}
}

func TestUnexplainedWaiverIsRefused(t *testing.T) {
	// The whole point is the written justification. Placeholder and too-short
	// reasons must invalidate the waiver, leaving the original finding an
	// error, and report SDD176.
	for _, reason := range []string{"TBD", "n/a", "-", "known issue", ""} {
		waiver := "waivers:\n  - code: SDD020\n    reason: \"" + reason + "\"\n"
		r := rootFrom(t, map[string]string{"Research/topic.md": researchArtifact(false, waiver)})
		got := RunWithWaivers(r)

		if d := findDiag(got, "SDD020"); d == nil || d.Severity != Error {
			t.Errorf("reason %q: the finding was excused by an unexplained waiver", reason)
		}
		if findDiag(got, "SDD176") == nil {
			t.Errorf("reason %q: expected SDD176 for the malformed waiver, got %v", reason, severityCodes(got))
		}
	}
}

func TestParseCodesCannotBeWaived(t *testing.T) {
	// Waiving a parse failure would assert that an unmodelable artifact is
	// fine, while every rule that silently did not run on it stays not-run.
	waiver := "waivers:\n  - code: SDD006\n    reason: \"We accept that this file's frontmatter cannot be parsed.\"\n"
	r := rootFrom(t, map[string]string{"Research/topic.md": researchArtifact(false, waiver)})
	got := RunWithWaivers(r)

	d := findDiag(got, "SDD176")
	if d == nil {
		t.Fatalf("expected SDD176 refusing an unwaivable code, got %v", severityCodes(got))
	}
	if !strings.Contains(d.Message, "cannot be waived") {
		t.Errorf("message does not explain unwaivability: %q", d.Message)
	}
}

func TestUnknownCodeWaiverIsRefused(t *testing.T) {
	// A typo'd or retired code yields a waiver that can never match. Silently
	// keeping it leaves a permanent no-op in the artifact.
	waiver := "waivers:\n  - code: SDD999\n    reason: \"This code does not exist and never will.\"\n"
	r := rootFrom(t, map[string]string{"Research/topic.md": researchArtifact(false, waiver)})

	if findDiag(RunWithWaivers(r), "SDD176") == nil {
		t.Error("a waiver naming an unknown code was accepted")
	}
}

func TestStaleWaiverIsReported(t *testing.T) {
	// The artifact is clean, so the waiver matches nothing. Left alone it would
	// silently excuse the finding if it ever returned.
	waiver := "waivers:\n  - code: SDD020\n    reason: \"Kept long after the underlying finding was fixed.\"\n"
	r := rootFrom(t, map[string]string{"Research/topic.md": researchArtifact(true, waiver)})

	got := RunWithWaivers(r)
	d := findDiag(got, "SDD177")
	if d == nil {
		t.Fatalf("expected SDD177 for a stale waiver, got %v", severityCodes(got))
	}
	if d.Severity != Error {
		t.Errorf("a stale waiver must be an error, got %q", d.Severity)
	}
}

func TestRunIgnoresWaiversEntirely(t *testing.T) {
	// `Run` is what the parity oracle and --no-waivers use. It must report the
	// unexcused truth, so waivers can never hide the real state of a root.
	waiver := "waivers:\n  - code: SDD020\n    reason: \"This legacy research note predates the required-section layout.\"\n"
	r := rootFrom(t, map[string]string{"Research/topic.md": researchArtifact(false, waiver)})

	for _, d := range Run(r) {
		if d.Severity == Waived {
			t.Fatal("Run applied a waiver; it must report the strict, unexcused state")
		}
	}
	if d := findDiag(Run(r), "SDD020"); d == nil || d.Severity != Error {
		t.Error("Run did not report the underlying finding as an error")
	}
}

func TestWaiverCannotDowngradeCandidate(t *testing.T) {
	// Candidates are already advisory. Marking one waived would misreport an
	// unblocking finding as an accepted exception.
	r := rootFrom(t, map[string]string{"Research/topic.md": researchArtifact(false, "")})
	for _, d := range RunWithWaivers(r) {
		if d.Severity == Waived {
			t.Fatalf("unexpected waived severity with no waivers declared: %v", d)
		}
	}
}


// severityCodes renders findings as severity:code, so a failure message shows
// whether a diagnostic was excused rather than only which one fired.
func severityCodes(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, string(d.Severity)+":"+d.Code)
	}
	return out
}
