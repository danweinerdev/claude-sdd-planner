"""Discovery must not load one ledger twice on a case-insensitive filesystem.

`discover()` probes both `DECISIONS.md` and `decisions.md` in the ledger's own
directory. On macOS and Windows both probes hit the same file, which used to
load that ledger twice and report every decision id as a cross-file duplicate
(DLG050), plus phantom DLG042/DLG043/DLG045 findings about a canonical ledger
that does not exist.
"""

import sys
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(REPO_ROOT / "scripts"))

from sdd_decision_validate import discover  # noqa: E402

LEDGER = """---
title: "Decision Ledger"
type: decision-log
status: active
created: 2026-01-01
updated: 2026-01-01
tags: [decisions]
related: []
decisions:
  - id: D-0001
    kind: decision
    status: accepted
    date: 2026-01-01
    decided_by: user
    statement: "A recorded truth."
    rationale: "Because it is needed for the fixture."
    scope: []
    tags: [fixture]
---
"""


def case_insensitive(directory: Path) -> bool:
    probe = directory / "CaseProbe.tmp"
    probe.write_text("", encoding="utf-8")
    try:
        return (directory / "caseprobe.tmp").exists()
    finally:
        probe.unlink()


class TestDiscoverCollapsesOneFile(unittest.TestCase):
    """One ledger on disk is one ledger to validate, whatever its spelling."""

    def _one_ledger(self, name: str) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            decisions = Path(tmp) / "planning" / "Decisions"
            decisions.mkdir(parents=True)
            ledger = decisions / name
            ledger.write_text(LEDGER, encoding="utf-8")

            found = discover(ledger)

            self.assertEqual(
                len(found),
                1,
                f"{name} discovered {len(found)} paths for one file: "
                f"{[str(p) for p in found]}",
            )
            self.assertTrue(found[0].samefile(ledger))

    def test_lowercase_ledger_is_discovered_once(self):
        self._one_ledger("decisions.md")

    def test_uppercase_ledger_is_discovered_once(self):
        self._one_ledger("DECISIONS.md")

    def test_survivor_keeps_the_real_on_disk_spelling(self):
        """The kept path must be the name the directory holds, not the probe.

        Sorting puts `DECISIONS.md` before `decisions.md`, so collapsing to an
        arbitrary survivor would relabel a canonical ledger as an external one
        and emit a spurious DLG042.
        """
        with tempfile.TemporaryDirectory() as tmp:
            decisions = Path(tmp) / "planning" / "Decisions"
            decisions.mkdir(parents=True)
            ledger = decisions / "decisions.md"
            ledger.write_text(LEDGER, encoding="utf-8")

            found = discover(ledger)

            self.assertEqual([p.name for p in found], ["decisions.md"])


class TestDiscoverKeepsGenuinelyDistinctLedgers(unittest.TestCase):
    """Case-folding would hide a real collision; inode identity does not."""

    def test_two_real_files_both_survive(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            if case_insensitive(root):
                self.skipTest("filesystem is case-insensitive; two spellings cannot coexist")
            decisions = root / "planning" / "Decisions"
            decisions.mkdir(parents=True)
            lower = decisions / "decisions.md"
            upper = decisions / "DECISIONS.md"
            lower.write_text(LEDGER, encoding="utf-8")
            upper.write_text(LEDGER, encoding="utf-8")

            found = discover(lower)

            self.assertEqual(len(found), 2, [str(p) for p in found])


if __name__ == "__main__":
    unittest.main()
