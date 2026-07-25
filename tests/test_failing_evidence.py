"""Regression tests for SDD073 — failing evidence outside a result row.

The original check was:

    re.search(r"\\b(?:FAIL|FAILED|exit\\s+[1-9]\\d*)\\b", visible_body, re.IGNORECASE)

which produced false positives on ordinary prose, because:

  - `re.IGNORECASE` matched the English word "fail", not just the uppercase
    token test runners emit;
  - `\\b` treats a hyphen as a word boundary, so `fail-closed`, `fail-fast`,
    and `fail-safe` — names of *design properties* — matched `FAIL`;
  - it scanned the whole evidence body, including the `Observable evidence`
    column of rows whose Result cell SDD072 already checks exactly.

The worst case was `0 failed, 42 passed` — passing pytest/jest output reported
as failing evidence.

These tests pin both directions: failure-shaped prose must not trip the check,
and genuinely failing output must still be caught.
"""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))

from sdd_validate import (  # noqa: E402
    FAILING_EVIDENCE,
    strip_evidence_rows,
)


def flags(text: str) -> bool:
    """Mirror the caller: strip result rows, then scan what narration remains."""
    return bool(FAILING_EVIDENCE.search(strip_evidence_rows(text)))


class TestLegitimateProseIsNotFlagged(unittest.TestCase):
    def test_hyphenated_design_properties_pass(self):
        """`fail-closed` names a property, not a result — the reported bug."""
        for value in (
            "Verified the guard is fail-closed on a malformed token.",
            "Added fail-fast validation at startup.",
            "Confirms fail-safe rollback when the migration aborts.",
            "Switched the middleware from fail-open to fail-closed.",
            "Documented the FAIL-CLOSED default in the runbook.",
        ):
            with self.subTest(value=value):
                self.assertFalse(flags(value))

    def test_lowercase_english_usage_passes(self):
        for value in (
            "No tests fail.",
            "0 failed, 42 passed",
            "Covers the failure path that previously panicked.",
            "Tested failover between replicas.",
            "The failing-test-first workflow (TDD) was followed.",
            "Renamed `test_fail_open` to `test_fail_closed`.",
            "Retries use exponential backoff before failing over.",
        ):
            with self.subTest(value=value):
                self.assertFalse(flags(value))

    def test_passing_summary_lines_pass(self):
        """Runner summaries that mention failures while reporting success."""
        for value in (
            "PASS (exit 0) — 128 passed, 0 failed, 2 skipped",
            "Test Suites: 12 passed, 0 failed, 12 total",
            "ok — 0 failed",
        ):
            with self.subTest(value=value):
                self.assertFalse(flags(value))

    def test_exit_zero_passes(self):
        self.assertFalse(flags("pytest tests/ — PASS (exit 0)"))

    def test_words_containing_fail_pass(self):
        for value in ("failsafe mode", "The FAILOVER path is covered.", "unfailing"):
            with self.subTest(value=value):
                self.assertFalse(flags(value))


class TestGenuineFailuresAreFlagged(unittest.TestCase):
    def test_uppercase_result_tokens_flagged(self):
        for value in (
            "cargo test — FAILED",
            "Build result: FAIL",
            "FAILED (errors=3)",
        ):
            with self.subTest(value=value):
                self.assertTrue(flags(value))

    def test_nonzero_exit_flagged(self):
        for value in ("make build — exit 2", "npm run lint — exit 1"):
            with self.subTest(value=value):
                self.assertTrue(flags(value))

    def test_pasted_runner_output_flagged(self):
        self.assertTrue(
            flags("Ran 41 tests in 0.6s\n\nFAILED (failures=1)\nmake: *** Error 1")
        )


class TestResultRowsAreLeftToSDD072(unittest.TestCase):
    """SDD073 is the backstop for output *outside* rows; rows are SDD072's."""

    ROWS = (
        "| Command | Working directory | Result | Observable evidence |\n"
        "| --- | --- | --- | --- |\n"
        "| pytest | . | FAIL (exit 1) | 3 assertions failed |\n"
    )

    def test_row_content_is_stripped(self):
        self.assertFalse(flags(self.ROWS))

    def test_observable_evidence_column_does_not_trigger(self):
        passing = (
            "| Command | Working directory | Result | Observable evidence |\n"
            "| --- | --- | --- | --- |\n"
            "| pytest | . | PASS (exit 0) | 42 passed, 0 failed |\n"
        )
        self.assertFalse(flags(passing))

    def test_failure_outside_a_row_is_still_caught(self):
        body = self.ROWS + "\nRe-ran after the fix: still FAILED on CI.\n"
        self.assertTrue(flags(body))

    def test_strip_keeps_non_table_lines(self):
        kept = strip_evidence_rows("before\n| a | b |\nafter")
        self.assertEqual(kept.splitlines(), ["before", "after"])

    def test_strip_handles_indented_rows(self):
        self.assertEqual(strip_evidence_rows("  | a | b |").strip(), "")


if __name__ == "__main__":
    unittest.main()
