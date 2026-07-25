"""Reusable planning-root fixtures for end-to-end validator tests.

Unit tests can call a single validator method with a hand-built dict. Anything
that asserts on *whole-artifact* behavior — required headings, task field
enforcement, cross-document graph checks — needs a real planning root on disk,
because that is the only thing `sdd_validate.py` knows how to read.

Building one inline per test produces long, near-duplicate heredocs where the
one line that matters is buried. This module builds a **valid** planning root
by default and exposes narrow knobs for injecting the specific defect under
test, so a test reads as "a valid plan, except this one thing".

Documents are assembled from the real templates in `shared/templates/` rather
than from copies pasted here. That is deliberate: it makes the CLAUDE.md
maintenance rule ("templates <-> schema <-> validator must not drift apart")
mechanical. If a template gains a required heading and the validator is not
taught about it — or vice versa — these fixtures stop validating clean and the
suite fails, instead of the drift going unnoticed until a user hits it.

Typical use:

    from fixtures import PlanningRoot

    def test_something(self):
        with PlanningRoot() as root:                     # valid baseline
            self.assertEqual(root.validate().codes, [])

        with PlanningRoot() as root:                     # one injected defect
            root.set_task(1, justifies="Might need it later.")
            self.assertIn("SDD076", root.validate().codes)
"""

from __future__ import annotations

import json
import re
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Iterable, Sequence

REPO_ROOT = Path(__file__).resolve().parents[1]
TEMPLATES = REPO_ROOT / "shared" / "templates"

sys.path.insert(0, str(REPO_ROOT / "scripts"))

import sdd_validate  # noqa: E402

DATE = "2026-07-24"


def template_headings(name: str, level: int = 2) -> list[str]:
    """Return the H2 headings a template supplies, in order.

    Used both to build fixture bodies and to assert that templates and
    `sdd_validate.REQUIRED_HEADINGS` agree.
    """
    text = (TEMPLATES / name).read_text(encoding="utf-8")
    text = re.sub(r"<!--.*?-->", "", text, flags=re.S)  # drop contract comments
    text = re.sub(r"^---\n.*?\n---\n", "", text, flags=re.S)  # drop frontmatter
    pattern = rf"^ {{0,3}}#{{{level}}}[ \t]+(.+?)[ \t]*$"
    return [match.group(1).strip() for match in re.finditer(pattern, text, re.M)]


def _body(name: str, filler: dict[str, str] | None = None) -> str:
    """Render every H2 a template declares, with placeholder prose beneath."""
    filler = filler or {}
    parts = []
    for heading in template_headings(name):
        parts.append(f"## {heading}")
        parts.append(filler.get(heading, "Fixture content."))
        parts.append("")
    return "\n".join(parts)


@dataclass
class Task:
    """One task entry. Defaults are valid; override to inject a defect."""

    id: str
    title: str = "Add token refresh"
    status: str = "planned"
    verification: str = "pytest tests/test_auth.py — 8 pass incl. expiry case"
    justifies: str = "Sessions currently die at the one-hour mark mid-checkout."

    #: Fields set to DROP are omitted from the emitted YAML entirely, which is
    #: how a test exercises "field is absent" as distinct from "field is empty".
    DROP = object()

    def entry(self) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key in ("id", "title", "status", "verification", "justifies"):
            value = getattr(self, key)
            if value is not Task.DROP:
                result[key] = value
        return result


@dataclass
class Result:
    """Outcome of one validator run."""

    valid: bool
    diagnostics: list[dict[str, Any]]

    @property
    def codes(self) -> list[str]:
        return [item["code"] for item in self.diagnostics]

    def codes_for(self, code: str) -> list[dict[str, Any]]:
        return [item for item in self.diagnostics if item["code"] == code]

    def messages(self, code: str) -> list[str]:
        return [item["message"] for item in self.codes_for(code)]

    def __repr__(self) -> str:  # keeps assertion failures readable
        lines = [f"{i['code']} {i['path']}:{i['line']}: {i['message']}" for i in self.diagnostics]
        return "Result(valid={}, diagnostics=[\n  {}\n])".format(
            self.valid, "\n  ".join(lines) if lines else ""
        )


@dataclass
class PlanningRoot:
    """A temporary planning root, valid by default.

    Use as a context manager so the directory is always cleaned up. Mutators
    (`set_task`, `add_task`, `drop_section`, ...) rewrite the affected file
    immediately, so `validate()` always reflects the current state.
    """

    plan_name: str = "DemoPlan"
    plan_title: str = "DemoPlan"
    phase_title: str = "Phase One"
    tasks: list[Task] = field(default_factory=lambda: [Task(id="1.1")])
    git: bool = False

    def __post_init__(self) -> None:
        self.path = Path(tempfile.mkdtemp(prefix="sdd-fixture-"))
        self._write_config()
        self.write_plan()
        if self.git:
            self._init_git()

    # -- lifecycle ---------------------------------------------------------

    def __enter__(self) -> "PlanningRoot":
        return self

    def __exit__(self, *exc: object) -> None:
        self.cleanup()

    def cleanup(self) -> None:
        shutil.rmtree(self.path, ignore_errors=True)

    def _init_git(self) -> None:
        """Initialize a quiet git repo — needed by SCM-identity checks."""
        env = {
            "GIT_AUTHOR_NAME": "Fixture",
            "GIT_AUTHOR_EMAIL": "fixture@example.invalid",
            "GIT_COMMITTER_NAME": "Fixture",
            "GIT_COMMITTER_EMAIL": "fixture@example.invalid",
        }
        for args in (["init", "-q"], ["add", "-A"], ["commit", "-qm", "fixture"]):
            subprocess.run(
                ["git", *args], cwd=self.path, check=True,
                capture_output=True, env={"PATH": "/usr/bin:/bin", **env},
            )

    # -- paths -------------------------------------------------------------

    @property
    def plan_dir(self) -> Path:
        return self.path / "Plans" / self.plan_name

    @property
    def readme(self) -> Path:
        return self.plan_dir / "README.md"

    @property
    def phase_doc(self) -> Path:
        return self.plan_dir / "01-Phase-One.md"

    # -- construction ------------------------------------------------------

    def _write_config(self) -> None:
        (self.path / "planning-config.json").write_text(
            json.dumps({"planningRoot": "."}, indent=2) + "\n", encoding="utf-8"
        )

    def write_plan(self) -> None:
        self.plan_dir.mkdir(parents=True, exist_ok=True)
        (self.plan_dir / "notes").mkdir(exist_ok=True)
        self._write_readme()
        self._write_phase()

    def _write_readme(self) -> None:
        frontmatter = "\n".join(
            [
                "---",
                f'title: "{self.plan_title}"',
                "type: plan",
                "status: draft",
                f"created: {DATE}",
                f"updated: {DATE}",
                "tags: []",
                "related: []",
                "phases:",
                "  - id: 1",
                f'    title: "{self.phase_title}"',
                "    status: planned",
                f'    doc: "{self.phase_doc.name}"',
                "---",
                "",
                f"# {self.plan_title}",
                "",
            ]
        )
        body = _body(
            "plan-readme.md",
            {"Plan Completion Evidence": sdd_validate.PENDING},
        )
        self.readme.write_text(frontmatter + body, encoding="utf-8")

    def _write_phase(self) -> None:
        lines = [
            "---",
            f'title: "{self.phase_title}"',
            "type: phase",
            f'plan: "{self.plan_title}"',
            "phase: 1",
            "status: planned",
            f"created: {DATE}",
            f"updated: {DATE}",
            'deliverable: "A working token refresh path"',
            "tasks:",
        ]
        for task in self.tasks:
            entry = task.entry()
            first = True
            for key, value in entry.items():
                prefix = "  - " if first else "    "
                lines.append(f'{prefix}{key}: "{value}"')
                first = False
            if not entry:  # a task entry with every field dropped
                lines.append("  - {}")
        lines += ["---", "", f"# Phase 1: {self.phase_title}", ""]

        sections = ["## Overview", "Fixture content.", ""]
        for task in self.tasks:
            sections += [
                f"## {task.id}: {task.title}",
                "",
                "### Subtasks",
                "- [ ] Do the work",
                "",
                "### Notes",
                f"Revision boundary: {task.title} lands complete.",
                "",
                "### Completion Evidence",
                sdd_validate.PENDING,
                "",
            ]
        sections += [
            "## Acceptance Criteria",
            "- [ ] The behavior works",
            "",
            "## Phase Completion Evidence",
            sdd_validate.PENDING,
            "",
        ]
        self.phase_doc.write_text("\n".join(lines + sections), encoding="utf-8")

    # -- mutators ----------------------------------------------------------

    def set_task(self, index: int = 0, **fields: Any) -> "PlanningRoot":
        """Override fields on an existing task, then rewrite the phase doc.

        Pass ``Task.DROP`` as a value to omit the field from the YAML.
        """
        for key, value in fields.items():
            setattr(self.tasks[index], key, value)
        self._write_phase()
        return self

    def add_task(self, **fields: Any) -> "PlanningRoot":
        fields.setdefault("id", f"1.{len(self.tasks) + 1}")
        self.tasks.append(Task(**fields))
        self._write_phase()
        return self

    def drop_section(
        self, heading: str, document: str = "plan", slug: str = "demo-topic"
    ) -> "PlanningRoot":
        """Remove an H2 section, for required-heading tests.

        ``document`` selects the target: ``plan``, ``phase``, or ``brainstorm``
        (which uses ``slug`` to locate the artifact).
        """
        if document == "plan":
            path = self.readme
        elif document == "phase":
            path = self.phase_doc
        elif document == "brainstorm":
            path = self.path / "Brainstorm" / f"{slug}.md"
        else:
            raise AssertionError(f"unknown document kind {document!r}")
        text = path.read_text(encoding="utf-8")
        pattern = rf"^## {re.escape(heading)}[ \t]*$.*?(?=^## |\Z)"
        updated, count = re.subn(pattern, "", text, flags=re.M | re.S)
        if not count:
            raise AssertionError(f"no `## {heading}` section in the {document} document")
        path.write_text(updated, encoding="utf-8")
        return self

    def write_brainstorm(
        self,
        slug: str = "demo-topic",
        ideas: Sequence[str] | None = None,
        status: str = "active",
    ) -> Path:
        """Write a brainstorm artifact.

        ``ideas`` are H3 heading texts. The default includes the do-nothing
        baseline; pass a list without it to exercise SDD078.
        """
        if ideas is None:
            ideas = ["Idea 0: Do nothing / status quo", "Idea 1: Build it"]
        sections = ["## Problem Statement", "Fixture content.", "", "## Ideas", ""]
        for idea in ideas:
            sections += [
                f"### {idea}",
                "**Description:** Fixture content.",
                "**Pros:** Fixture content.",
                "**Cons:** Fixture content.",
                "**Effort:** Low",
                "",
            ]
        sections += [
            "## Evaluation",
            "Fixture content.",
            "",
            "## Next Steps",
            "- Decide",
            "",
        ]
        frontmatter = "\n".join(
            [
                "---",
                f'title: "{slug}"',
                "type: brainstorm",
                f"status: {status}",
                f"created: {DATE}",
                f"updated: {DATE}",
                "tags: []",
                "related: []",
                "---",
                "",
                f"# {slug}",
                "",
            ]
        )
        return self.write(f"Brainstorm/{slug}.md", frontmatter + "\n".join(sections))

    def write(self, relative: str, content: str) -> Path:
        """Write an arbitrary extra artifact into the planning root."""
        path = self.path / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        return path

    # -- execution ---------------------------------------------------------

    def validate(self, scope: str | None = None, identity_mode: str = "historical") -> Result:
        """Run the validator in-process and return its diagnostics.

        ``identity_mode`` defaults to ``historical`` so fixtures that are not
        git repositories do not trip worktree-identity checks that are beside
        the point of most tests.
        """
        argv = ["--root", str(self.path), "--format", "json", "--identity-mode", identity_mode]
        if scope:
            argv += ["--scope", scope]
        payload = _run_cli(argv)
        return Result(valid=bool(payload.get("valid")), diagnostics=payload.get("diagnostics", []))


def _run_cli(argv: Sequence[str]) -> dict[str, Any]:
    """Invoke the validator's own CLI entry point and parse its JSON."""
    completed = subprocess.run(
        [sys.executable, str(REPO_ROOT / "scripts" / "sdd_validate.py"), *argv],
        capture_output=True,
        text=True,
        cwd=str(REPO_ROOT),
    )
    if not completed.stdout.strip():
        raise AssertionError(
            f"validator produced no JSON (exit {completed.returncode}):\n{completed.stderr}"
        )
    return json.loads(completed.stdout)


def codes_excluding(result: Result, ignore: Iterable[str]) -> list[str]:
    """Diagnostic codes minus an ignore set, for focused assertions."""
    ignored = set(ignore)
    return [code for code in result.codes if code not in ignored]
