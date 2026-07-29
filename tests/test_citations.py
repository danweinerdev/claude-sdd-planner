"""End-to-end tests for `_citations` scanning frontmatter task fields.

`_citations` (SDD120/SDD121 for decision ids, SDD122 for FR/NFR/AC ids) used
to scan only the Markdown body, so a dangling id cited inside a task's
`justifies` or `verification` frontmatter field — which the schema *requires*
to carry citations — went unvalidated. These tests pin the fix: a phase doc's
task frontmatter is scanned the same as its body.
"""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from fixtures import PlanningRoot  # noqa: E402

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
---
"""

SPEC = """---
title: "Demo Spec"
type: spec
status: draft
created: 2026-01-01
updated: 2026-01-01
tags: []
related: []
---

# Demo Spec

## Overview
Fixture content.

## Goals
- Fixture content.

## Non-Goals
- Fixture content.

## Requirements

### Functional Requirements
- **FR-01**: Fixture requirement.

### Non-Functional Requirements
- **NFR-01**: Fixture requirement.

## User Stories
- Fixture content.

## Acceptance Criteria
- [ ] **AC-01**: Fixture content.

## Constraints
- Fixture content.

## Dependencies
- Fixture content.

## Open Questions
- Fixture content.
"""


class FrontmatterCitationRoot:
    """A `PlanningRoot` wrapper that also seeds a ledger and a related spec."""

    def __init__(self) -> None:
        self.root = PlanningRoot()

    def __enter__(self) -> PlanningRoot:
        root = self.root
        root.write("Decisions/decisions.md", LEDGER)
        root.write("Specs/DemoSpec/README.md", SPEC)
        text = root.readme.read_text(encoding="utf-8")
        assert "related: []" in text
        text = text.replace("related: []", 'related: ["Specs/DemoSpec"]', 1)
        root.readme.write_text(text, encoding="utf-8")
        return root

    def __exit__(self, *exc: object) -> None:
        self.root.cleanup()


class TestFrontmatterDecisionCitations(unittest.TestCase):
    def test_dangling_decision_in_justifies_reports_sdd120(self):
        with FrontmatterCitationRoot() as root:
            root.set_task(0, justifies="Implements the ledger shape decided in D-0002.")
            result = root.validate()
            self.assertIn("SDD120", result.codes, repr(result))


class TestFrontmatterRequirementCitations(unittest.TestCase):
    def test_dangling_requirement_in_verification_reports_sdd122(self):
        with FrontmatterCitationRoot() as root:
            root.set_task(0, verification="pytest tests/test_x.py — covers FR-02.")
            result = root.validate()
            self.assertIn("SDD122", result.codes, repr(result))


class TestFrontmatterResolvedCitations(unittest.TestCase):
    def test_resolved_decision_and_requirement_report_nothing(self):
        with FrontmatterCitationRoot() as root:
            root.set_task(
                0,
                justifies="Implements the ledger shape decided in D-0001.",
                verification="pytest tests/test_x.py — covers FR-01.",
            )
            result = root.validate()
            self.assertNotIn("SDD120", result.codes, repr(result))
            self.assertNotIn("SDD121", result.codes, repr(result))
            self.assertNotIn("SDD122", result.codes, repr(result))


class TestFrontmatterCitationLineNumbers(unittest.TestCase):
    def test_frontmatter_only_citation_reports_its_yaml_line(self):
        """A YAML-only id must point at its frontmatter line, not line 1."""
        with FrontmatterCitationRoot() as root:
            root.set_task(0, justifies="Implements the ledger shape decided in D-0002.")
            result = root.validate()
            findings = result.codes_for("SDD120")
            self.assertTrue(findings, repr(result))
            self.assertGreater(findings[0]["line"], 1, repr(result))


class TestCompletedTaskVerificationIsHistorical(unittest.TestCase):
    """A completed task's `verification` records what was true at completion.

    Its citations were resolved when the task completed; a decision superseded
    or a spec reshuffled afterwards must not retroactively flag the record.
    `justifies` stays live even on completed tasks — it states why the work
    exists, which remains a claim about the present.
    """

    def test_completed_task_verification_is_not_scanned(self):
        with FrontmatterCitationRoot() as root:
            root.set_task(
                0,
                status="complete",
                verification="pytest tests/test_x.py — covers FR-02.",
            )
            result = root.validate()
            self.assertNotIn("SDD122", result.codes, repr(result))

    def test_completed_task_justifies_is_still_scanned(self):
        with FrontmatterCitationRoot() as root:
            root.set_task(
                0,
                status="complete",
                justifies="Implements the ledger shape decided in D-0002.",
            )
            result = root.validate()
            self.assertIn("SDD120", result.codes, repr(result))


if __name__ == "__main__":
    unittest.main()
