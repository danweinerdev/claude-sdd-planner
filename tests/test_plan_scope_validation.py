"""End-to-end validator tests for the necessity/scope enforcement.

`test_task_justification.py` unit-tests `_task_justification` in isolation.
These tests drive the real CLI over a real planning root built from the real
templates, which is the only way to catch the failures that live *between*
components: a template that stops satisfying the schema, a required heading
the validator enforces but no template supplies, or a diagnostic that fires in
isolation but is filtered out of the actual report.

The baseline case matters most. If `PlanningRoot()` — assembled entirely from
`shared/templates/` — ever stops validating clean, templates and validator have
drifted apart, which is exactly the CLAUDE.md maintenance rule this suite
exists to keep honest.
"""

import re
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from fixtures import (  # noqa: E402
    REPO_ROOT,
    PlanningRoot,
    Task,
    template_headings,
)

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))

from sdd_validate import BASELINE_IDEA, REQUIRED_HEADINGS  # noqa: E402


class TestBaselineFixtureIsValid(unittest.TestCase):
    """The templates must produce an artifact the validator accepts."""

    def test_default_planning_root_validates_clean(self):
        with PlanningRoot() as root:
            result = root.validate()
            self.assertTrue(result.valid, repr(result))
            self.assertEqual(result.codes, [], repr(result))

    def test_multiple_tasks_validate_clean(self):
        with PlanningRoot() as root:
            root.add_task(title="Add retry logic", justifies="Prevents duplicate charges after a redelivered webhook.")
            root.add_task(title="Add metrics", justifies="Prevents blind rollouts.")
            result = root.validate()
            self.assertTrue(result.valid, repr(result))


class TestTemplateValidatorAgreement(unittest.TestCase):
    """Templates and REQUIRED_HEADINGS must not drift apart."""

    CASES = {
        "plan-readme.md": "plan",
        "plan-phase.md": "phase",
        "spec.md": "spec",
        "design.md": "design",
        "brainstorm.md": "brainstorm",
        "research.md": "research",
    }

    def test_every_required_heading_is_supplied_by_its_template(self):
        for template, kind in self.CASES.items():
            with self.subTest(template=template):
                supplied = set(template_headings(template))
                missing = set(REQUIRED_HEADINGS[kind]) - supplied
                self.assertEqual(
                    missing,
                    set(),
                    f"{template} omits headings the validator requires: {sorted(missing)}",
                )

    def test_non_goals_survives_spec_design_and_plan(self):
        """A scope boundary set in a spec must have somewhere to land downstream."""
        for template in ("spec.md", "design.md", "plan-readme.md"):
            with self.subTest(template=template):
                self.assertIn("Non-Goals", template_headings(template))


class TestJustifiesEnforcedEndToEnd(unittest.TestCase):
    def test_missing_justifies_reports_sdd063(self):
        with PlanningRoot() as root:
            root.set_task(0, justifies=Task.DROP)
            result = root.validate()
            self.assertIn("SDD063", result.codes, repr(result))
            self.assertTrue(
                any("justifies" in message for message in result.messages("SDD063")),
                repr(result),
            )

    def test_placeholder_justifies_reports_sdd076(self):
        with PlanningRoot() as root:
            root.set_task(0, justifies="Might need it later.")
            result = root.validate()
            self.assertIn("SDD076", result.codes, repr(result))
            self.assertFalse(result.valid)

    def test_title_echo_justifies_reports_sdd077(self):
        with PlanningRoot() as root:
            root.set_task(0, title="Add retry logic", justifies="Adds the retry logic.")
            result = root.validate()
            self.assertIn("SDD077", result.codes, repr(result))

    def test_sourced_justifies_reports_nothing(self):
        with PlanningRoot() as root:
            root.set_task(0, justifies="Prevents mid-session logout when the token expires.")
            result = root.validate()
            self.assertNotIn("SDD076", result.codes, repr(result))
            self.assertNotIn("SDD077", result.codes, repr(result))
            self.assertTrue(result.valid, repr(result))

    def test_each_defective_task_is_reported_independently(self):
        with PlanningRoot() as root:
            root.set_task(0, justifies="Satisfies FR-01.")
            root.add_task(title="Add cache layer", justifies="For completeness.")
            root.add_task(title="Add metrics", justifies="Adds metrics.")
            result = root.validate()
            self.assertEqual(len(result.codes_for("SDD076")), 1, repr(result))
            self.assertEqual(len(result.codes_for("SDD077")), 1, repr(result))
            self.assertIn("1.2", result.messages("SDD076")[0])
            self.assertIn("1.3", result.messages("SDD077")[0])


class TestNonGoalsEnforcedEndToEnd(unittest.TestCase):
    def test_plan_without_non_goals_is_invalid(self):
        with PlanningRoot() as root:
            root.drop_section("Non-Goals")
            result = root.validate()
            self.assertIn("SDD020", result.codes, repr(result))
            self.assertTrue(
                any("Non-Goals" in message for message in result.messages("SDD020")),
                repr(result),
            )
            self.assertFalse(result.valid)

    def test_dropping_an_unrelated_section_does_not_blame_non_goals(self):
        """Guards the drop_section helper itself against over-matching."""
        with PlanningRoot() as root:
            root.drop_section("Key Decisions")
            result = root.validate()
            messages = " ".join(result.messages("SDD020"))
            self.assertIn("Key Decisions", messages, repr(result))
            self.assertNotIn("Non-Goals", messages, repr(result))


class TestBrainstormBaselineIdea(unittest.TestCase):
    """Idea 0 is the baseline every other option must beat.

    Enforced as a `candidate`, not an `error`: dropping the baseline is
    legitimate when inaction is impossible, and a draft in progress should not
    be blocked. The requirement is that the omission be visible, not silent.
    """

    def test_brainstorm_with_baseline_is_clean(self):
        with PlanningRoot() as root:
            root.write_brainstorm()
            result = root.validate()
            self.assertNotIn("SDD078", result.codes, repr(result))
            self.assertTrue(result.valid, repr(result))

    def test_brainstorm_without_baseline_reports_sdd078(self):
        with PlanningRoot() as root:
            root.write_brainstorm(ideas=["Idea 1: Build it", "Idea 2: Buy it"])
            result = root.validate()
            self.assertIn("SDD078", result.codes, repr(result))

    def test_missing_baseline_does_not_invalidate(self):
        """A candidate surfaces the omission without blocking the artifact."""
        with PlanningRoot() as root:
            root.write_brainstorm(ideas=["Idea 1: Build it"])
            result = root.validate()
            self.assertTrue(result.valid, repr(result))
            self.assertEqual(
                [item["severity"] for item in result.codes_for("SDD078")],
                ["candidate"],
                repr(result),
            )

    def test_renamed_but_genuine_baselines_are_accepted(self):
        """Match the baseline on intent, not on the template's exact wording."""
        for heading in (
            "Idea 0: Keep the nightly cron",
            "Option 0: Leave it as is",
            "Do nothing",
            "Approach 0: no change",
            "Status quo — absorb the toil manually",
            "Baseline: current behavior",
        ):
            with self.subTest(heading=heading):
                with PlanningRoot() as root:
                    root.write_brainstorm(ideas=[heading, "Idea 1: Build it"])
                    result = root.validate()
                    self.assertNotIn("SDD078", result.codes, repr(result))

    def test_prose_mentioning_status_quo_does_not_satisfy_the_check(self):
        """The baseline must be an idea that *is* the status quo, not one that
        merely mentions it. Each heading below names the status quo while
        proposing to leave it behind — accepting any of them would let a
        brainstorm skip the baseline while appearing to have one.
        """
        for heading in (
            "Idea 1: Build it, which beats the status quo by a mile",
            "Idea 1: Build the thing rather than preserve the status quo",
            "Idea 2: Migrate away from the status quo",
            "Idea 1: A faster cache with no change to the public API",
            "Idea 3: Replace the baseline scheduler with a queue",
        ):
            with self.subTest(heading=heading):
                self.assertIsNone(
                    BASELINE_IDEA.search(f"### {heading}"),
                    f"{heading!r} was accepted as a do-nothing baseline",
                )
                with PlanningRoot() as root:
                    root.write_brainstorm(ideas=[heading])
                    self.assertIn("SDD078", root.validate().codes)

    def test_baseline_absent_when_ideas_section_missing_is_not_double_reported(self):
        """A missing `## Ideas` section is SDD020's finding, not SDD078's."""
        with PlanningRoot() as root:
            root.write_brainstorm()
            root.drop_section("Ideas", document="brainstorm")
            result = root.validate()
            self.assertIn("SDD020", result.codes, repr(result))
            self.assertNotIn("SDD078", result.codes, repr(result))


class TestBrainstormTemplateSuppliesBaseline(unittest.TestCase):
    def test_template_declares_idea_zero(self):
        headings = template_headings("brainstorm.md", level=3)
        self.assertTrue(
            any(re.match(r"Idea 0\b", heading) for heading in headings),
            f"brainstorm.md must supply a do-nothing baseline; found {headings}",
        )

    def test_template_baseline_satisfies_the_validator(self):
        """The shipped template must not itself trip SDD078."""
        text = (REPO_ROOT / "shared" / "templates" / "brainstorm.md").read_text(
            encoding="utf-8"
        )
        self.assertIsNotNone(BASELINE_IDEA.search(text))


if __name__ == "__main__":
    unittest.main()
