"""Regression tests for `Final aligned review` phase-evidence parsing.

Guards against the v1.11.0/v1.12.0 defect where `_phase_final_review` applied
`markdown_scalar()` to the whole evidence value before parsing. When both the
path and the frozen identity were backticked, the value began and ended with a
backtick, so the outer pair was stripped and the inner backticks were orphaned:

    `PATH`; frozen: `SHA`   ->   PATH`; frozen: `SHA   ->   ('PATH`', '`SHA')

The mangled path then failed to resolve and emitted a misleading SDD166,
masking the downstream SDD173/SDD174 checks.
"""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))

from sdd_validate import (  # noqa: E402
    evidence_values,
    markdown_scalar,
    parse_final_aligned_review,
)

PATH = "Plans/BuildMCP/reviews/09-buildmcp-phase-2-gate-7e3d037.md"
FROZEN = "0a5517345acf8a43448a1360a7babd63a30ffc66..7e3d0374112ed1c1bd128ab6dc57db12a3f07508"

PERMUTATIONS = {
    "neither": f"{PATH}; frozen: {FROZEN}",
    "path-only": f"`{PATH}`; frozen: {FROZEN}",
    "frozen-only": f"{PATH}; frozen: `{FROZEN}`",
    "both": f"`{PATH}`; frozen: `{FROZEN}`",
}


def phase_body(value: str) -> str:
    return "\n".join(
        [
            "## Phase Completion Evidence",
            "",
            f"- Final aligned review: {value}",
            "",
        ]
    )


class TestParseFinalAlignedReview(unittest.TestCase):
    def test_all_backtick_permutations_parse(self):
        for name, value in PERMUTATIONS.items():
            with self.subTest(permutation=name):
                self.assertEqual(parse_final_aligned_review(value), (PATH, FROZEN))

    def test_caller_path_all_permutations(self):
        """Mirror `_phase_final_review`: extract the value, then parse it raw."""
        for name, value in PERMUTATIONS.items():
            with self.subTest(permutation=name):
                values = evidence_values(phase_body(value), "Final aligned review")
                self.assertEqual(len(values), 1)
                self.assertEqual(parse_final_aligned_review(values[0]), (PATH, FROZEN))

    def test_double_scalar_regression(self):
        """The old caller pre-stripped the value; that is what corrupted `both`."""
        buggy = parse_final_aligned_review(markdown_scalar(PERMUTATIONS["both"]))
        self.assertNotEqual(buggy, (PATH, FROZEN))
        self.assertEqual(buggy, (f"{PATH}`", f"`{FROZEN}"))

    def test_malformed_values_rejected(self):
        for value in ("", f"{PATH} frozen: {FROZEN}", PATH, f"; frozen: {FROZEN}"):
            with self.subTest(value=value):
                self.assertIsNone(parse_final_aligned_review(value))


if __name__ == "__main__":
    unittest.main()
