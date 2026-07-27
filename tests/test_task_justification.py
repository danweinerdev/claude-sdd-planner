"""Tests for the task `justifies` field — the necessity counterweight.

`verification` says how we know a task is done; `justifies` says why it should
be started at all. The validator enforces the two failure modes a script can
detect without judging content:

    SDD063  the field is absent or empty
    SDD076  the field is a placeholder ("might need it later")
    SDD077  the field only restates the task title

Whether a *stated* demand is a good one is the plan-reviewer's Scope lens, not
a script's call — so anything naming a requirement id or a concrete failure
passes here. These tests pin that boundary in both directions: real
justifications must not trip the checks (the false-positive risk that would
make the field noise), and non-answers must not slip through.
"""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))

from sdd_validate import (  # noqa: E402
    JUSTIFICATION_PLACEHOLDERS,
    REQUIRED_HEADINGS,
    Validator,
)


def task(justifies, title="Add token refresh", task_id="1.1"):
    return {
        "id": task_id,
        "title": title,
        "status": "planned",
        "verification": "pytest tests/test_auth.py — 8 pass incl. expiry case",
        "justifies": justifies,
    }


class JustificationHarness(unittest.TestCase):
    """Runs `_task_justification` in isolation and collects its diagnostics."""

    def codes(self, entry):
        validator = Validator(Path("."), Path("."))
        validator._task_justification(None, entry)
        return [diagnostic.code for diagnostic in validator.out]

    def assertAccepted(self, justifies, **kwargs):
        codes = self.codes(task(justifies, **kwargs))
        self.assertEqual(codes, [], f"unexpectedly rejected {justifies!r} with {codes}")

    def assertRejected(self, justifies, code, **kwargs):
        self.assertIn(code, self.codes(task(justifies, **kwargs)), f"accepted {justifies!r}")


class TestSourcedJustifications(JustificationHarness):
    """Real justifications must pass — false positives would make the field noise."""

    def test_requirement_id_citations_pass(self):
        for value in (
            "FR-03",
            "Satisfies FR-12 and AC-04.",
            "NFR-02 — p99 latency under 200ms.",
            "Implements the ledger shape decided in D-0004.",
        ):
            with self.subTest(value=value):
                self.assertAccepted(value)

    def test_concrete_failure_prevented_passes(self):
        for value in (
            "Prevents silent data loss when a partial write is retried.",
            "Without this, expired tokens 401 mid-session and drop the user's work.",
            "Stops duplicate charges when the webhook is redelivered.",
        ):
            with self.subTest(value=value):
                self.assertAccepted(value)

    def test_correctness_work_is_not_speculative(self):
        """Error handling and tests are load-bearing; naming the failure suffices."""
        for value in (
            "Prevents unbounded memory growth when the queue backs up.",
            "Covers the malformed-input path that currently panics.",
            "Rollback path — a failed migration otherwise leaves rows half-written.",
        ):
            with self.subTest(value=value):
                self.assertAccepted(value)

    def test_id_citation_outweighs_title_echo(self):
        """A cited requirement is sourced even if the prose mirrors the title."""
        self.assertAccepted("Add token refresh (FR-07)")

    def test_prose_containing_placeholder_words_is_not_flagged(self):
        """Anchoring must not catch ordinary prose that merely contains a word."""
        for value in (
            "Runs a completeness check against the upstream manifest (AC-02).",
            "The consistency guarantee breaks without this — readers see torn writes.",
            "Required by the vendor's API contract: unsigned requests are rejected.",
            # Words beginning with "na" must not trip the N/A stub pattern.
            "All three native completion APIs ship behind the portable fallback.",
            "Prevents the failure modes named in the design's error table.",
            "Narrows the blast radius of a corrupted bundle read.",
        ):
            with self.subTest(value=value):
                self.assertAccepted(value)


class TestPlaceholderJustifications(JustificationHarness):
    def test_deferred_need_placeholders_rejected(self):
        for value in (
            "Might need it later.",
            "We might need this in the future.",
            "May be needed later.",
            "Just in case.",
            "Future-proofing the interface.",
            "Future use.",
        ):
            with self.subTest(value=value):
                self.assertRejected(value, "SDD076")

    def test_shape_and_symmetry_placeholders_rejected(self):
        for value in (
            "For completeness.",
            "For symmetry with the reader implementation.",
            "For consistency with the other handlers.",
            "For parity.",
            "Nice to have.",
            "Part of the architecture.",
            "Part of the refactor.",
        ):
            with self.subTest(value=value):
                self.assertRejected(value, "SDD076")

    def test_appeal_to_practice_rejected(self):
        for value in ("Best practice.", "Good practice.", "Standard practice."):
            with self.subTest(value=value):
                self.assertRejected(value, "SDD076")

    def test_bare_assertions_and_stubs_rejected(self):
        for value in ("TBD", "TODO", "N/A", "NA", "Required.", "It is needed.", "Necessary."):
            with self.subTest(value=value):
                self.assertRejected(value, "SDD076")

    def test_placeholder_wins_over_title_echo(self):
        """One finding per task — a placeholder reports SDD076, not both."""
        codes = self.codes(task("Might need it later.", title="Might need it later"))
        self.assertEqual(codes, ["SDD076"])

    def test_placeholder_detected_mid_sentence(self):
        self.assertRejected("Adds the cache layer, might need it later.", "SDD076")

    def test_placeholder_is_case_insensitive(self):
        self.assertRejected("FOR COMPLETENESS", "SDD076")


class TestTitleEchoJustifications(JustificationHarness):
    def test_verbatim_restatement_rejected(self):
        self.assertRejected("Add token refresh", "SDD077")

    def test_restatement_survives_inflection_and_filler(self):
        for value in (
            "Adds the token refresh.",
            "To implement token refreshing.",
            "This task adds token refresh.",
        ):
            with self.subTest(value=value):
                self.assertRejected(value, "SDD077")

    def test_subset_of_title_rejected(self):
        """Saying less than the title is still saying nothing about why."""
        self.assertRejected("Token refresh.", "SDD077")

    def test_superset_of_title_passes(self):
        """Extra content words mean a demand may actually be stated."""
        self.assertAccepted("Sessions currently die at the one-hour mark without it.")

    def test_stemming_does_not_overreach(self):
        """Short words must not be stemmed into collisions ('caches' -> 'cach').

        Over-stemming produces false SDD077s, which is the failure mode that
        would make authors distrust the check.
        """
        self.assertAccepted(
            "Prevents a cold-start stall on the first request.",
            title="Add cache warming",
        )

    def test_distinct_words_sharing_a_stem_prefix_are_not_echoes(self):
        self.assertAccepted(
            "Bounds the retry budget so a flapping upstream cannot stall checkout.",
            title="Add retry logic",
        )

    def test_empty_title_does_not_trip_echo_check(self):
        self.assertAccepted("Keeps sessions alive past expiry.", title="")


class TestAbsentJustification(JustificationHarness):
    def test_absent_and_empty_defer_to_sdd063(self):
        """Absence is SDD063's finding in `_phase`; don't double-report it."""
        for value in (None, "", "   "):
            with self.subTest(value=value):
                entry = task("x")
                if value is None:
                    del entry["justifies"]
                else:
                    entry["justifies"] = value
                self.assertEqual(self.codes(entry), [])

    def test_non_string_justification_is_not_crashed_on(self):
        for value in (42, ["FR-01"], {"id": "FR-01"}, True):
            with self.subTest(value=value):
                entry = task("x")
                entry["justifies"] = value
                self.assertEqual(self.codes(entry), [])


class TestJustificationRegexAnchoring(unittest.TestCase):
    """The placeholder pattern is anchored so it matches phrases, not substrings."""

    def test_matches_at_string_start(self):
        self.assertTrue(JUSTIFICATION_PLACEHOLDERS.search("TBD"))

    def test_matches_after_punctuation_and_whitespace(self):
        for prefix in ("Adds it — ", "Adds it, ", "Adds it; ", "Adds it ("):
            with self.subTest(prefix=prefix):
                self.assertTrue(
                    JUSTIFICATION_PLACEHOLDERS.search(prefix + "for completeness")
                )

    def test_does_not_match_mid_word(self):
        for value in ("Nonstandard practices are rejected by the linter (FR-09).",):
            with self.subTest(value=value):
                match = JUSTIFICATION_PLACEHOLDERS.search(value)
                self.assertIsNone(
                    match,
                    f"matched {match.group(0)!r} inside a word" if match else "",
                )


class TestRequiredNonGoalsHeadings(unittest.TestCase):
    """Non-Goals is a scope boundary; it must survive spec -> design -> plan."""

    def test_plan_and_design_require_non_goals(self):
        for kind in ("plan", "design", "spec"):
            with self.subTest(kind=kind):
                self.assertIn("Non-Goals", REQUIRED_HEADINGS[kind])

    def test_phase_does_not_require_non_goals(self):
        """Boundaries are set at plan level, not repeated per phase."""
        self.assertNotIn("Non-Goals", REQUIRED_HEADINGS["phase"])


if __name__ == "__main__":
    unittest.main()
