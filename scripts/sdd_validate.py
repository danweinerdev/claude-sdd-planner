#!/usr/bin/env python3
"""Deterministically validate sdd-planner Markdown artifacts."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import subprocess
import sys
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any, Iterable, Sequence

try:
    from sdd_decision_validate import validate as validate_decision_ledgers
except ImportError:
    from scripts.sdd_decision_validate import validate as validate_decision_ledgers

try:
    import yaml
    from yaml.nodes import MappingNode, Node, ScalarNode, SequenceNode
except ImportError:
    print(
        "sdd-validate: PyYAML is required; install it with "
        "`python3 -m pip install -r <plugin-root>/requirements.txt`",
        file=sys.stderr,
    )
    raise SystemExit(2)


STATUS = {
    "research": {"draft", "active", "archived"},
    "brainstorm": {"draft", "active", "archived"},
    "spec": {"draft", "review", "approved", "implemented", "superseded"},
    "design": {"draft", "review", "approved", "implemented", "superseded"},
    "plan": {"draft", "approved", "active", "complete", "archived"},
    "phase": {"planned", "in-progress", "complete", "blocked", "deferred"},
    "debrief": {"draft", "complete"},
    "retro": {"draft", "complete"},
    "diagram": {"draft", "active", "archived"},
    "decision-log": {"active", "archived"},
    "review": {"open", "resolved", "superseded"},
}
TASK_STATUS = {"planned", "in-progress", "complete", "blocked", "deferred"}
FINDING_STATUS = {"open", "fixed", "deferred", "rejected", "answered"}
DECISION_STATUS = {"proposed", "accepted", "rejected", "superseded"}
ARTIFACT_DIRS = ("Research", "Brainstorm", "Specs", "Designs", "Plans", "Decisions", "Retro", "Diagrams")
COMMON_FIELDS = ("title", "type", "status", "created", "updated")
PENDING = "Pending — not complete."
IDS = {
    "FR": re.compile(r"\bFR-(\d{2,})\b"),
    "NFR": re.compile(r"\bNFR-(\d{2,})\b"),
    "AC": re.compile(r"\bAC-(\d{2,})\b"),
    "D": re.compile(r"\bD-(\d{4,})\b"),
}
DEFINITIONS = {
    "FR": re.compile(r"^\s*-\s+\*\*(FR-\d{2,})\*\*\s*:", re.MULTILINE),
    "NFR": re.compile(r"^\s*-\s+\*\*(NFR-\d{2,})\*\*\s*:", re.MULTILINE),
    "AC": re.compile(r"^\s*-\s+\[[ xX]\]\s+\*\*(AC-\d{2,})\*\*\s*:", re.MULTILINE),
}
NON_BLOCKING = re.compile(r"\*\*non-blocking\*\*", re.IGNORECASE)
REQUIRED_HEADINGS = {
    "research": ("Context", "Findings", "Analysis", "Open Questions"),
    "brainstorm": ("Problem Statement", "Ideas", "Evaluation", "Next Steps"),
    "spec": ("Overview", "Goals", "Non-Goals", "Requirements", "User Stories", "Acceptance Criteria", "Constraints", "Dependencies", "Open Questions"),
    "design": ("Overview", "Architecture", "Design Decisions", "Error Handling", "Testing Strategy", "Migration / Rollout"),
    "plan": ("Overview", "Architecture", "Key Decisions", "Dependencies", "Plan Completion Evidence"),
    "phase": ("Overview", "Acceptance Criteria", "Phase Completion Evidence"),
    "review": ("Findings", "Resolution Log"),
    "debrief": ("Decisions Made", "Requirements Assessment", "Deviations", "Risks & Issues Encountered", "Lessons Learned", "Impact on Subsequent Phases", "Skill Opportunities"),
}


@dataclass(frozen=True)
class Diagnostic:
    severity: str
    code: str
    path: str
    line: int
    message: str
    correction: str
    implicated: tuple[str, ...] = ()


@dataclass
class Artifact:
    path: Path
    rel: str
    meta: dict[str, Any]
    body: str
    source: str
    body_line: int

    @property
    def kind(self) -> str:
        return self.meta.get("type") if isinstance(self.meta.get("type"), str) else ""

    @property
    def status(self) -> str:
        return self.meta.get("status") if isinstance(self.meta.get("status"), str) else ""

    def line(self, text: str, body: bool = False) -> int:
        source = self.body if body else self.source
        offset = self.body_line - 1 if body else 0
        for number, value in enumerate(source.splitlines(), 1):
            if text in value:
                return number + offset
        return 1


@dataclass(frozen=True)
class ScopeSelection:
    artifacts: frozenset[str]
    roots: tuple[str, ...]
    error: str | None = None


class Validator:
    def __init__(self, root: Path, repo: Path, identity_mode: str = "auto") -> None:
        self.root = root.resolve()
        self.repo = repo.resolve()
        self.artifacts: list[Artifact] = []
        self.by_path: dict[str, Artifact] = {}
        self.tasks: dict[tuple[str, str], tuple[Artifact, dict[str, Any]]] = {}
        self.decisions: dict[tuple[str, str], tuple[Artifact, dict[str, Any]]] = {}
        self.spec_ids: dict[str, dict[str, set[str]]] = {}
        self.out: list[Diagnostic] = []
        self.identity_mode = identity_mode
        self.artifact_repos: dict[str, Path] = {}
        self.plan_repos: dict[str, Path] = {}
        self._planning_root_scm_name: str | None = None
        self._configure_repositories()

    def error(self, artifact: Artifact | None, code: str, message: str, correction: str, line: int = 1, path: str | None = None, implicated: Iterable[str] = ()) -> None:
        self.out.append(Diagnostic("error", code, path or (artifact.rel if artifact else str(self.root)), line, message, correction, tuple(sorted(set(implicated)))))

    def candidate(self, artifact: Artifact | None, code: str, message: str, correction: str, line: int = 1, path: str | None = None) -> None:
        self.out.append(Diagnostic("candidate", code, path or (artifact.rel if artifact else str(self.root)), line, message, correction))

    def _configure_repositories(self) -> None:
        config = self.repo / "planning-config.json"
        if not config.is_file():
            return
        try:
            data = json.loads(config.read_text(encoding="utf-8"))
        except (OSError, UnicodeError, json.JSONDecodeError) as exc:
            self.error(None, "SDD000", f"Cannot parse `{config}`: {exc}", "Correct planning-config.json before validation.")
            return
        mappings = data.get("planMapping", {})
        repositories = data.get("repositories", {})
        if not isinstance(mappings, dict) or not isinstance(repositories, dict):
            self.error(None, "SDD000", "`planMapping` and `repositories` must be JSON objects.", "Correct planning-config.json repository mapping.")
            return
        for plan_name, repository_key in mappings.items():
            record = repositories.get(repository_key)
            raw_path = record.get("path") if isinstance(record, dict) else record
            if not isinstance(plan_name, str) or not isinstance(raw_path, str):
                self.error(None, "SDD000", f"Plan mapping `{plan_name}` does not resolve to a repository path.", "Add repositories.<key>.path for every plan mapping.")
                continue
            target = Path(raw_path)
            target = (target if target.is_absolute() else config.parent / target).resolve()
            if not target.is_dir():
                self.error(None, "SDD000", f"Mapped repository `{target}` for plan `{plan_name}` does not exist.", "Correct the mapping or create the target repository.")
                continue
            self.plan_repos[plan_name] = target

    def _repo_for_path(self, relative: str) -> Path:
        parts = Path(relative).parts
        if len(parts) >= 2 and parts[0] == "Plans":
            return self.plan_repos.get(parts[1], self.repo)
        return self.repo

    def _repo_for_artifact(self, artifact: Artifact) -> Path:
        return self.artifact_repos.get(artifact.rel, self._repo_for_path(artifact.rel))

    def _planning_root_scm(self) -> str:
        """Return the lifecycle transport available for this planning root."""
        if self._planning_root_scm_name is None:
            self._planning_root_scm_name = detected_scm(self.root)
        return self._planning_root_scm_name

    def run(self) -> list[Diagnostic]:
        self._discover()
        self._legacy_layouts()
        for artifact in self.artifacts:
            self._common(artifact)
        self._index()
        self._append_only_repository_history()
        for artifact in self.artifacts:
            self._headings(artifact)
            self._references(artifact)
            self._specific(artifact)
            self._citations(artifact)
        self._phase_ownership()
        self._graphs()
        self._traceability()
        self._decision_links()
        self._focused_decision_logs()
        return sorted(self.out, key=lambda item: (item.path, item.line, item.code, item.message))

    def _discover(self) -> None:
        if not self.root.is_dir():
            self.error(None, "SDD001", "Planning root is not a directory.", "Pass an existing planning root with --root.")
            return
        paths: set[Path] = set()
        for dirname in ARTIFACT_DIRS:
            directory = self.root / dirname
            if directory.is_dir():
                paths.update(directory.rglob("*.md"))
        for path in sorted(paths):
            artifact = self._parse(path)
            if artifact:
                self.artifacts.append(artifact)
                self.by_path[artifact.rel] = artifact
                self.artifact_repos[artifact.rel] = self._repo_for_path(artifact.rel)
        for key, repository in sorted({str(path): path for path in self.plan_repos.values()}.items()):
            if repository == self.repo:
                continue
            candidates = [repository / "DECISIONS.md", *sorted(repository.glob("archive-*.md"))]
            for path in candidates:
                if not path.is_file():
                    continue
                rel = f"@repo:{key}/{path.name}"
                artifact = self._parse(path, rel)
                if artifact:
                    self.artifacts.append(artifact)
                    self.by_path[rel] = artifact
                    self.artifact_repos[rel] = repository

    def _parse(self, path: Path, rel_override: str | None = None) -> Artifact | None:
        rel = rel_override or path.relative_to(self.root).as_posix()
        try:
            source = path.read_bytes().decode("utf-8")
        except (OSError, UnicodeError) as exc:
            self.error(None, "SDD002", f"Cannot read UTF-8 artifact: {exc}", "Store the artifact as readable UTF-8.", path=rel)
            return None
        if "\r\n" in source:
            self.error(None, "SDD003", "Artifact uses CRLF line endings.", "Normalize it to UTF-8 with LF endings.", path=rel)
        lines = source.splitlines(keepends=True)
        if not lines or lines[0].strip() != "---":
            self.error(None, "SDD004", "Missing opening YAML frontmatter delimiter.", "Start the artifact with `---` YAML frontmatter.", path=rel)
            return None
        end = next((i for i, line in enumerate(lines[1:], 1) if line.strip() == "---"), None)
        if end is None:
            self.error(None, "SDD005", "Missing closing YAML frontmatter delimiter.", "Close frontmatter with a standalone `---`.", path=rel)
            return None
        try:
            meta = yaml.safe_load("".join(lines[1:end]))
        except yaml.YAMLError as exc:
            mark = getattr(exc, "problem_mark", None)
            self.error(None, "SDD006", f"Invalid YAML frontmatter: {exc}", "Correct the YAML syntax.", (mark.line + 2) if mark else 1, rel)
            return None
        if not isinstance(meta, dict):
            self.error(None, "SDD007", "Frontmatter is not a mapping.", "Use key/value YAML frontmatter.", path=rel)
            return None
        return Artifact(path, rel, meta, "".join(lines[end + 1 :]), source, end + 2)

    def _legacy_layouts(self) -> None:
        status_names = set().union(*STATUS.values(), TASK_STATUS)
        for dirname in ARTIFACT_DIRS:
            directory = self.root / dirname
            if not directory.is_dir():
                continue
            for child in directory.iterdir():
                if child.is_dir() and child.name.lower() in status_names:
                    rel = child.relative_to(self.root).as_posix()
                    self.error(None, "SDD008", f"Legacy status subfolder `{rel}` is invalid.", "Move artifacts to the type directory and keep lifecycle in frontmatter.", path=rel)

    def _common(self, artifact: Artifact) -> None:
        for field in COMMON_FIELDS:
            if artifact.meta.get(field) in (None, ""):
                self.error(artifact, "SDD010", f"Required field `{field}` is missing or empty.", f"Add a nonempty `{field}` value.")
        if artifact.kind not in STATUS:
            self.error(artifact, "SDD011", f"Unknown type `{artifact.kind or '<missing>'}`.", f"Use one of: {', '.join(sorted(STATUS))}.", artifact.line("type:"))
            return
        if artifact.status not in STATUS[artifact.kind]:
            self.error(artifact, "SDD012", f"Status `{artifact.status or '<missing>'}` is invalid for `{artifact.kind}`.", f"Use one of: {', '.join(sorted(STATUS[artifact.kind]))}.", artifact.line("status:"))
        for field in ("created", "updated"):
            value = artifact.meta.get(field)
            if not isinstance(value, dt.date) and not (isinstance(value, str) and re.fullmatch(r"\d{4}-\d{2}-\d{2}", value)):
                self.error(artifact, "SDD013", f"`{field}` must be YYYY-MM-DD.", f"Set `{field}` to an ISO date.", artifact.line(f"{field}:"))
        if artifact.kind != "phase":
            for field in ("tags", "related"):
                if not isinstance(artifact.meta.get(field), list):
                    self.error(artifact, "SDD014", f"`{field}` must be a YAML list.", f"Use `{field}: []` when empty.", artifact.line(f"{field}:"))

    def sections(self, artifact: Artifact, level: int = 2) -> dict[str, tuple[int, str]]:
        lines = markdown_lines(artifact.body)
        matches: list[tuple[int, str]] = []
        pattern = re.compile(rf"^ {{0,3}}{'#' * level}\s+(.+?)\s*$")
        for index, (_, visible) in enumerate(lines):
            match = pattern.match(visible.rstrip("\r\n"))
            if match:
                matches.append((index, match.group(1).strip()))
        result: dict[str, tuple[int, str]] = {}
        for index, (start, heading) in enumerate(matches):
            end = matches[index + 1][0] if index + 1 < len(matches) else len(lines)
            line = artifact.body_line + start
            result[heading] = (line, "".join(visible for _, visible in lines[start + 1 : end]))
        return result

    def _headings(self, artifact: Artifact) -> None:
        sections = self.sections(artifact)
        for heading in REQUIRED_HEADINGS.get(artifact.kind, ()):
            if heading not in sections:
                self.error(artifact, "SDD020", f"Required section `## {heading}` is missing.", f"Add a nonempty `## {heading}` section.")
        if artifact.kind in {"plan", "phase"}:
            visible = visible_markdown(artifact.body)
            legacy = re.search(
                r"^ {0,3}#{3}[ \t]+.*\bEvidence[ \t]+Rollup\b(?:[ \t]+#+)?[ \t]*$",
                visible,
                re.MULTILINE | re.IGNORECASE,
            )
            if legacy:
                self.error(
                    artifact,
                    "SDD158" if artifact.kind == "plan" else "SDD157",
                    "Legacy `Evidence Rollup` headings are not permitted.",
                    "Replace the rollup with the required concise completed-identity section.",
                    artifact.body_line + visible[: legacy.start()].count("\n"),
                )
            heading = "Plan Completion Evidence" if artifact.kind == "plan" else "Phase Completion Evidence"
            evidence_sections = heading_bodies(artifact.body, 2, heading)
            if len(evidence_sections) > 1:
                self.error(
                    artifact,
                    "SDD020",
                    f"Parent completion-evidence section `## {heading}` is duplicated.",
                    f"Keep exactly one visible `## {heading}` section.",
                    artifact.line(f"## {heading}", True),
                )
            elif len(evidence_sections) == 1:
                self._evidence(artifact, artifact.status, heading, *sections[heading])

    def _index(self) -> None:
        for artifact in self.artifacts:
            if artifact.kind == "spec":
                body = no_comments(artifact.body)
                families: dict[str, set[str]] = {}
                for family in ("FR", "NFR", "AC"):
                    found = DEFINITIONS[family].findall(body)
                    for value in duplicates(found):
                        self.error(artifact, "SDD030", f"Duplicate `{value}` in its owning spec.", "Assign a new append-only id and update citations.", artifact.line(value, True))
                    families[family] = set(found)
                self.spec_ids[artifact.rel] = families
            elif artifact.kind == "phase" and isinstance(artifact.meta.get("tasks"), list):
                plan_name = str(artifact.meta.get("plan", ""))
                for task in artifact.meta["tasks"]:
                    if not isinstance(task, dict) or not isinstance(task.get("id"), str):
                        continue
                    task_id = task["id"]
                    key = (plan_name, task_id)
                    if key in self.tasks:
                        self.error(artifact, "SDD031", f"Duplicate task id `{task_id}` in plan `{plan_name}`.", "Assign a unique append-only id within the plan and update references.")
                    else:
                        self.tasks[key] = (artifact, task)
            elif artifact.kind == "decision-log" and isinstance(artifact.meta.get("decisions"), list):
                for entry in artifact.meta["decisions"]:
                    if not isinstance(entry, dict) or not isinstance(entry.get("id"), str):
                        continue
                    decision_id = entry["id"]
                    repo_key = str(self.artifact_repos.get(artifact.rel, self.repo))
                    key = (repo_key, decision_id)
                    if key in self.decisions:
                        self.error(artifact, "SDD032", f"Duplicate decision id `{decision_id}`.", "Renumber the later entry and update all links.")
                    else:
                        self.decisions[key] = (artifact, entry)

    def resolve(self, reference: str) -> Artifact | None:
        value = reference.strip()
        path = Path(value)
        if not value or path.is_absolute() or "\\" in value or any(part in {".", ".."} for part in path.parts):
            return None
        for candidate in (value, f"{value}/README.md", f"{value}.md"):
            if candidate in self.by_path:
                return self.by_path[candidate]
        return None

    def _references(self, artifact: Artifact) -> None:
        related = artifact.meta.get("related", [])
        if not isinstance(related, list):
            return
        for reference in related:
            if not isinstance(reference, str) or not reference:
                self.error(artifact, "SDD040", "A `related` entry is not a nonempty string.", "Use a planning-root-relative artifact path.")
            elif self.resolve(reference) is None:
                self.error(artifact, "SDD041", f"Related path `{reference}` does not resolve.", "Point it at an existing artifact directory or Markdown file.", artifact.line(reference))

    def _specific(self, artifact: Artifact) -> None:
        self._open_questions(artifact)
        if artifact.kind == "spec":
            for family in ("FR", "NFR", "AC"):
                if not self.spec_ids[artifact.rel][family]:
                    self.error(artifact, "SDD050", f"Spec defines no `{family}-NN` element.", f"Number applicable elements with stable `{family}-NN` ids.")
        elif artifact.kind == "plan":
            self._plan(artifact)
        elif artifact.kind == "phase":
            self._phase(artifact)
        elif artifact.kind == "review":
            self._review(artifact)
        elif artifact.kind == "decision-log":
            self._ledger(artifact)
        elif artifact.kind == "debrief":
            self._required(artifact, ("plan", "phase", "phase_title"))

    def _required(self, artifact: Artifact, fields: Sequence[str]) -> None:
        for field in fields:
            if artifact.meta.get(field) in (None, ""):
                self.error(artifact, "SDD051", f"Required `{artifact.kind}` field `{field}` is missing.", f"Add a nonempty `{field}` value.")

    def _open_questions(self, artifact: Artifact) -> None:
        gated = (
            artifact.kind in {"spec", "design"} and artifact.status in {"approved", "implemented"}
        ) or (
            artifact.kind == "plan" and artifact.status in {"approved", "active", "complete"}
        )
        if not gated:
            return
        section = self.sections(artifact).get("Open Questions")
        if not section:
            return
        for question in open_question_items(section[1]):
            markers = list(NON_BLOCKING.finditer(question))
            marker = markers[0] if len(markers) == 1 else None
            prompt = question[: marker.start()].strip(" \t:—-") if marker else ""
            rationale = question[marker.end() :].strip(" \t:—-") if marker else ""
            if marker is None or not prompt or not rationale:
                self.error(
                    artifact,
                    "SDD153",
                    "Approved artifact contains a blocking or unexplained open question.",
                    "Resolve it or mark the bullet `**non-blocking** — <rationale>`.",
                    section[0],
                )

    def _append_only_repository_history(self) -> None:
        repository = git_root(self.root)
        if repository is None:
            return
        try:
            prefix = self.root.relative_to(repository).as_posix()
        except ValueError:
            return
        roots = [f"{prefix}/{name}" if prefix != "." else name for name in ("Specs", "Plans")]
        output, error = git_output(repository, "ls-tree", "-r", "--name-only", "-z", "HEAD", "--", *roots)
        if error or output is None:
            return
        for raw_path in output.split(b"\0"):
            if not raw_path or not raw_path.endswith(b".md"):
                continue
            repository_relative = os.fsdecode(raw_path)
            baseline_bytes, baseline_error = git_output(repository, "show", f"HEAD:{repository_relative}")
            if baseline_error or baseline_bytes is None:
                continue
            try:
                baseline = baseline_bytes.decode("utf-8")
            except UnicodeDecodeError:
                continue
            meta = parse_frontmatter_source(baseline)
            kind = meta.get("type") if meta else None
            if kind not in {"spec", "plan", "phase"}:
                continue
            current_path = repository / repository_relative
            try:
                artifact_relative = current_path.relative_to(self.root).as_posix()
            except ValueError:
                artifact_relative = repository_relative
            worktree = read_utf8(current_path)
            index_bytes, index_error = git_output(repository, "show", f":{repository_relative}")
            index = None
            if not index_error and index_bytes is not None:
                try:
                    index = index_bytes.decode("utf-8")
                except UnicodeDecodeError:
                    index = None
            for source_name, current in (("worktree", worktree), ("index", index)):
                self._check_retained_ids(kind, baseline, current, artifact_relative, source_name)

    def _check_retained_ids(self, kind: str, baseline: str, current: str | None, path: str, source_name: str) -> None:
        current_meta = parse_frontmatter_source(current or "")
        if not current_meta or current_meta.get("type") != kind:
            self.error(
                None,
                "SDD164",
                f"Previously tracked `{kind}` artifact changed type or disappeared from the {source_name}.",
                f"Restore the artifact as `type: {kind}` at its tracked path before moving or superseding it.",
                path=path,
            )
        if kind == "spec":
            prior = spec_definition_ids(baseline)
            retained = spec_retained_ids(current or "")
            code = "SDD154"
            noun = "spec"
            correction = "Restore the id and mark it retired with `removed — see <reason/citation>` or a struck-through definition."
        elif kind == "plan":
            prior = frontmatter_entry_ids(baseline, "phases")
            retained = frontmatter_entry_ids(current or "", "phases")
            code = "SDD155"
            noun = "phase"
            correction = "Restore the append-only phase id and preserve its historical entry."
        else:
            prior = frontmatter_entry_ids(baseline, "tasks")
            retained = frontmatter_entry_ids(current or "", "tasks")
            code = "SDD156"
            noun = "task"
            correction = "Restore the append-only task id and preserve its historical entry."
        for identifier in sorted(prior - retained):
            self.error(
                None,
                code,
                f"Previously tracked {noun} id `{identifier}` was removed from the {source_name}.",
                correction,
                path=path,
            )

    def _phase_ownership(self) -> None:
        owners: dict[str, list[str]] = {}
        for plan in (item for item in self.artifacts if item.kind == "plan"):
            plan_name = Path(plan.rel).parent.name
            phases = plan.meta.get("phases", [])
            if not isinstance(phases, list):
                continue
            for phase in phases:
                if not isinstance(phase, dict) or not isinstance(phase.get("doc"), str):
                    continue
                target = (Path(plan.rel).parent / phase["doc"]).as_posix()
                owners.setdefault(target, []).append(plan_name)
        for phase in (item for item in self.artifacts if item.kind == "phase"):
            parts = Path(phase.rel).parts
            physical_plan = parts[1] if len(parts) >= 3 and parts[0] == "Plans" else ""
            declared_plan = str(phase.meta.get("plan", ""))
            listed = owners.get(phase.rel, [])
            if len(listed) != 1 or listed[0] != physical_plan or declared_plan != physical_plan:
                self.error(
                    phase,
                    "SDD163",
                    f"Phase ownership is inconsistent: path plan `{physical_plan}`, declared plan `{declared_plan}`, listed by {listed}.",
                    "Place the phase under its owning plan, set the matching `plan` field, and list it exactly once in that plan README.",
                )

    def _plan(self, artifact: Artifact) -> None:
        phases = artifact.meta.get("phases")
        if not isinstance(phases, list):
            self.error(artifact, "SDD052", "`phases` must be a list.", "Use `phases: []` when empty.")
            return
        ids: list[str] = []
        for phase in phases:
            if not isinstance(phase, dict):
                self.error(artifact, "SDD053", "A phase entry is not a mapping.", "Add id, title, status, and doc fields.")
                continue
            for field in ("id", "title", "status", "doc"):
                if phase.get(field) in (None, ""):
                    self.error(artifact, "SDD054", f"Phase entry is missing `{field}`.", f"Add `{field}` to the entry.")
            phase_id = str(phase.get("id", ""))
            ids.append(phase_id)
            if phase.get("status") not in STATUS["phase"]:
                self.error(artifact, "SDD055", f"Phase `{phase_id}` has invalid status `{phase.get('status')}`.", "Use an allowed phase status.")
            doc = phase.get("doc")
            target = self.by_path.get((Path(artifact.rel).parent / str(doc)).as_posix()) if doc else None
            if target is None:
                self.error(artifact, "SDD056", f"Phase `{phase_id}` doc `{doc}` does not resolve.", "Point `doc` at an existing phase file.")
            else:
                plan_name = Path(artifact.rel).parent.name
                if target.kind != "phase":
                    self.error(artifact, "SDD150", f"Phase `{phase_id}` doc `{doc}` has type `{target.kind}`.", "Point `doc` at a `type: phase` artifact.")
                if str(target.meta.get("plan", "")) != plan_name:
                    self.error(artifact, "SDD151", f"Phase `{phase_id}` doc `{doc}` belongs to plan `{target.meta.get('plan')}`.", f"Set its `plan` field to `{plan_name}`.")
                if target.meta.get("title") != phase.get("title"):
                    self.error(artifact, "SDD152", f"Phase `{phase_id}` title disagrees with `{doc}`.", "Make the phase entry and document titles identical.")
                if str(target.meta.get("phase", "")) != phase_id:
                    self.error(artifact, "SDD057", f"Phase `{phase_id}` disagrees with `{doc}` id `{target.meta.get('phase')}`.", "Make both ids identical.")
                if target.status != phase.get("status"):
                    self.error(artifact, "SDD058", f"Phase `{phase_id}` status disagrees with `{doc}`.", "Make both statuses identical.")
                if artifact.status == "complete" and target.status != "complete":
                    self.error(artifact, "SDD059", f"Complete plan contains incomplete phase `{phase_id}`.", "Complete every phase first.")
        for value in duplicates(ids):
            self.error(artifact, "SDD060", f"Duplicate phase id `{value}`.", "Assign a unique append-only phase id.")
        if artifact.status == "complete":
            self._plan_phase_identities(artifact, phases)

    def _phase(self, artifact: Artifact) -> None:
        self._required(artifact, ("plan", "phase", "deliverable"))
        tasks = artifact.meta.get("tasks")
        if not isinstance(tasks, list):
            self.error(artifact, "SDD061", "`tasks` must be a list.", "Use `tasks: []` when empty.")
            return
        self._task_completion_evidence_structure(
            artifact,
            [
                task["id"]
                for task in tasks
                if isinstance(task, dict) and isinstance(task.get("id"), str)
            ],
        )
        sections = self.sections(artifact)
        phase_id = str(artifact.meta.get("phase", ""))
        task_evidence: dict[str, str] = {}
        for task in tasks:
            if not isinstance(task, dict):
                self.error(artifact, "SDD062", "A task entry is not a mapping.", "Add id, title, status, and verification fields.")
                continue
            for field in ("id", "title", "status", "verification"):
                if task.get(field) in (None, ""):
                    self.error(artifact, "SDD063", f"Task is missing `{field}`.", f"Add a nonempty `{field}`.")
            task_id = str(task.get("id", ""))
            if not re.fullmatch(rf"{re.escape(phase_id)}\.\d+", task_id):
                self.error(artifact, "SDD064", f"Task id `{task_id}` is not in phase `{phase_id}`.", f"Use `{phase_id}.N`.")
            if task.get("status") not in TASK_STATUS:
                self.error(artifact, "SDD065", f"Task `{task_id}` has invalid status `{task.get('status')}`.", "Use an allowed task status.")
            heading = next((name for name in sections if re.match(rf"^{re.escape(task_id)}(?:\s*:|\s|$)", name)), None)
            if heading is None:
                self.error(artifact, "SDD066", f"Task `{task_id}` has no body section.", f"Add `## {task_id}: ...` with task detail sections.")
                continue
            line, body = sections[heading]
            for required in ("Subtasks", "Notes", "Completion Evidence"):
                if not re.search(rf"^ {{0,3}}###\s+{re.escape(required)}\s*$", body, re.MULTILINE):
                    self.error(artifact, "SDD067", f"Task `{task_id}` is missing `### {required}`.", f"Add it inside the task section.", line)
            evidence_blocks = heading_bodies(body, 3, "Completion Evidence")
            if len(evidence_blocks) == 1:
                value = evidence_blocks[0]
                task_evidence[task_id] = no_comments(value).strip()
                self._evidence(artifact, str(task.get("status", "")), f"Task {task_id} Completion Evidence", line, value)
            subtasks = heading_bodies(body, 3, "Subtasks")
            if len(subtasks) > 1:
                self.error(artifact, "SDD067", f"Task `{task_id}` has duplicate visible `### Subtasks` sections.", "Keep exactly one Subtasks section inside the task.", line)
            if task.get("status") == "complete" and len(subtasks) == 1 and has_unchecked_checkbox(subtasks[0]):
                self.error(artifact, "SDD069", f"Complete task `{task_id}` has unchecked subtasks.", "Verify and check every subtask.", line)
            if artifact.status == "complete" and task.get("status") != "complete":
                self.error(artifact, "SDD068", f"Complete phase contains incomplete task `{task_id}`.", "Complete every task first.")
        criteria = sections.get("Acceptance Criteria")
        if artifact.status == "complete" and criteria and has_unchecked_checkbox(criteria[1]):
            self.error(artifact, "SDD069", "Complete phase has unchecked acceptance criteria.", "Verify and check every criterion.", criteria[0])
        if artifact.status == "complete":
            phase_evidence = sections.get("Phase Completion Evidence")
            if phase_evidence:
                rollup = phase_evidence[1]
                task_identities = self._phase_task_git_identities(
                    artifact, tasks, task_evidence, phase_evidence[0]
                )
                self._phase_final_review(
                    artifact, rollup, phase_evidence[0], task_identities
                )
                expected = {
                    str(task.get("id", "")): markdown_scalar(
                        evidence_value(task_evidence.get(str(task.get("id", "")), ""), "Revision / checkpoint")
                    )
                    for task in tasks
                    if isinstance(task, dict) and task.get("status") == "complete"
                }
                self._completed_identities(
                    artifact,
                    rollup,
                    "Completed task identities",
                    expected,
                    phase_evidence[0],
                    "task",
                )

    def _task_completion_evidence_structure(
        self, artifact: Artifact, task_ids: Sequence[str]
    ) -> None:
        """Require task evidence headings to belong to one declared task section."""
        lines = markdown_lines(artifact.body)
        sections = declared_task_completion_evidence_sections(lines, task_ids)
        contained = {start for _, start, _ in sections}
        marker = re.compile(r"^ {0,3}###\s+Completion Evidence\s*$")
        for index, (_, visible) in enumerate(lines):
            if marker.match(visible.rstrip("\r\n")) and index not in contained:
                self.error(
                    artifact,
                    "SDD067",
                    "Visible `### Completion Evidence` is outside a declared task section.",
                    "Keep Completion Evidence only inside the matching declared `## <task-id>` section.",
                    artifact.body_line + index,
                )
        counts: dict[str, int] = {}
        for task_id, _, _ in sections:
            counts[task_id] = counts.get(task_id, 0) + 1
        for task_id, count in counts.items():
            if count > 1:
                first = next(start for found_id, start, _ in sections if found_id == task_id)
                self.error(
                    artifact,
                    "SDD067",
                    f"Task `{task_id}` has duplicate visible `### Completion Evidence` sections.",
                    "Keep exactly one Completion Evidence section inside the task.",
                    artifact.body_line + first,
                )

    def _phase_task_git_identities(
        self,
        phase: Artifact,
        tasks: list[Any],
        task_evidence: dict[str, str],
        line: int,
    ) -> list[tuple[str, str]]:
        """Extract clean-Git task commits for inclusion in a phase review range."""
        identities: list[tuple[str, str]] = []
        if detected_scm(self._repo_for_artifact(phase)) != "git":
            return identities
        for task in tasks:
            if not isinstance(task, dict) or task.get("status") != "complete":
                continue
            task_id = str(task.get("id", ""))
            evidence = task_evidence.get(task_id, "")
            vcs = markdown_scalar(evidence_value(evidence, "VCS")) or "missing"
            revision = markdown_scalar(
                evidence_value(evidence, "Revision / checkpoint")
            ) or "missing"
            if vcs in {"git", "git-worktree"} and re.fullmatch(
                r"[0-9a-fA-F]{40}", revision
            ):
                identities.append((task_id, revision))
                continue
            self.error(
                phase,
                "SDD172",
                f"Git phase review range cannot validate completed task `{task_id}` identity `{revision}` with VCS `{vcs}` because no deterministic task-identity adapter is available.",
                "Record a clean full native Git revision/checkpoint in every completed task's evidence, or keep the phase non-complete until a deterministic adapter for the task identity is available.",
                line,
            )
        return identities

    def _phase_final_review(
        self,
        artifact: Artifact,
        body: str,
        line: int,
        task_identities: Sequence[tuple[str, str]] = (),
    ) -> None:
        """Validate the durable, frozen all-lane review gate for phase closure."""
        target_is_git = self._phase_review_identity_adapter_available(artifact, line)
        final_review_values = evidence_values(body, "Final aligned review")
        value = markdown_scalar(final_review_values[0] if len(final_review_values) == 1 else None)
        parsed = parse_final_aligned_review(value)
        if parsed is None:
            self.error(
                artifact,
                "SDD166",
                "Complete phase must contain exactly one valid visible `Final aligned review` entry.",
                "Keep one `- Final aligned review: <review artifact path>; frozen: <exact revision/range>` line outside comments and fenced blocks.",
                line,
            )
            return
        review_ref, frozen = parsed
        review = self.resolve(review_ref)
        if review is None or review.kind != "review":
            self.error(
                artifact,
                "SDD166",
                f"`Final aligned review` `{review_ref}` does not resolve to a review artifact.",
                "Point it at the persisted final phase code-review artifact.",
                line,
            )
            return
        if review.meta.get("rev") != frozen:
            self.error(
                artifact,
                "SDD168",
                f"Final review `{review.rel}` frozen identity `{frozen}` does not exactly match its frontmatter `rev`.",
                "Use the exact nonempty review `rev` after `frozen:` in the Final aligned review entry.",
                line,
            )
        self._verify_phase_review_planning_revision(artifact, review, line)
        if target_is_git:
            self._verify_phase_review_identity(
                artifact, body, frozen, line, task_identities
            )
        if not self._is_valid_phase_review(review, artifact):
            self.error(
                artifact,
                "SDD167",
                f"Final review `{review.rel}` is not a resolved, frozen Aligned phase review across all four lanes.",
                "Record review_scope: phase, frozen: true, verdict: Aligned, the four stable lanes, and resolved status on a review of this phase.",
                line,
            )
        if self._planning_root_scm() == "git":
            self._verify_git_phase_review_committed(artifact, review, frozen, line)
        if target_is_git:
            identities = parse_git_frozen_identity(frozen)
            if identities and all(git_commit_exists(self._repo_for_artifact(artifact), identity) for identity in identities):
                self._verify_git_phase_post_review_state(artifact, review, identities[-1], line)

    def _phase_review_identity_adapter_available(self, phase: Artifact, line: int) -> bool:
        repository = self._repo_for_artifact(phase)
        scm = detected_scm(repository)
        if scm == "git":
            return True
        self.error(
            phase,
            "SDD172",
            f"Phase review identity cannot be validated: target repository `{repository}` uses unsupported SCM adapter `{scm}`.",
            "Keep the phase non-complete until a deterministic review-identity adapter for the target SCM is available.",
            line,
        )
        return False

    def _verify_phase_review_identity(
        self,
        phase: Artifact,
        body: str,
        frozen: str,
        line: int,
        task_identities: Sequence[tuple[str, str]] = (),
    ) -> None:
        """Git review-identity adapter for a frozen phase gate."""
        repository = self._repo_for_artifact(phase)
        checkpoint = markdown_scalar(evidence_value(body, "Revision / checkpoint"))
        if not checkpoint or not re.fullmatch(r"[0-9a-fA-F]{40}", checkpoint):
            self.error(
                phase,
                "SDD173",
                "Git phase completion requires `Revision / checkpoint` to be one clean full native Git revision/checkpoint.",
                "Record the exact full native Git revision/checkpoint as `Revision / checkpoint`; a validated integration merge is allowed, but do not use a dirty or fallback identity.",
                line,
            )
            return
        identities = parse_git_frozen_identity(frozen)
        if identities is None:
            self.error(
                phase,
                "SDD173",
                f"Git phase review identity `{frozen}` is not an exact `<full40>..<full40>` range.",
                "Use an immutable full-commit range in both review `rev` and `frozen:`.",
                line,
            )
            return
        if identities[0] == identities[1]:
            self.error(
                phase,
                "SDD173",
                "Git phase review range has identical base and endpoint commits.",
                "Use distinct full commits that bound the reviewed phase diff.",
                line,
            )
            return
        if identities[-1] != checkpoint:
            self.error(
                phase,
                "SDD173",
                f"Git phase review endpoint `{identities[-1]}` does not equal phase `Revision / checkpoint` `{checkpoint}`.",
                "Review the final phase revision/checkpoint or use a range whose endpoint is that exact checkpoint; a validated integration merge is allowed.",
                line,
            )
        for identity in identities:
            if not git_commit_exists(repository, identity):
                self.error(
                    phase,
                    "SDD173",
                    f"Git phase review identity commit `{identity}` does not exist in target repository `{repository}`.",
                    "Use only full commits that exist in the target repository.",
                    line,
                )
        if not all(git_commit_exists(repository, identity) for identity in identities):
            return
        ancestor = subprocess.run(
            ["git", "-C", str(repository), "merge-base", "--is-ancestor", identities[0], identities[1]],
            check=False,
            capture_output=True,
        )
        if ancestor.returncode != 0:
            self.error(
                phase,
                "SDD173",
                f"Git phase review range base `{identities[0]}` is not an ancestor of endpoint `{identities[1]}`.",
                "Use a forward reviewed range whose base is an ancestor of the phase checkpoint.",
                line,
            )
            return
        for task_id, revision in task_identities:
            if not git_commit_exists(repository, revision):
                self.error(
                    phase,
                    "SDD173",
                    f"Completed task `{task_id}` evidence revision/checkpoint `{revision}` does not exist in target repository `{repository}`.",
                    "Record an existing clean full native Git revision/checkpoint in the completed task's evidence before completing the phase.",
                    line,
                )
                continue
            included_at_endpoint = subprocess.run(
                ["git", "-C", str(repository), "merge-base", "--is-ancestor", revision, identities[1]],
                check=False,
                capture_output=True,
            )
            if included_at_endpoint.returncode != 0:
                self.error(
                    phase,
                    "SDD173",
                    f"Git phase review range `{frozen}` omits completed task `{task_id}` evidence revision/checkpoint `{revision}` because it is not an ancestor of the endpoint.",
                    "Use a frozen range whose endpoint descends from every completed task evidence revision/checkpoint.",
                    line,
                )
                continue
            at_or_before_base = subprocess.run(
                ["git", "-C", str(repository), "merge-base", "--is-ancestor", revision, identities[0]],
                check=False,
                capture_output=True,
            )
            if at_or_before_base.returncode == 0:
                self.error(
                    phase,
                    "SDD173",
                    f"Git phase review range `{frozen}` omits completed task `{task_id}` evidence revision/checkpoint `{revision}` because it is at or before the range base.",
                    "Move the frozen range base before every completed task evidence revision/checkpoint.",
                    line,
                )

    def _is_valid_phase_review(self, review: Artifact, phase: Artifact) -> bool:
        return (
            normalized(review.meta.get("review_of"))
            in {normalized(phase.rel), normalized(phase.rel.removesuffix(".md"))}
            and review.meta.get("review_scope") == "phase"
            and review.meta.get("frozen") is True
            and review.meta.get("verdict") == "Aligned"
            and review.status == "resolved"
            and isinstance(review.meta.get("rev"), str)
            and bool(review.meta["rev"])
            and not phase_review_schema_errors(review.meta)
        )

    def _verify_phase_review_planning_revision(
        self, phase: Artifact, review: Artifact, line: int
    ) -> None:
        """Bind the phase gate to lifecycle-normalized planning artifacts in Git."""
        plan_name = self._plan_name(phase)
        plan = self.by_path.get(f"Plans/{plan_name}/README.md") if plan_name else None
        if plan is None:
            self.error(
                phase,
                "SDD174",
                "Phase review cannot validate its plan README at the reviewed planning revision.",
                "Ensure the reviewed phase belongs to a discoverable plan README before completing the phase.",
                line,
            )
            return
        repository = git_worktree_root(self.root)
        if repository is None:
            self.error(
                phase,
                "SDD174",
                "Phase review requires a Git planning-root lifecycle adapter.",
                "Keep the phase non-complete until the planning root is a Git worktree.",
                line,
            )
            return
        revision = review.meta.get("reviewed_planning_revision")
        if not isinstance(revision, str) or not re.fullmatch(r"[0-9a-fA-F]{40}", revision):
            return
        if not git_commit_exists(repository, revision):
            self.error(
                phase,
                "SDD174",
                f"Final review `{review.rel}` planning revision `{revision}` does not exist in planning Git history.",
                "Record an existing full planning Git commit in `reviewed_planning_revision`.",
                line,
            )
            return
        if self.identity_mode != "historical":
            ancestor = subprocess.run(
                ["git", "-C", str(repository), "merge-base", "--is-ancestor", revision, "HEAD"],
                check=False,
                capture_output=True,
            )
            if ancestor.returncode != 0:
                self.error(
                    phase,
                    "SDD174",
                    f"Final review `{review.rel}` planning revision `{revision}` is not an ancestor of planning HEAD.",
                    "Use a reviewed planning revision retained by the current planning Git history.",
                    line,
                )
                return
        for current, label in ((phase, "phase"), (plan, "plan README")):
            try:
                relative = current.path.resolve().relative_to(repository).as_posix()
            except ValueError:
                self.error(
                    phase,
                    "SDD174",
                    f"Final review `{review.rel}` cannot load {label} outside the planning Git worktree.",
                    "Store phase and plan lifecycle artifacts under the planning root.",
                    line,
                )
                continue
            historical = self._git_lifecycle_normalized_artifact(repository, revision, relative, current.kind)
            if historical is None:
                self.error(
                    phase,
                    "SDD174",
                    f"Final review `{review.rel}` cannot load {label} at planning revision `{revision}`.",
                    "Review a planning commit that contains the current phase and plan README.",
                    line,
                )
            elif self.identity_mode != "historical":
                try:
                    current_normalized = lifecycle_normalized_artifact(current)
                except ValueError:
                    self.error(
                        phase,
                        "SDD174",
                        f"Final review `{review.rel}` cannot normalize current lifecycle {label} content.",
                        "Use block-style single-line lifecycle fields before completing the phase review.",
                        line,
                    )
                    continue
                if historical != current_normalized:
                    self.error(
                        phase,
                        "SDD174",
                        f"Final review `{review.rel}` does not match current lifecycle-normalized {label} content.",
                        "Rerun the four-lane review after changing phase or plan intent.",
                        line,
                    )

    def _plan_phase_identities(self, artifact: Artifact, phases: list[Any]) -> None:
        plan_evidence = self.sections(artifact).get("Plan Completion Evidence")
        if not plan_evidence:
            return
        expected: dict[str, str | None] = {}
        for phase in phases:
            if not isinstance(phase, dict) or phase.get("status") != "complete" or not isinstance(phase.get("doc"), str):
                continue
            phase_id = str(phase.get("id", ""))
            target = self.by_path.get((Path(artifact.rel).parent / phase["doc"]).as_posix())
            if not target:
                continue
            sections = self.sections(target)
            phase_body = sections.get("Phase Completion Evidence", (1, ""))[1]
            review = parse_final_aligned_review(markdown_scalar(evidence_value(phase_body, "Final aligned review")))
            expected[phase_id] = (
                markdown_scalar(evidence_value(phase_body, "Revision / checkpoint")),
                review[0] if review else None,
            )
        self._completed_identities(
            artifact,
            plan_evidence[1],
            "Completed phase identities",
            expected,
            plan_evidence[0],
            "phase",
        )
        self._verify_git_plan_phase_checkpoints(
            artifact, phases, plan_evidence[1], plan_evidence[0]
        )

    def _verify_git_plan_phase_checkpoints(
        self, plan: Artifact, phases: list[Any], body: str, line: int
    ) -> None:
        """Bind completed phase Git checkpoints to the plan checkpoint."""
        plan_vcs = markdown_scalar(evidence_value(body, "VCS"))
        if plan_vcs not in {"git", "git-worktree"}:
            return
        plan_checkpoint = markdown_scalar(
            evidence_value(body, "Revision / checkpoint")
        )
        repository = self._repo_for_artifact(plan)
        if detected_scm(repository) != "git":
            self.error(
                plan,
                "SDD175",
                f"Git plan evidence targets `{repository}`, which has no Git identity adapter.",
                "Record plan Git evidence only for its Git target repository.",
                line,
            )
            return
        if not plan_checkpoint or not re.fullmatch(r"[0-9a-fA-F]{40}", plan_checkpoint):
            self.error(
                plan,
                "SDD175",
                "Git plan completion requires a full native Git `Revision / checkpoint`.",
                "Record the target repository's exact full native Git revision/checkpoint; a validated integration merge is allowed.",
                line,
            )
            return
        if not git_commit_exists(repository, plan_checkpoint):
            self.error(
                plan,
                "SDD175",
                f"Plan Git checkpoint `{plan_checkpoint}` does not exist in target repository `{repository}`.",
                "Record an existing target-repository Git checkpoint.",
                line,
            )
            return
        for phase_entry in phases:
            if (
                not isinstance(phase_entry, dict)
                or phase_entry.get("status") != "complete"
                or not isinstance(phase_entry.get("doc"), str)
            ):
                continue
            phase_id = str(phase_entry.get("id", ""))
            phase = self.by_path.get(
                (Path(plan.rel).parent / phase_entry["doc"]).as_posix()
            )
            if phase is None:
                continue
            phase_repository = self._repo_for_artifact(phase)
            if phase_repository.resolve() != repository.resolve():
                self.error(
                    plan,
                    "SDD175",
                    f"Completed phase `{phase_id}` targets `{phase_repository}`, not plan target `{repository}`; cross-repository checkpoint ordering is unsupported.",
                    "Keep all completed phase and plan Git checkpoints in one target repository.",
                    line,
                )
                continue
            phase_body = self.sections(phase).get("Phase Completion Evidence", (1, ""))[1]
            phase_vcs = markdown_scalar(evidence_value(phase_body, "VCS"))
            phase_checkpoint = markdown_scalar(
                evidence_value(phase_body, "Revision / checkpoint")
            )
            if phase_vcs not in {"git", "git-worktree"}:
                self.error(
                    plan,
                    "SDD175",
                    f"Completed phase `{phase_id}` VCS `{phase_vcs or 'missing'}` is incompatible with Git plan evidence.",
                    "Record a Git/git-worktree phase checkpoint in the plan target repository.",
                    line,
                )
                continue
            if not phase_checkpoint or not re.fullmatch(
                r"[0-9a-fA-F]{40}", phase_checkpoint
            ):
                self.error(
                    plan,
                    "SDD175",
                    f"Completed phase `{phase_id}` lacks a full native Git revision/checkpoint for Git plan evidence.",
                    "Record the phase's exact full native Git revision/checkpoint; a validated integration merge is allowed.",
                    line,
                )
                continue
            if not git_commit_exists(repository, phase_checkpoint):
                self.error(
                    plan,
                    "SDD175",
                    f"Completed phase `{phase_id}` Git checkpoint `{phase_checkpoint}` does not exist in target repository `{repository}`.",
                    "Record an existing phase Git checkpoint in the plan target repository.",
                    line,
                )
                continue
            ancestor = subprocess.run(
                [
                    "git",
                    "-C",
                    str(repository),
                    "merge-base",
                    "--is-ancestor",
                    phase_checkpoint,
                    plan_checkpoint,
                ],
                check=False,
                capture_output=True,
            )
            if ancestor.returncode != 0:
                self.error(
                    plan,
                    "SDD175",
                    f"Completed phase `{phase_id}` Git checkpoint `{phase_checkpoint}` is not an ancestor of plan checkpoint `{plan_checkpoint}`.",
                    "Record a plan checkpoint equal to or descending from every completed phase checkpoint.",
                    line,
                )

    def _completed_identities(
        self,
        artifact: Artifact,
        body: str,
        heading: str,
        expected: dict[str, Any],
        line: int,
        kind: str,
    ) -> None:
        """Require one visible concise identity entry for every completed child."""
        sections = heading_bodies(body, 3, heading)
        invalid = len(sections) != 1
        entries: dict[str, Any] = {}
        if len(sections) == 1:
            pattern = (
                r"\s*-\s+`(?P<identity>[^`\s]+)`: `(?P<checkpoint>[^`;]+)`\s*"
                if kind == "task"
                else r"\s*-\s+`(?P<identity>[^`\s]+)`: `(?P<checkpoint>[^`;]+)`; review: `(?P<review>[^`;]+)`\s*"
            )
            for value in sections[0].splitlines():
                match = re.fullmatch(pattern, value)
                if not match:
                    invalid = True
                    continue
                identity = match.group("identity")
                recorded: Any = (
                    (match.group("checkpoint"), match.group("review"))
                    if kind == "phase"
                    else match.group("checkpoint")
                )
                if identity in entries:
                    invalid = True
                entries[identity] = recorded
        if set(entries) != set(expected) or any(entries.get(identity) != value for identity, value in expected.items()):
            invalid = True
        if invalid:
            self.error(
                artifact,
                "SDD157" if kind == "task" else "SDD158",
                f"{heading} must contain one exact identity entry for every completed {kind}.",
                f"Use exactly one `### {heading}` section with each completed {kind}'s checkpoint" + (" and final review path." if kind == "phase" else "."),
                line,
            )

    def _evidence(self, artifact: Artifact, status: str, name: str, line: int, body: str) -> None:
        visible_body = visible_markdown(body)
        pending = PENDING in visible_body
        if status == "complete" and pending:
            self.error(artifact, "SDD070", f"Complete `{name}` is pending.", "Replace the marker with retrospective evidence.", line)
            return
        if pending:
            return
        labels = ("Verified", "Repository", "VCS", "Revision / checkpoint", "Identity recheck")
        for label in labels:
            values = evidence_values(body, label)
            if len(values) != 1 or not markdown_scalar(values[0] if values else None):
                self.error(artifact, "SDD071", f"`{name}` must contain exactly one populated visible `{label}` label.", f"Keep one populated `{label}` evidence line outside comments and fenced blocks.", line)
        verified = markdown_scalar(evidence_value(body, "Verified"))
        if verified and not re.fullmatch(r"\d{4}-\d{2}-\d{2}", verified):
            self.error(artifact, "SDD072", f"`{name}` has invalid verification date `{verified}`.", "Use YYYY-MM-DD.", line)
        vcs = markdown_scalar(evidence_value(body, "VCS")) or ""
        if vcs and vcs not in {"git", "git-worktree", "perforce", "none"}:
            self.error(artifact, "SDD072", f"`{name}` has invalid VCS `{vcs}`.", "Use git, git-worktree, perforce, or none.", line)
        revision = markdown_scalar(evidence_value(body, "Revision / checkpoint")) or ""
        task_match = re.fullmatch(r"Task\s+(\S+)\s+Completion Evidence", name)
        if status == "complete" and task_match:
            self._task_review_evidence(artifact, name, body, revision, vcs, line)
        if vcs in {"git", "git-worktree"} and revision and not re.fullmatch(r"[0-9a-fA-F]{40}", revision):
            self.error(artifact, "SDD072", f"`{name}` has invalid Git revision/checkpoint `{revision}`.", "Record exactly one clean full native Git revision/checkpoint.", line)
        if vcs == "none" and revision and revision != "none":
            self.error(artifact, "SDD072", f"`{name}` with VCS `none` has revision/base `{revision}`.", "Use `none`.", line)
        repository = markdown_scalar(evidence_value(body, "Repository"))
        expected_repository = self._repo_for_artifact(artifact)
        if repository:
            recorded_repository = Path(repository).expanduser().resolve()
            if recorded_repository != expected_repository:
                self.error(artifact, "SDD072", f"`{name}` repository `{recorded_repository}` does not match target `{expected_repository}`.", "Record the exact resolved target repository root.", line)
        removed_labels = ("Fallback reason", "Evidence exclusions", "Governing intent", "Ignored inputs", "Directory inputs", "Content snapshot")
        if any(evidence_value(body, label) is not None for label in removed_labels):
            self.error(artifact, "SDD074", f"`{name}` uses removed synthetic identity fields.", "Remove fallback, snapshot, exclusion, and inventory labels; record the native SCM checkpoint only.", line)
        if evidence_value(body, "Revision / base") is not None:
            self.error(artifact, "SDD074", f"`{name}` uses removed `Revision / base` identity evidence.", "Use only `Revision / checkpoint` with the native SCM identity.", line)
        if vcs in {"git", "git-worktree"} and revision:
            compare_current = self.identity_mode != "historical"
            self._verify_clean_git_identity(artifact, revision, name, line, compare_current)
        elif status == "complete":
            self.error(artifact, "SDD172", f"`{name}` cannot complete with VCS `{vcs or 'missing'}` because no validated native identity adapter is available.", "Keep the entity non-complete until a validated native SCM and lifecycle adapter are available.", line)
        if status == "complete":
            self._verify_evidence_committed(artifact, name, body, line)
        rows = evidence_rows(body)
        if not rows:
            self.error(artifact, "SDD072", f"`{name}` has no conforming command or tool evidence row.", "Add a four-column command or tool row with PASS and specific observable evidence.", line)
        else:
            for row_kind, row in rows:
                if not row[2].startswith("PASS"):
                    self.error(artifact, "SDD072", f"`{name}` contains non-passing result `{row[2]}`.", "Every required command and inspection row must record PASS.", line)
                if row_kind == "command" and not re.search(r"\bexit\s+0\b", row[2], re.IGNORECASE):
                    self.error(artifact, "SDD072", f"`{name}` command row lacks explicit `exit 0`.", "Record PASS with the command exit status.", line)
        if re.search(r"\b(?:FAIL|FAILED|exit\s+[1-9]\d*)\b", visible_body, re.IGNORECASE):
            self.error(artifact, "SDD073", f"`{name}` contains failing evidence.", "Return it to a non-complete status until final checks pass.", line)
        recheck = evidence_value(body, "Identity recheck") or ""
        if recheck and (
            not re.search(r"\bmatch(?:ed|es|ing)?\b", recheck, re.IGNORECASE)
            or not re.search(r"\b\d{4}-\d{2}-\d{2}[T ][0-2]\d:[0-5]\d", recheck)
        ):
            self.error(artifact, "SDD075", f"`{name}` recheck lacks a timestamped matching result.", "Record the exact tool, ISO timestamp, and matching identity.", line)
        if vcs in {"git", "git-worktree"} and revision and revision.lower() not in recheck.lower():
            self.error(artifact, "SDD075", f"`{name}` recheck does not name tested Git revision/checkpoint `{revision}`.", "Record the exact tested Git revision/checkpoint in the identity-recheck procedure and result.", line)

    def _task_review_evidence(self, artifact: Artifact, name: str, body: str, revision: str, vcs: str, line: int) -> None:
        """Require a durable, auditable focused review for every complete task."""
        focused_values = evidence_values(body, "Focused review")
        reviewed_values = evidence_values(body, "Reviewed candidate / final")
        result_values = evidence_values(body, "Review result")
        focused_raw = focused_values[0] if len(focused_values) == 1 else None
        focused = markdown_scalar(focused_raw)
        reviewed = markdown_scalar(reviewed_values[0] if len(reviewed_values) == 1 else None)
        result = markdown_scalar(result_values[0] if len(result_values) == 1 else None)
        missing = [
            label
            for label, values, value in (
                ("Focused review", focused_values, focused),
                ("Reviewed candidate / final", reviewed_values, reviewed),
                ("Review result", result_values, result),
            )
            if len(values) != 1 or not value
        ]
        if missing:
            self.error(
                artifact,
                "SDD169",
                f"`{name}` must contain exactly one populated visible auditable task-review label: {', '.join(missing)}.",
                "Keep one populated visible Focused review, Reviewed candidate / final, and Review result label.",
                line,
            )
            return
        if not valid_focused_review_syntax(focused_raw):
            self.error(
                artifact,
                "SDD169",
                f"`{name}` focused review must contain an exact nonempty command/tool followed by the complete-task-diff statement.",
                "Use `Focused review: `<exact command/tool>`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary`.",
                line,
            )
        if vcs in {"git", "git-worktree"} and revision and not revision.endswith("-dirty"):
            self._valid_git_task_review_identity(artifact, name, focused, reviewed, revision, line)
        elif reviewed != revision:
            self.error(
                artifact,
                "SDD169",
                f"`{name}` reviewed candidate/final `{reviewed}` does not exactly equal native `Revision / checkpoint` `{revision}`.",
                "For this SCM, record the exact native revision/checkpoint reviewed; no deterministic alternate review-identity adapter is available.",
                line,
            )
        if result != "PASS/Aligned":
            self.error(
                artifact,
                "SDD169",
                f"`{name}` review result `{result}` is not `PASS/Aligned`.",
                "Record `- Review result: PASS/Aligned` only after the focused review passes.",
                line,
            )

    def _valid_git_task_review_identity(self, artifact: Artifact, name: str, focused: str, reviewed: str, revision: str, line: int) -> bool:
        """Validate clean-Git task review identity against the target repository."""
        repository = self._repo_for_artifact(artifact)
        if reviewed == revision:
            identities = (reviewed,)
            expected_review_identity = revision
        else:
            match = re.fullmatch(r"diff: ([0-9a-fA-F]{40})\.\.([0-9a-fA-F]{40})", reviewed)
            if not match:
                self.error(
                    artifact,
                    "SDD169",
                    f"`{name}` reviewed candidate/final must be the exact clean Git task commit or `diff: <full40>..<full40>`.",
                    "Record the full task commit, or a full-commit diff range ending at `Revision / checkpoint`.",
                    line,
                )
                return False
            identities = (match.group(1), match.group(2))
            expected_review_identity = f"{identities[0]}..{identities[1]}"
            if identities[0] == identities[1]:
                self.error(
                    artifact,
                    "SDD169",
                    f"`{name}` reviewed Git diff range has identical base and final commits.",
                    "Use distinct full commits with the direct first parent of the task commit as base.",
                    line,
                )
                return False
            if identities[-1] != revision:
                self.error(
                    artifact,
                    "SDD169",
                    f"`{name}` reviewed Git diff endpoint `{identities[-1]}` does not equal `Revision / checkpoint` `{revision}`.",
                    "Use a reviewed diff range whose exact endpoint is the task revision.",
                    line,
                )
                return False
        if not all(git_commit_exists(repository, identity) for identity in identities):
            self.error(
                artifact,
                "SDD169",
                f"`{name}` reviewed Git identity names a commit absent from target repository `{repository}`.",
                "Use only full reviewed commits that exist in the target repository.",
                line,
            )
            return False
        parents = subprocess.run(
            ["git", "-C", str(repository), "show", "-s", "--format=%P", revision],
            check=False,
            capture_output=True,
            text=True,
        )
        if parents.returncode != 0 or len(parents.stdout.split()) > 1:
            self.error(
                artifact,
                "SDD169",
                f"`{name}` clean Git implementation revision `{revision}` is a merge commit.",
                "Record a non-merge, independently bisectable task implementation commit and review its complete diff.",
                line,
            )
            return False
        if len(identities) == 2:
            parent = subprocess.run(
                ["git", "-C", str(repository), "rev-parse", f"{revision}^"],
                check=False,
                capture_output=True,
                text=True,
            )
            if parent.returncode != 0 or parent.stdout.strip() != identities[0]:
                self.error(
                    artifact,
                    "SDD169",
                    f"`{name}` reviewed Git diff base `{identities[0]}` is not the direct first parent of task revision `{revision}`.",
                    "Use `diff: <task revision first parent>..<task revision>` for a ranged focused review.",
                    line,
                )
                return False
        expected_command = (
            f"git show {expected_review_identity}"
            if len(identities) == 1
            else f"git diff {expected_review_identity}"
        )
        recorded_command = focused
        recorded = re.fullmatch(
            r"`(?P<command>[^`;\n]+)`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary",
            focused,
        )
        if recorded:
            recorded_command = recorded.group("command")
        if recorded_command != expected_command:
            self.error(
                artifact,
                "SDD169",
                f"`{name}` focused review command must be `{expected_command}` for reviewed identity `{expected_review_identity}`.",
                "For clean Git, use `git show <full task commit>` or `git diff <full base>..<full task commit>` with no extra operands.",
                line,
            )
            return False
        return True

    def _verify_clean_git_identity(self, artifact: Artifact, revision: str, name: str, line: int, compare_current: bool) -> None:
        repository = self._repo_for_artifact(artifact)

        def git(*args: str) -> subprocess.CompletedProcess[bytes]:
            return subprocess.run(["git", "-C", str(repository), *args], check=False, capture_output=True)

        inside = git("rev-parse", "--is-inside-work-tree")
        if inside.returncode != 0 or inside.stdout.strip() != b"true":
            self.error(artifact, "SDD072", f"`{name}` records Git but `{repository}` is not a Git worktree.", "Correct the repository/VCS evidence.", line)
            return
        if not git_commit_exists(repository, revision):
            self.error(artifact, "SDD072", f"`{name}` Git revision/checkpoint `{revision}` is not a commit in `{repository}`.", "Record an existing full native Git revision/checkpoint, not a tag or another Git object.", line)
            return
        if not compare_current:
            return
        ancestor = git("merge-base", "--is-ancestor", revision, "HEAD")
        if ancestor.returncode != 0:
            self.error(artifact, "SDD072", f"`{name}` Git revision/checkpoint `{revision}` is not an ancestor of current HEAD.", "Check out a descendant containing the completed entity or use historical identity mode for an archival audit.", line)

    def _verify_evidence_committed(self, artifact: Artifact, name: str, body: str, line: int) -> None:
        """Dispatch lifecycle durability checks by planning-root SCM adapter."""
        scm = self._planning_root_scm()
        if scm == "git":
            self._verify_git_evidence_committed(artifact, name, body, line)
            return
        self.error(
            artifact,
            "SDD171",
            f"`{name}` is complete but no validated durable lifecycle adapter is available for planning-root SCM `{scm}`.",
            "Keep the entity non-complete until a validated durable lifecycle adapter is available.",
            line,
        )

    def _verify_git_evidence_committed(self, artifact: Artifact, name: str, body: str, line: int) -> None:
        """Git lifecycle adapter: require completion evidence at the current HEAD."""
        repository = git_worktree_root(self.root)
        if repository is None:
            self.error(artifact, "SDD072", f"`{name}` is complete but the Git planning root is not a worktree.", "Use a Git worktree and commit the lifecycle/evidence artifact before finalizing completion.", line)
            return
        try:
            relative = artifact.path.resolve().relative_to(repository).as_posix()
        except ValueError:
            self.error(artifact, "SDD072", f"`{name}` planning artifact cannot be resolved inside its Git worktree.", "Commit the lifecycle/evidence artifact before finalizing completion.", line)
            return
        tracked = subprocess.run(
            ["git", "-C", str(repository), "show", f"HEAD:{relative}"],
            check=False,
            capture_output=True,
        )
        if tracked.returncode != 0:
            self.error(artifact, "SDD072", f"`{name}` completion evidence is not committed at HEAD.", "Create the separate scoped lifecycle/evidence commit before finalizing completion.", line)
            return
        committed_source = tracked.stdout.decode("utf-8", errors="replace")
        committed_meta = parse_frontmatter_source(committed_source)
        source_lines = committed_source.splitlines(keepends=True)
        frontmatter_end = next(
            (index for index, value in enumerate(source_lines[1:], 1) if value.strip() == "---"),
            None,
        )
        if committed_meta is None or frontmatter_end is None:
            self.error(artifact, "SDD072", f"`{name}` committed planning artifact is malformed.", "Commit a valid populated lifecycle artifact before finalizing completion.", line)
            return
        committed = Artifact(
            artifact.path,
            artifact.rel,
            committed_meta,
            "".join(source_lines[frontmatter_end + 1 :]),
            committed_source,
            frontmatter_end + 2,
        )
        committed_body: str | None = None
        lifecycle_complete = False
        task_match = re.fullmatch(r"Task\s+(\S+)\s+Completion Evidence", name)
        if task_match:
            task_id = task_match.group(1)
            tasks = committed.meta.get("tasks", [])
            lifecycle_complete = any(
                isinstance(task, dict)
                and str(task.get("id", "")) == task_id
                and task.get("status") == "complete"
                for task in tasks
            ) if isinstance(tasks, list) else False
            sections = self.sections(committed)
            heading = next(
                (value for value in sections if re.match(rf"^{re.escape(task_id)}(?:\s*:|\s|$)", value)),
                None,
            )
            if heading:
                committed_body = completion_evidence_body(sections[heading][1])
                subtasks = heading_bodies(sections[heading][1], 3, "Subtasks")
                lifecycle_complete = lifecycle_complete and len(subtasks) == 1 and not has_unchecked_checkbox(subtasks[0])
        elif name == "Phase Completion Evidence":
            lifecycle_complete = committed.status == "complete"
            committed_body = next(iter(heading_bodies(committed.body, 2, name)), None)
            criteria = heading_bodies(committed.body, 2, "Acceptance Criteria")
            lifecycle_complete = lifecycle_complete and bool(criteria) and not has_unchecked_checkbox(criteria[0])
            plan_name = self._plan_name(artifact)
            plan_path = self.root / "Plans" / plan_name / "README.md" if plan_name else None
            plan_repository = git_root(plan_path) if plan_path else None
            if plan_path is None or plan_repository != repository:
                lifecycle_complete = False
            else:
                plan_relative = plan_path.resolve().relative_to(plan_repository).as_posix()
                plan_at_head = subprocess.run(
                    ["git", "-C", str(plan_repository), "show", f"HEAD:{plan_relative}"],
                    check=False,
                    capture_output=True,
                )
                plan_meta = parse_frontmatter_source(
                    plan_at_head.stdout.decode("utf-8", errors="replace")
                ) if plan_at_head.returncode == 0 else None
                phases = plan_meta.get("phases", []) if plan_meta else []
                lifecycle_complete = lifecycle_complete and any(
                    isinstance(phase, dict)
                    and str(phase.get("id", "")) == str(artifact.meta.get("phase", ""))
                    and phase.get("status") == "complete"
                    for phase in phases
                ) if isinstance(phases, list) else False
        elif name == "Plan Completion Evidence":
            lifecycle_complete = committed.status == "complete"
            committed_body = next(iter(heading_bodies(committed.body, 2, name)), None)
        if not lifecycle_complete or committed_body is None:
            self.error(artifact, "SDD072", f"`{name}` lifecycle completion is not committed at HEAD.", "Commit the complete status, checked criteria/subtasks, and evidence in the scoped lifecycle commit.", line)
            return
        if no_comments(body).strip() != no_comments(committed_body).strip():
            self.error(artifact, "SDD072", f"`{name}` completion evidence differs from its committed section.", "Commit the populated evidence and lifecycle status in a scoped bookkeeping commit.", line)

    def _verify_git_phase_review_committed(self, phase: Artifact, review: Artifact, frozen: str, line: int) -> None:
        """Git phase-review adapter: the cited frozen review must be HEAD bytes."""
        repository = git_worktree_root(self.root)
        if repository is None:
            self.error(phase, "SDD170", "Final aligned review cannot be checked because the Git planning root is not a worktree.", "Use a Git worktree and commit the phase review before phase completion.", line)
            return
        try:
            relative = review.path.resolve().relative_to(repository).as_posix()
        except ValueError:
            self.error(phase, "SDD170", f"Final aligned review `{review.rel}` is outside the Git planning worktree.", "Store and commit the phase review in the planning root before phase completion.", line)
            return
        tracked = subprocess.run(
            ["git", "-C", str(repository), "show", f"HEAD:{relative}"],
            check=False,
            capture_output=True,
        )
        if tracked.returncode != 0:
            self.error(phase, "SDD170", f"Final aligned review `{review.rel}` is not committed at HEAD.", "Commit the exact final review artifact in the Git lifecycle record before phase completion.", line)
            return
        committed_source = tracked.stdout.decode("utf-8", errors="replace")
        if committed_source != review.source:
            self.error(phase, "SDD170", f"Final aligned review `{review.rel}` differs from its committed bytes at HEAD.", "Commit the exact reviewed artifact bytes, including its frontmatter, before phase completion.", line)
            return
        committed_meta = parse_frontmatter_source(committed_source)
        if committed_meta is None:
            self.error(phase, "SDD170", f"Committed final aligned review `{review.rel}` has malformed frontmatter.", "Commit a valid resolved frozen Aligned review artifact at HEAD.", line)
            return
        committed = Artifact(review.path, review.rel, committed_meta, review.body, committed_source, review.body_line)
        if not self._is_valid_phase_review(committed, phase) or committed.meta.get("rev") != frozen:
            self.error(phase, "SDD170", f"Committed final aligned review `{review.rel}` does not establish resolved frozen Aligned four-lane review state for `{frozen}`.", "Commit frontmatter with review_of, rev, review_scope: phase, frozen: true, verdict: Aligned, all four lanes, and status: resolved.", line)

    def _verify_git_phase_post_review_state(self, phase: Artifact, review: Artifact, endpoint: str, line: int) -> None:
        """Enforce current-state post-review gates outside archival audits."""
        if self.identity_mode == "historical":
            return
        repository = self._repo_for_artifact(phase)
        target_root = git_worktree_root(repository)
        if target_root is None:
            self.error(phase, "SDD173", f"Git phase completion target `{repository}` is not a Git worktree.", "Use a target Git worktree for phase completion.", line)
            return
        status = subprocess.run(
            ["git", "-C", str(target_root), "status", "--porcelain", "--ignore-submodules=none", "--untracked-files=all"],
            check=False,
            capture_output=True,
        )
        if status.returncode != 0 or status.stdout:
            self.error(phase, "SDD173", "Git phase completion requires the current target worktree to be clean after review.", "Commit only permitted lifecycle records, remove uncommitted changes, and rerun the full phase review after material changes.", line)
            return
        head = subprocess.run(
            ["git", "-C", str(target_root), "rev-parse", "HEAD"],
            check=False,
            capture_output=True,
            text=True,
        )
        if head.returncode != 0:
            self.error(phase, "SDD173", f"Git phase completion target `{target_root}` has no current HEAD.", "Use a target worktree with the reviewed endpoint checked into history.", line)
            return
        current = head.stdout.strip()
        allowed = self._git_phase_lifecycle_paths(phase, review, target_root)
        if not allowed:
            if current != endpoint:
                self.error(phase, "SDD173", "Phase lifecycle files are outside the target repository, so target HEAD must remain the reviewed endpoint.", "Keep target HEAD at the frozen review endpoint or rerun the full phase review after target changes.", line)
            return
        if current == endpoint:
            return
        descendant = subprocess.run(
            ["git", "-C", str(target_root), "merge-base", "--is-ancestor", endpoint, "HEAD"],
            check=False,
            capture_output=True,
        )
        if descendant.returncode != 0:
            self.error(phase, "SDD173", f"Reviewed endpoint `{endpoint}` is not an ancestor of current target HEAD.", "Check out a descendant of the reviewed endpoint or rerun the full phase review.", line)
            return
        commits = subprocess.run(
            ["git", "-C", str(target_root), "rev-list", f"{endpoint}..HEAD"],
            check=False,
            capture_output=True,
            text=True,
        )
        if commits.returncode != 0:
            self.error(phase, "SDD173", "Cannot inspect committed target changes after the frozen phase review.", "Repair the target Git worktree and rerun phase completion validation.", line)
            return
        paths: set[str] = set()
        for commit in commits.stdout.splitlines():
            changed = subprocess.run(
                ["git", "-C", str(target_root), "diff-tree", "--no-commit-id", "--name-only", "--no-renames", "-r", "-m", "-z", commit],
                check=False,
                capture_output=True,
            )
            if changed.returncode != 0:
                self.error(phase, "SDD173", "Cannot inspect every committed target change after the frozen phase review.", "Repair the target Git worktree and rerun phase completion validation.", line)
                return
            paths.update(value.decode("utf-8", errors="surrogateescape") for value in changed.stdout.split(b"\0") if value)
        material = sorted(paths - allowed)
        if material:
            self.error(phase, "SDD173", f"Committed target paths changed after the frozen phase review are not lifecycle-only: {', '.join(material)}.", "Rerun the full phase review after source, test, configuration, or other material changes.", line)
            return
        for path, kind in self._git_phase_lifecycle_intent_paths(phase, target_root):
            frozen_normalized = self._git_lifecycle_normalized_artifact(target_root, endpoint, path, kind)
            current_normalized = self._git_lifecycle_normalized_artifact(target_root, "HEAD", path, kind)
            if frozen_normalized is None or current_normalized is None:
                self.error(phase, "SDD173", f"Cannot compare canonical {kind} intent for lifecycle path `{path}` across the frozen phase review.", "Keep the governing phase and plan artifacts valid and present at both the frozen endpoint and HEAD, or rerun the full phase review.", line)
                return
            if frozen_normalized != current_normalized:
                self.error(phase, "SDD173", f"Lifecycle path `{path}` changes canonical {kind} intent after the frozen phase review.", "Do not change phase/plan scope, requirements, tasks, or acceptance text after review; rerun the full phase review.", line)
                return

    def _git_phase_lifecycle_intent_paths(self, phase: Artifact, target_root: Path) -> list[tuple[str, str]]:
        """Return governed lifecycle artifacts whose intent must remain frozen."""
        paths = [(phase.path, "phase")]
        plan_name = self._plan_name(phase)
        if plan_name:
            paths.append((self.root / "Plans" / plan_name / "README.md", "plan"))
        result: list[tuple[str, str]] = []
        for path, kind in paths:
            try:
                result.append((path.resolve().relative_to(target_root).as_posix(), kind))
            except ValueError:
                pass
        return result

    def _git_lifecycle_normalized_artifact(self, repository: Path, revision: str, relative: str, kind: str) -> bytes | None:
        """Load lifecycle-normalized artifact bytes from one immutable Git revision."""
        source = subprocess.run(
            ["git", "-C", str(repository), "show", f"{revision}:{relative}"],
            check=False,
            capture_output=True,
        )
        if source.returncode != 0:
            return None
        artifact = Artifact(repository / relative, relative, {"type": kind}, "", source.stdout.decode("utf-8", errors="replace"), 1)
        try:
            return lifecycle_normalized_artifact(artifact)
        except (StopIteration, ValueError):
            return None

    def _git_phase_lifecycle_paths(self, phase: Artifact, review: Artifact, target_root: Path) -> set[str]:
        """Return only the explicit phase lifecycle paths that live in this target."""
        paths = [phase.path, review.path]
        plan_name = self._plan_name(phase)
        if plan_name:
            paths.append(self.root / "Plans" / plan_name / "README.md")
            for artifact in self.artifacts:
                if artifact.kind == "debrief" and artifact.meta.get("plan") == plan_name and str(artifact.meta.get("phase")) == str(phase.meta.get("phase")):
                    paths.append(artifact.path)
        allowed: set[str] = set()
        for path in paths:
            try:
                allowed.add(path.resolve().relative_to(target_root).as_posix())
            except ValueError:
                pass
        return allowed

    def _review(self, artifact: Artifact) -> None:
        self._required(artifact, ("review_of", "rev"))
        if isinstance(artifact.meta.get("review_of"), str) and self.resolve(artifact.meta["review_of"]) is None:
            self.error(artifact, "SDD080", f"`review_of` `{artifact.meta['review_of']}` does not resolve.", "Point it at the reviewed artifact.")
        findings = artifact.meta.get("findings")
        followups = artifact.meta.get("followups")
        if not isinstance(findings, list):
            self.error(artifact, "SDD081", "`findings` must be a list.", "Use `findings: []` when empty.")
            return
        if not isinstance(followups, list):
            self.error(artifact, "SDD082", "`followups` must be a list.", "Use `followups: []` when empty.")
            followups = []
        if artifact.meta.get("review_scope") == "phase":
            for issue in phase_review_schema_errors(artifact.meta):
                self.error(
                    artifact,
                    "SDD167",
                    f"Phase review frontmatter is invalid: {issue}.",
                    "Set valid `review_mode` and exactly one PASS/Aligned lane_results entry with matching reviewed_identity and nonempty evidence for each stable lane.",
                )
        statuses: dict[str, str] = {}
        resolution = self.sections(artifact).get("Resolution Log", (1, ""))[1]
        finding_ids: list[str] = []
        for finding in findings:
            if not isinstance(finding, dict):
                self.error(artifact, "SDD083", "A finding is not a mapping.", "Add id, severity, title, and status.")
                continue
            finding_id = str(finding.get("id", ""))
            finding_ids.append(finding_id)
            statuses[finding_id] = str(finding.get("status", ""))
            for field in ("id", "severity", "title", "status"):
                if finding.get(field) in (None, ""):
                    self.error(artifact, "SDD083", f"Finding is missing `{field}`.", f"Add a nonempty `{field}`.")
            if not re.fullmatch(r"F-\d{2,}", finding_id):
                self.error(artifact, "SDD084", f"Invalid finding id `{finding_id}`.", "Use `F-NN`.")
            if finding.get("severity") not in {"critical", "major", "minor", "question"}:
                self.error(artifact, "SDD085", f"Finding `{finding_id}` has invalid severity.", "Use critical, major, minor, or question.")
            status = finding.get("status")
            if status not in FINDING_STATUS:
                self.error(artifact, "SDD086", f"Finding `{finding_id}` has invalid status `{status}`.", "Use an allowed finding status.")
            if not re.search(rf"^###\s+{re.escape(finding_id)}\b", artifact.body, re.MULTILINE):
                self.error(artifact, "SDD087", f"Finding `{finding_id}` has no body section.", f"Add `### {finding_id} — ...`.")
            if status != "open":
                entry = re.search(rf"^###\s+{re.escape(finding_id)}\s+—\s+([a-z-]+)\b", resolution, re.MULTILINE)
                if not entry:
                    self.error(artifact, "SDD088", f"Terminal finding `{finding_id}` has no resolution entry.", "Append a dated Resolution Log entry.")
                elif entry.group(1) != status:
                    self.error(artifact, "SDD089", f"Finding `{finding_id}` status disagrees with its resolution.", "Make both dispositions agree.")
        for value in duplicates(finding_ids):
            self.error(artifact, "SDD090", f"Duplicate finding id `{value}`.", "Assign a new append-only id.")
        has_open = any(value == "open" for value in statuses.values())
        if artifact.status == "resolved" and has_open:
            self.error(artifact, "SDD091", "Resolved review contains open findings.", "Resolve them or set review status to open.")
        tracked_findings: set[str] = set()
        followup_ids: list[str] = []
        plan_names = self._candidate_plan_names(artifact)
        for followup in followups:
            if not isinstance(followup, dict):
                self.error(artifact, "SDD092", "A follow-up is not a mapping.", "Add id, finding, summary, and tracked_in.")
                continue
            followup_id = str(followup.get("id", ""))
            finding_id = str(followup.get("finding", ""))
            followup_ids.append(followup_id)
            for field in ("id", "finding", "summary", "tracked_in"):
                if field not in followup:
                    self.error(artifact, "SDD092", f"Follow-up is missing `{field}`.", f"Add the `{field}` field.")
            if not re.fullmatch(r"FU-\d{2,}", followup_id):
                self.error(artifact, "SDD093", f"Invalid follow-up id `{followup_id}`.", "Use `FU-NN`.")
            if finding_id not in statuses:
                self.error(artifact, "SDD094", f"Follow-up `{followup_id}` references unknown `{finding_id}`.", "Reference a finding in this review.")
            tracked = followup.get("tracked_in")
            if not tracked:
                self.error(artifact, "SDD095", f"Follow-up `{followup_id}` is floating.", "Create a plan task and set `tracked_in`.")
            else:
                matches = [plan for plan in plan_names if (plan, str(tracked)) in self.tasks]
                if not matches:
                    self.error(artifact, "SDD096", f"Follow-up `{followup_id}` points to unknown task `{tracked}`.", "Reference an existing task in a related plan.")
                elif len(matches) > 1:
                    self.error(artifact, "SDD096", f"Follow-up `{followup_id}` task `{tracked}` is ambiguous across plans {matches}.", "Link the review to one plan or use an unambiguous tracked task.")
                else:
                    tracked_findings.add(finding_id)
        for value in duplicates(followup_ids):
            self.error(artifact, "SDD097", f"Duplicate follow-up id `{value}`.", "Assign a new append-only id.")
        for finding_id, status in statuses.items():
            if status != "deferred" or finding_id in tracked_findings:
                continue
            entry = resolution_entry(resolution, finding_id)
            cited = set(re.findall(r"\b\d+\.\d+\b", entry))
            if not any((plan_name, task_id) in self.tasks for plan_name in plan_names for task_id in cited):
                self.error(artifact, "SDD098", f"Deferred finding `{finding_id}` is untracked.", "Cite an existing task in the reviewed plan or add a tracked follow-up.")
        self._review_supersession(artifact)

    def _review_supersession(self, artifact: Artifact) -> None:
        if artifact.status == "superseded" and not artifact.meta.get("superseded_by"):
            self.error(artifact, "SDD099", "Superseded review lacks `superseded_by`.", "Link the replacing review.")
        if artifact.meta.get("superseded_by") and artifact.status != "superseded":
            self.error(artifact, "SDD099", f"Review with `superseded_by` has status `{artifact.status}`.", "Set its artifact status to `superseded`.")
        for field, reverse in (("supersedes", "superseded_by"), ("superseded_by", "supersedes")):
            value = artifact.meta.get(field)
            if not value:
                continue
            target = self.resolve(str(value))
            if target is None or target.kind != "review":
                self.error(artifact, "SDD100", f"Review `{field}` `{value}` does not resolve.", "Point it at an existing review.")
            elif target.meta.get(reverse) not in {artifact.rel, artifact.rel.removesuffix(".md")}:
                self.error(artifact, "SDD101", f"Review `{field}` link is not reciprocated.", f"Add matching `{reverse}`.")
            elif normalized(target.meta.get("review_of")) != normalized(artifact.meta.get("review_of")):
                self.error(artifact, "SDD102", f"Review `{field}` links reviews of different targets.", "Link only reviews of the same normalized `review_of` target.")
            elif field == "supersedes" and target.status != "superseded":
                self.error(artifact, "SDD102", f"Superseded review `{value}` still has status `{target.status}`.", "Set the replaced review status to `superseded`.")

    def _plan_name(self, artifact: Artifact) -> str | None:
        if artifact.kind == "phase" and artifact.meta.get("plan"):
            return str(artifact.meta["plan"])
        parts = Path(artifact.rel).parts
        if len(parts) >= 2 and parts[0] == "Plans":
            return parts[1]
        review_of = artifact.meta.get("review_of")
        if isinstance(review_of, str):
            review_parts = Path(review_of).parts
            if len(review_parts) >= 2 and review_parts[0] == "Plans":
                return review_parts[1]
        return None

    def _candidate_plan_names(self, artifact: Artifact) -> set[str]:
        direct = self._plan_name(artifact)
        if direct:
            return {direct}
        targets: list[Artifact] = []
        review_of = artifact.meta.get("review_of")
        if isinstance(review_of, str):
            target = self.resolve(review_of)
            if target:
                targets.append(target)
        related = artifact.meta.get("related", [])
        if isinstance(related, list):
            for reference in related:
                target = self.resolve(reference) if isinstance(reference, str) else None
                if target:
                    targets.append(target)
        result: set[str] = set()
        for plan in (item for item in self.artifacts if item.kind == "plan"):
            plan_name = self._plan_name(plan)
            if plan_name and any(
                plan.rel == target.rel
                or self._artifacts_connected(plan, target)
                or self._artifacts_connected(target, plan)
                for target in targets
            ):
                result.add(plan_name)
        return result

    def _ledger(self, artifact: Artifact) -> None:
        entries = artifact.meta.get("decisions")
        if not isinstance(entries, list):
            self.error(artifact, "SDD110", "`decisions` must be a list.", "Use `decisions: []` when empty.")
            return
        for entry in entries:
            if not isinstance(entry, dict):
                self.error(artifact, "SDD111", "A decision is not a mapping.", "Use the decision entry schema.")
                continue
            for field in ("id", "kind", "status", "date", "decided_by", "statement", "rationale"):
                if entry.get(field) in (None, ""):
                    self.error(artifact, "SDD112", f"Decision is missing `{field}`.", f"Add a nonempty `{field}`.")
            decision_id = str(entry.get("id", ""))
            if not re.fullmatch(r"D-\d{4,}", decision_id):
                self.error(artifact, "SDD113", f"Invalid decision id `{decision_id}`.", "Use `D-NNNN`.")
            if entry.get("kind") not in {"decision", "definition", "answered-question", "assumption"}:
                self.error(artifact, "SDD114", f"Decision `{decision_id}` has invalid kind.", "Use an allowed decision kind.")
            if entry.get("status") not in DECISION_STATUS:
                self.error(artifact, "SDD115", f"Decision `{decision_id}` has invalid status.", "Use an allowed decision status.")
            if entry.get("kind") == "answered-question" and not entry.get("question"):
                self.error(artifact, "SDD116", f"Answered question `{decision_id}` lacks `question`.", "Record the question.")
            if entry.get("decided_by") not in {"agent", "user", "user-approved"}:
                self.error(artifact, "SDD117", f"Decision `{decision_id}` has invalid `decided_by`.", "Use agent, user, or user-approved as allowed by lifecycle status.")
            if entry.get("decided_by") == "agent" and entry.get("status") != "proposed":
                self.error(artifact, "SDD118", f"Decision `{decision_id}` attributes a non-proposed entry to agent.", "Use agent only for unconfirmed proposals; user acceptance changes provenance to user-approved.")

    def _citations(self, artifact: Artifact) -> None:
        body = no_comments(artifact.body)
        if artifact.kind != "decision-log":
            repository_key = str(self._repo_for_artifact(artifact))
            for number in IDS["D"].findall(body):
                decision_id = f"D-{number}"
                target = self.decisions.get((repository_key, decision_id))
                if target is None:
                    self.error(artifact, "SDD120", f"Citation `{decision_id}` does not resolve.", "Correct it or restore the decision.", artifact.line(decision_id, True))
                elif self._is_live(artifact) and target[1].get("status") in {"rejected", "superseded"}:
                    self.error(artifact, "SDD121", f"Live artifact cites `{decision_id}` with status `{target[1].get('status')}`.", "Cite the accepted replacement or reconcile content.", artifact.line(decision_id, True))
        if artifact.kind == "spec":
            return
        specs = self._related_specs(artifact)
        for family in ("FR", "NFR", "AC"):
            available = set().union(*(self.spec_ids[item.rel][family] for item in specs)) if specs else set()
            for number in IDS[family].findall(body):
                value = f"{family}-{number}"
                if value not in available:
                    self.error(artifact, "SDD122", f"Citation `{value}` does not resolve in a related spec.", "Relate the owning spec or correct the citation.", artifact.line(value, True))

    def _related_specs(self, artifact: Artifact) -> list[Artifact]:
        result: dict[str, Artifact] = {}
        frontier = [artifact]
        seen = {artifact.rel}
        if artifact.kind == "phase" and artifact.meta.get("plan"):
            plan = self.by_path.get(f"Plans/{artifact.meta['plan']}/README.md")
            if plan:
                frontier.append(plan)
                seen.add(plan.rel)
        if artifact.kind == "review" and isinstance(artifact.meta.get("review_of"), str):
            target = self.resolve(artifact.meta["review_of"])
            if target:
                frontier.append(target)
                seen.add(target.rel)
        while frontier:
            current = frontier.pop(0)
            related = current.meta.get("related", [])
            if not isinstance(related, list):
                continue
            for reference in related:
                target = self.resolve(reference) if isinstance(reference, str) else None
                if not target:
                    continue
                if target.kind == "spec":
                    result[target.rel] = target
                elif target.rel not in seen and target.kind in {"plan", "design", "review"}:
                    seen.add(target.rel)
                    frontier.append(target)
        return list(result.values())

    def _graphs(self) -> None:
        for plan in (item for item in self.artifacts if item.kind == "plan"):
            phases = plan.meta.get("phases")
            if not isinstance(phases, list):
                continue
            phase_ids = {str(item.get("id")) for item in phases if isinstance(item, dict)}
            phase_graph: dict[str, list[str]] = {}
            plan_tasks: dict[str, tuple[Artifact, dict[str, Any]]] = {}
            for phase in phases:
                if not isinstance(phase, dict):
                    continue
                phase_id = str(phase.get("id", ""))
                dependencies = self._deps(plan, phase, f"phase `{phase_id}`")
                phase_graph[phase_id] = dependencies
                for dependency in dependencies:
                    if dependency not in phase_ids:
                        self.error(plan, "SDD130", f"Phase `{phase_id}` depends on unknown `{dependency}`.", "Reference a phase in this plan.")
                    if dependency == phase_id:
                        self.error(plan, "SDD131", f"Phase `{phase_id}` depends on itself.", "Remove the self-dependency.")
                doc = phase.get("doc")
                target = self.by_path.get((Path(plan.rel).parent / str(doc)).as_posix()) if doc else None
                if target and isinstance(target.meta.get("tasks"), list):
                    for task in target.meta["tasks"]:
                        if isinstance(task, dict) and isinstance(task.get("id"), str):
                            plan_tasks[task["id"]] = (target, task)
            for cycle in cycles(phase_graph):
                self.error(plan, "SDD132", f"Phase dependency cycle: {' -> '.join(cycle)}.", "Make the graph acyclic.")
            task_graph: dict[str, list[str]] = {}
            for task_id, (phase, task) in plan_tasks.items():
                dependencies = self._deps(phase, task, f"task `{task_id}`")
                task_graph[task_id] = dependencies
                for dependency in dependencies:
                    if dependency not in plan_tasks:
                        self.error(phase, "SDD133", f"Task `{task_id}` depends on unknown `{dependency}`.", "Reference a task in this plan.")
                    if dependency == task_id:
                        self.error(phase, "SDD134", f"Task `{task_id}` depends on itself.", "Remove the self-dependency.")
            for cycle in cycles(task_graph):
                self.error(plan_tasks[cycle[0]][0], "SDD135", f"Task dependency cycle: {' -> '.join(cycle)}.", "Make the graph acyclic.")

    def _traceability(self) -> None:
        for plan in (item for item in self.artifacts if item.kind == "plan" and item.status in {"approved", "active", "complete"}):
            specs = self._related_specs(plan)
            if not specs:
                continue
            related = plan.meta.get("related", [])
            designs = [
                target
                for reference in related if isinstance(related, list) and isinstance(reference, str)
                if (target := self.resolve(reference)) is not None and target.kind == "design"
            ] if isinstance(related, list) else []
            plan_documents = [plan]
            phases = plan.meta.get("phases", [])
            if isinstance(phases, list):
                for phase in phases:
                    if not isinstance(phase, dict) or not isinstance(phase.get("doc"), str):
                        continue
                    target = self.by_path.get((Path(plan.rel).parent / phase["doc"]).as_posix())
                    if target:
                        plan_documents.append(target)
            plan_text_parts: list[str] = []
            for phase in plan_documents[1:]:
                tasks = phase.meta.get("tasks", [])
                if isinstance(tasks, list):
                    plan_text_parts.extend(str(task.get("verification", "")) for task in tasks if isinstance(task, dict))
                sections = self.sections(phase)
                acceptance = sections.get("Acceptance Criteria")
                if acceptance:
                    plan_text_parts.append(acceptance[1])
                for heading, (_, task_body) in sections.items():
                    if re.match(r"^\d+(?:[A-Z])?(?:-[A-Z])?\.\d+(?:\s*:|\s|$)", heading):
                        plan_text_parts.append(strip_completion_evidence(task_body))
            plan_text = "\n".join(plan_text_parts)
            design_text = "\n".join(no_comments(item.body) + "\n" + json.dumps(item.meta, default=str) for item in designs)
            for spec in specs:
                implicated = [spec.rel, *(design.rel for design in designs)]
                for family in ("FR", "NFR"):
                    for identifier in sorted(self.spec_ids[spec.rel][family]):
                        if identifier not in plan_text:
                            self.error(plan, "SDD160", f"Plan hierarchy never cites `{identifier}` from `{spec.rel}`.", "Cite the requirement in task verification/detail or phase acceptance criteria, or explicitly narrow the related specifications.", implicated=implicated)
                        if designs and identifier not in design_text:
                            self.error(plan, "SDD161", f"Related designs never cite `{identifier}` from `{spec.rel}`.", "Cite the requirement in a realizing design or remove an incorrect design relationship.", implicated=implicated)
                for identifier in sorted(self.spec_ids[spec.rel]["AC"]):
                    if identifier not in plan_text:
                        self.error(plan, "SDD162", f"Plan hierarchy never cites `{identifier}` from `{spec.rel}`.", "Cite the acceptance criterion in task verification/detail or phase acceptance criteria.", implicated=implicated)

    def _deps(self, artifact: Artifact, entry: dict[str, Any], label: str) -> list[str]:
        value = entry.get("depends_on", [])
        if value is None:
            return []
        if not isinstance(value, list):
            self.error(artifact, "SDD136", f"`depends_on` for {label} is not a list.", "Use a YAML list or omit it.")
            return []
        if any(not isinstance(item, (str, int)) for item in value):
            self.error(artifact, "SDD137", f"`depends_on` for {label} contains a non-scalar.", "Use only ids.")
        return [str(item) for item in value if isinstance(item, (str, int))]

    def _decision_links(self) -> None:
        for (repository_key, decision_id), (artifact, entry) in self.decisions.items():
            if entry.get("status") == "superseded" and not entry.get("superseded_by"):
                self.error(artifact, "SDD140", f"Superseded `{decision_id}` lacks `superseded_by`.", "Link the accepted replacement.")
            for field, reverse in (("supersedes", "superseded_by"), ("superseded_by", "supersedes")):
                value = entry.get(field)
                if not value:
                    continue
                target = self.decisions.get((repository_key, str(value)))
                if target is None:
                    self.error(artifact, "SDD141", f"Decision `{decision_id}` {field} unknown `{value}`.", "Reference an existing decision.")
                elif target[1].get(reverse) != decision_id:
                    self.error(artifact, "SDD142", f"Decision `{decision_id}` {field} link is not reciprocated.", f"Add matching `{reverse}`.")
            scope = entry.get("scope", [])
            if not isinstance(scope, list):
                self.error(artifact, "SDD143", f"Decision `{decision_id}` scope is not a list.", "Use a YAML list.")
                continue
            for reference in scope:
                if not isinstance(reference, str):
                    self.error(artifact, "SDD144", f"Decision `{decision_id}` has a non-string scope.", "Use repository-relative paths.")
                    continue
                target = self.resolve(reference)
                filesystem = (Path(repository_key) / reference).resolve()
                if target is None and not filesystem.exists():
                    self.error(artifact, "SDD145", f"Decision `{decision_id}` scope `{reference}` does not resolve.", "Point it at an existing artifact or repository path.")
                elif target and entry.get("status") == "accepted" and decision_id not in target.source:
                    self.error(target, "SDD146", f"Artifact is governed by `{decision_id}` but does not cite it.", f"Cite `{decision_id}` or narrow its scope.")
        accepted = [(key[0], key[1], value[0], value[1]) for key, value in self.decisions.items() if value[1].get("status") == "accepted"]
        for index, (left_repo, left_id, artifact, left) in enumerate(accepted):
            for right_repo, right_id, _, right in accepted[index + 1 :]:
                if left_repo != right_repo:
                    continue
                if not self._scopes_overlap(left.get("scope"), right.get("scope")):
                    continue
                if normalized(left.get("question")) and normalized(left.get("question")) == normalized(right.get("question")) and normalized(left.get("statement")) != normalized(right.get("statement")):
                    self.candidate(artifact, "SDD147", f"`{left_id}` and `{right_id}` answer the same question differently.", "Judge whether they conflict, refine one another, or have disjoint scope.")
                if chosen_rejected(left, right) or chosen_rejected(right, left):
                    self.candidate(artifact, "SDD148", f"`{left_id}` and `{right_id}` choose and reject the same option.", "Judge whether they conflict or have disjoint scope.")
                left_term = definition_term(left)
                right_term = definition_term(right)
                if left_term and left_term == right_term and normalized(left.get("statement")) != normalized(right.get("statement")):
                    self.candidate(artifact, "SDD149", f"`{left_id}` and `{right_id}` define `{left_term}` differently.", "Judge whether the definitions conflict or have disjoint scope.")

    def _focused_decision_logs(self) -> None:
        candidates: list[Path] = []
        internal = self.root / "Decisions"
        canonical = internal / "decisions.md"
        if canonical.is_file():
            candidates.append(canonical)
        else:
            candidates.extend(sorted(internal.glob("archive-*.md"))[:1])
        for repository in {self.repo, *self.plan_repos.values()}:
            canonical = repository / "DECISIONS.md"
            if canonical.is_file():
                candidates.append(canonical)
            else:
                candidates.extend(sorted(repository.glob("archive-*.md"))[:1])
        seen: set[Path] = set()
        for path in candidates:
            directory = path.parent.resolve()
            if directory in seen:
                continue
            seen.add(directory)
            diagnostics, _, _ = validate_decision_ledgers(
                path,
                history=self.identity_mode != "historical" and git_root(path) is not None,
            )
            for item in diagnostics:
                path = item.path
                try:
                    path = Path(path).resolve().relative_to(self.root).as_posix()
                except ValueError:
                    pass
                self.out.append(
                    Diagnostic(
                        item.severity,
                        item.code,
                        str(path),
                        item.line,
                        item.message,
                        item.correction,
                    )
                )

    def _is_live(self, artifact: Artifact) -> bool:
        if artifact.kind in {"debrief", "retro"}:
            return False
        return artifact.status not in {"archived", "superseded"}

    def _scopes_overlap(self, left: Any, right: Any) -> bool:
        if not isinstance(left, list) or not left or not isinstance(right, list) or not right:
            return True
        for left_item in left:
            for right_item in right:
                if not isinstance(left_item, str) or not isinstance(right_item, str):
                    continue
                left_path = left_item.rstrip("/")
                right_path = right_item.rstrip("/")
                if left_path == right_path or left_path.startswith(right_path + "/") or right_path.startswith(left_path + "/"):
                    return True
                left_artifact = self.resolve(left_item)
                right_artifact = self.resolve(right_item)
                if left_artifact and right_artifact and (
                    self._artifacts_connected(left_artifact, right_artifact)
                    or self._artifacts_connected(right_artifact, left_artifact)
                ):
                    return True
        return False

    def _artifacts_connected(self, left: Artifact, right: Artifact) -> bool:
        frontier = [(left, 0)]
        seen = {left.rel}
        while frontier:
            current, depth = frontier.pop(0)
            if current.rel == right.rel:
                return True
            if depth >= 2:
                continue
            related = current.meta.get("related", [])
            if not isinstance(related, list):
                continue
            for reference in related:
                target = self.resolve(reference) if isinstance(reference, str) else None
                if target and target.rel not in seen:
                    seen.add(target.rel)
                    frontier.append((target, depth + 1))
        return False


def open_question_items(body: str) -> list[str]:
    value = visible_markdown(body).strip()
    if not value or value.lower().rstrip(".") in {"none", "n/a"}:
        return []
    items: list[str] = []
    current: str | None = None
    for line in value.splitlines():
        match = re.match(r"^\s*-\s*(.*?)\s*$", line)
        if match:
            if current is not None:
                items.append(current)
            current = match.group(1)
            continue
        continuation = line.strip()
        if not continuation:
            continue
        if current is None:
            items.append(continuation)
        else:
            current = f"{current} {continuation}".strip()
    if current is not None:
        items.append(current)
    return [item for item in items if item.lower().rstrip(".") not in {"none", "n/a"}]


def read_utf8(path: Path) -> str | None:
    try:
        return path.read_text(encoding="utf-8")
    except (OSError, UnicodeError):
        return None


def parse_frontmatter_source(source: str) -> dict[str, Any] | None:
    lines = source.splitlines(keepends=True)
    if not lines or lines[0].strip() != "---":
        return None
    end = next((index for index, line in enumerate(lines[1:], 1) if line.strip() == "---"), None)
    if end is None:
        return None
    try:
        value = yaml.safe_load("".join(lines[1:end]))
    except yaml.YAMLError:
        return None
    return value if isinstance(value, dict) else None


def spec_definition_ids(source: str) -> set[str]:
    body = visible_markdown(source)
    return set().union(*(set(pattern.findall(body)) for pattern in DEFINITIONS.values()))


def spec_retained_ids(source: str) -> set[str]:
    result = spec_definition_ids(source)
    identifier = r"((?:FR|NFR|AC)-\d{2,})"
    removed = re.compile(
        rf"^\s*-\s+(?:\[[ xX]\]\s+)?\*\*{identifier}\*\*\s*:\s*removed\s+[—-]\s+see\s+\S.*$",
        re.IGNORECASE,
    )
    struck = re.compile(
        rf"^\s*-\s+(?:\[[ xX]\]\s+)?~~\*\*{identifier}\*\*\s*:\s*\S.*~~\s*$"
    )
    for line in visible_markdown(source).splitlines():
        match = removed.match(line) or struck.match(line)
        if match:
            result.add(match.group(1))
    return result


def frontmatter_entry_ids(source: str, field: str) -> set[str]:
    meta = parse_frontmatter_source(source)
    entries = meta.get(field) if meta else None
    if not isinstance(entries, list):
        return set()
    return {
        str(entry["id"])
        for entry in entries
        if isinstance(entry, dict) and entry.get("id") not in (None, "")
    }


def completion_evidence_body(task_body: str) -> str | None:
    bodies = heading_bodies(task_body, 3, "Completion Evidence")
    return bodies[0] if len(bodies) == 1 else None


def markdown_lines(body: str) -> list[tuple[str, str]]:
    result: list[tuple[str, str]] = []
    fence: tuple[str, int] | None = None
    in_comment = False
    for raw in body.splitlines(keepends=True):
        source = raw
        if fence is not None:
            marker, length = fence
            indent, stripped = markdown_indentation(raw)
            if indent <= 3 and re.match(rf"^{re.escape(marker)}{{{length},}}\s*$", stripped.rstrip("\r\n")):
                fence = None
            result.append((raw, "\n" if raw.endswith("\n") else ""))
            continue
        if in_comment:
            closing = raw.find("-->")
            if closing < 0:
                result.append((source, "\n" if source.endswith("\n") else ""))
                continue
            in_comment = False
            raw = raw[closing + 3 :]
        raw_indent, stripped_raw = markdown_indentation(raw)
        opener = raw_fence_opener(stripped_raw) if raw_indent <= 3 and not in_comment else None
        if opener:
            fence = opener
            result.append((source, "\n" if source.endswith("\n") else ""))
            continue
        if raw_indent >= 4:
            result.append((source, "\n" if source.endswith("\n") else ""))
            continue
        visible_parts: list[str] = []
        remaining = raw
        while remaining:
            if in_comment:
                closing = remaining.find("-->")
                if closing < 0:
                    remaining = ""
                    break
                in_comment = False
                remaining = remaining[closing + 3 :]
                continue
            opening = remaining.find("<!--")
            if opening < 0:
                visible_parts.append(remaining)
                break
            visible_parts.append(remaining[:opening])
            remaining = remaining[opening + 4 :]
            in_comment = True
        visible_text = "".join(visible_parts)
        if raw.endswith("\n") and not visible_text.endswith("\n"):
            visible_text += "\n"
        result.append((source, visible_text))
    return result


def markdown_indentation(raw: str) -> tuple[int, str]:
    """Return CommonMark indentation columns and the text after leading whitespace."""
    columns = 0
    index = 0
    while index < len(raw) and raw[index] in " \t":
        if raw[index] == "\t":
            columns += 4 - columns % 4
        else:
            columns += 1
        index += 1
    return columns, raw[index:]


def raw_fence_opener(raw: str) -> tuple[str, int] | None:
    """Return a valid CommonMark fence opener without parsing inline HTML."""
    match = re.match(r"^(`{3,}|~{3,})([^\r\n]*)$", raw.rstrip("\r\n"))
    if not match:
        return None
    token, info = match.groups()
    if token[0] == "`" and "`" in info:
        return None
    return token[0], len(token)


def visible_markdown(body: str) -> str:
    return "".join(visible for _, visible in markdown_lines(body))


def heading_bodies(body: str, level: int, label: str) -> list[str]:
    lines = markdown_lines(body)
    marker = re.compile(rf"^ {{0,3}}{'#' * level}\s+{re.escape(label)}\s*$")
    starts = [index for index, (_, visible) in enumerate(lines) if marker.match(visible.rstrip("\r\n"))]
    result: list[str] = []
    for start in starts:
        end = len(lines)
        for index in range(start + 1, len(lines)):
            _, visible = lines[index]
            if re.match(rf"^ {{0,3}}#{{1,{level}}}\s+", visible):
                end = index
                break
        result.append(no_comments("".join(visible for _, visible in lines[start + 1 : end])).strip())
    return result


def git_output(repository: Path, *args: str) -> tuple[bytes | None, str | None]:
    result = subprocess.run(
        ["git", "-C", str(repository), *args],
        check=False,
        capture_output=True,
    )
    if result.returncode != 0:
        return None, result.stderr.decode("utf-8", errors="replace").strip()
    return result.stdout, None


def evidence_value(body: str, label: str) -> str | None:
    values = evidence_values(body, label)
    return values[0] if values else None


def evidence_values(body: str, label: str) -> list[str]:
    """Return visible, non-fenced values for an exact evidence label."""
    pattern = re.compile(rf"^\s*-\s+{re.escape(label)}:\s*(.*?)\s*$")
    return [
        match.group(1)
        for _, visible in markdown_lines(body)
        if (match := pattern.match(visible.rstrip("\r\n")))
    ]


def markdown_scalar(value: str | None) -> str | None:
    if value is None:
        return None
    result = value.strip()
    if len(result) >= 2 and result[0] == result[-1] == "`":
        result = result[1:-1].strip()
    return result


def parse_final_aligned_review(value: str | None) -> tuple[str, str] | None:
    """Parse the deliberately narrow phase-review evidence syntax."""
    if not value:
        return None
    match = re.fullmatch(r"(?P<path>[^;\s](?:[^;]*[^;\s])?); frozen: (?P<frozen>[^;\s](?:[^;]*[^;\s])?)", value)
    if not match:
        return None
    path = markdown_scalar(match.group("path"))
    frozen = markdown_scalar(match.group("frozen"))
    return (path, frozen) if path and frozen else None


def parse_git_frozen_identity(value: str) -> tuple[str, ...] | None:
    """Accept only immutable full Git ranges for phase review gates."""
    match = re.fullmatch(r"([0-9a-fA-F]{40})\.\.([0-9a-fA-F]{40})", value)
    return (match.group(1), match.group(2)) if match else None


def git_commit_exists(repository: Path, identity: str) -> bool:
    try:
        result = subprocess.run(
            ["git", "-C", str(repository), "cat-file", "-t", identity],
            check=False,
            capture_output=True,
            text=True,
        )
    except OSError:
        return False
    return result.returncode == 0 and result.stdout.strip() == "commit"


def valid_focused_review_syntax(value: str | None) -> bool:
    """Require a quoted exact review command/tool and the full task-diff claim."""
    if not value:
        return False
    match = re.fullmatch(
        r"`(?P<tool>[^`;\n]+?)`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary",
        value.strip(),
    )
    if not match:
        return False
    tool = match.group("tool").strip()
    return bool(tool) and tool.lower() not in {
        "review",
        "code review",
        "diff",
    }


def phase_review_schema_errors(meta: dict[str, Any]) -> list[str]:
    """Return deterministic phase-gate frontmatter schema violations."""
    expected = {
        "review_plan_drift",
        "review_quality",
        "review_spec_compliance",
        "review_blind_spots",
    }
    errors: list[str] = []
    revision = meta.get("reviewed_planning_revision")
    if not isinstance(revision, str) or not re.fullmatch(r"[0-9a-fA-F]{40}", revision):
        errors.append("reviewed_planning_revision must be a full 40-hex Git commit")
    for field in ("reviewed_phase_intent_sha256", "reviewed_plan_intent_sha256"):
        if field in meta:
            errors.append(f"{field} is a removed custom SHA field")
    if meta.get("review_mode") not in {"independent", "mixed", "single-agent"}:
        errors.append("review_mode must be independent, mixed, or single-agent")
    rows = meta.get("lane_results")
    if not isinstance(rows, list) or len(rows) != len(expected):
        return [*errors, "lane_results must contain exactly four entries"]
    lanes: list[str] = []
    rev = meta.get("rev")
    for row in rows:
        if not isinstance(row, dict):
            errors.append("each lane_results entry must be a mapping")
            continue
        lane = row.get("lane")
        lanes.append(str(lane))
        if row.get("result") != "PASS/Aligned":
            errors.append(f"lane `{lane}` result must be PASS/Aligned")
        if row.get("reviewed_identity") != rev:
            errors.append(f"lane `{lane}` reviewed_identity must exactly equal rev")
        evidence = row.get("evidence")
        if not useful_lane_evidence(evidence):
            errors.append(f"lane `{lane}` evidence must be a specific concrete observation")
    if set(lanes) != expected or len(set(lanes)) != len(expected):
        errors.append("lane_results must name each stable lane exactly once")
    return errors


def useful_lane_evidence(value: Any) -> bool:
    """Reject blank and conclusory lane evidence without requiring copied output."""
    if not isinstance(value, str):
        return False
    words = re.findall(r"[A-Za-z0-9_./:-]+", value)
    normalized = " ".join(word.lower() for word in words)
    if re.fullmatch(
        r"(?:no|none|zero)(?: (?:blocking|material|significant|actionable|critical|major|minor))* "
        r"(?:findings?|issues?|concerns?|problems?|defects?|regressions?)(?: (?:were|was))?"
        r"(?: (?:found|identified|detected|observed))?",
        normalized,
    ):
        return False
    generic = {
        "a", "an", "and", "aligned", "boundary", "boundaries", "case", "cases",
        "code", "edge", "ok", "pass", "passed", "plan", "quality", "requirement",
        "requirements", "review", "scope", "success", "successful", "successfully", "task",
    }
    return len(words) >= 3 and any(word.strip(".,:;!?").lower() not in generic for word in words)


def evidence_rows(body: str) -> list[tuple[str, tuple[str, str, str, str]]]:
    rows: list[tuple[str, tuple[str, str, str, str]]] = []
    active: str | None = None
    for raw_line in visible_markdown(body).splitlines():
        cells = [markdown_scalar(cell) or "" for cell in raw_line.strip().strip("|").split("|")]
        if cells == ["Command", "Working directory", "Result", "Observable evidence"]:
            active = "command"
            continue
        if cells == ["Tool / inspection", "Context", "Result", "Observable evidence"]:
            active = "tool"
            continue
        if not active:
            continue
        if len(cells) == 4 and all(re.fullmatch(r":?-{3,}:?", cell) for cell in cells):
            continue
        if len(cells) != 4 or not raw_line.lstrip().startswith("|"):
            active = None
            continue
        values = tuple(cells)
        if all(values) and not any("<" in value and ">" in value for value in values):
            rows.append((active, values))  # type: ignore[arg-type]
    return rows


def resolution_entry(log: str, finding_id: str) -> str:
    match = re.search(rf"^###\s+{re.escape(finding_id)}\b.*$", log, re.MULTILINE)
    if not match:
        return ""
    following = re.search(r"^###\s+F-\d+\b", log[match.end() :], re.MULTILINE)
    end = match.end() + following.start() if following else len(log)
    return log[match.start() : end]


def no_comments(text: str) -> str:
    return re.sub(r"<!--.*?-->", "", text, flags=re.DOTALL)


def strip_completion_evidence(text: str) -> str:
    return re.sub(
        r"^###\s+Completion Evidence\s*$[\s\S]*?(?=^###\s+|\Z)",
        "",
        no_comments(text),
        flags=re.MULTILINE,
    )


def lifecycle_normalized_artifact(artifact: Artifact) -> bytes:
    """Normalize only lifecycle nodes while retaining all other source bytes."""
    frontmatter, body = frontmatter_and_body(artifact.source)
    root = compose_frontmatter(frontmatter)
    payload_offset = len(frontmatter.splitlines(keepends=True)[0])
    spans = [
        (start + payload_offset, end + payload_offset)
        for start, end in lifecycle_frontmatter_spans(root, artifact.kind)
    ]
    normalized_frontmatter = remove_spans(frontmatter, spans)
    if artifact.kind == "plan":
        body = normalize_evidence_section(body, 2, "Plan Completion Evidence")
    elif artifact.kind == "phase":
        task_ids = lifecycle_sequence_ids(root, "tasks")
        body = normalize_all_task_evidence(body, task_ids)
        body = normalize_evidence_section(body, 2, "Phase Completion Evidence")
        body = normalize_checkboxes(body, 3, "Subtasks")
        body = normalize_checkboxes(body, 2, "Acceptance Criteria")
    return (normalized_frontmatter + body).encode("utf-8")


def frontmatter_and_body(source: str) -> tuple[str, str]:
    """Split source without reconstructing its delimiter or line-ending bytes."""
    lines = source.splitlines(keepends=True)
    if not lines or lines[0].strip() != "---":
        raise ValueError("malformed frontmatter")
    end = next((index for index, line in enumerate(lines[1:], 1) if line.strip() == "---"), None)
    if end is None:
        raise ValueError("malformed frontmatter")
    return "".join(lines[: end + 1]), "".join(lines[end + 1 :])


def compose_frontmatter(frontmatter: str) -> MappingNode:
    """Compose the delimited YAML payload so removal can use source marks."""
    lines = frontmatter.splitlines(keepends=True)
    try:
        root = yaml.compose("".join(lines[1:-1]))
    except yaml.YAMLError as error:
        raise ValueError("malformed frontmatter") from error
    if not isinstance(root, MappingNode):
        raise ValueError("frontmatter must be a YAML mapping")
    return root


def mapping_entries(node: MappingNode, name: str) -> list[tuple[ScalarNode, Node]]:
    return [
        (key, value)
        for key, value in node.value
        if isinstance(key, ScalarNode) and key.value == name
    ]


def lifecycle_entry_span(key: ScalarNode, value: Node, path: str, container: Node) -> tuple[int, int]:
    """Return a removable block-style scalar mapping entry or reject ambiguity."""
    if (
        getattr(container, "flow_style", False)
        or not isinstance(value, ScalarNode)
        or value.style in {"|", ">"}
        or key.start_mark.line != key.end_mark.line
        or key.start_mark.line != value.end_mark.line
    ):
        raise ValueError(f"unsupported flow or multiline lifecycle node at `{path}`")
    return key.start_mark.index, value.end_mark.index


def lifecycle_frontmatter_spans(root: MappingNode, kind: str) -> list[tuple[int, int]]:
    """Locate only top-level and declared child lifecycle status entries."""
    spans: list[tuple[int, int]] = []
    for name in ("updated", "status"):
        for key, value in mapping_entries(root, name):
            spans.append(lifecycle_entry_span(key, value, name, root))

    field = "phases" if kind == "plan" else "tasks" if kind == "phase" else None
    if field is None:
        return spans
    for _, sequence in mapping_entries(root, field):
        if not isinstance(sequence, SequenceNode):
            continue
        for index, entry in enumerate(sequence.value):
            if not isinstance(entry, MappingNode):
                continue
            for key, value in mapping_entries(entry, "status"):
                spans.append(
                    lifecycle_entry_span(key, value, f"{field}[{index}].status", entry)
                )
    return spans


def lifecycle_sequence_ids(root: MappingNode, field: str) -> list[str]:
    """Extract string ids from the original node tree for body ownership lookup."""
    ids: list[str] = []
    for _, sequence in mapping_entries(root, field):
        if not isinstance(sequence, SequenceNode):
            continue
        for entry in sequence.value:
            if not isinstance(entry, MappingNode):
                continue
            for _, value in mapping_entries(entry, "id"):
                if isinstance(value, ScalarNode) and value.tag == "tag:yaml.org,2002:str":
                    ids.append(value.value)
    return ids


def remove_spans(source: str, spans: Sequence[tuple[int, int]]) -> str:
    """Remove exactly composed YAML node spans, leaving every other byte intact."""
    result = source
    for start, end in sorted(spans, reverse=True):
        result = result[:start] + result[end:]
    return result


def normalize_evidence_section(text: str, level: int, heading: str) -> str:
    return normalize_visible_sections(text, level, heading, PENDING)


def normalize_all_task_evidence(text: str, task_ids: Sequence[str]) -> str:
    lines = markdown_lines(text)
    sections = declared_task_completion_evidence_sections(lines, task_ids)
    return normalize_section_ranges(
        lines, [(start, end) for _, start, end in sections], PENDING
    )


def normalize_checkboxes(text: str, level: int, heading: str) -> str:
    lines = markdown_lines(text)
    sections = visible_section_ranges(lines, level, heading)
    covered = {index for start, end in sections for index in range(start + 1, end)}
    return "".join(
        re.sub(r"^(\s*-\s+)\[[xX]\]", r"\1[ ]", raw)
        if index in covered and visible and markdown_checkbox_state(raw) in {"x", "X"}
        else raw
        for index, (raw, visible) in enumerate(lines)
    )


def markdown_checkbox_state(line: str) -> str | None:
    """Return a visible task-list marker state at CommonMark list indentation."""
    columns, stripped = markdown_indentation(line)
    match = re.match(r"^-\s+\[([ xX])\]", stripped) if columns <= 3 else None
    return match.group(1) if match else None


def has_unchecked_checkbox(body: str) -> bool:
    """Return whether visible Markdown contains an unchecked task-list marker."""
    return any(markdown_checkbox_state(visible) == " " for _, visible in markdown_lines(body))


def visible_section_ranges(
    lines: list[tuple[str, str]], level: int, heading: str
) -> list[tuple[int, int]]:
    marker = re.compile(rf"^ {{0,3}}{'#' * level}\s+{re.escape(heading)}\s*$")
    starts = [index for index, (_, visible) in enumerate(lines) if marker.match(visible.rstrip("\r\n"))]
    result: list[tuple[int, int]] = []
    for start in starts:
        end = next(
            (
                index
                for index in range(start + 1, len(lines))
                if re.match(rf"^ {{0,3}}#{{1,{level}}}\s+", lines[index][1])
            ),
            len(lines),
        )
        result.append((start, end))
    return result


def normalize_visible_sections(text: str, level: int, heading: str, replacement: str) -> str:
    lines = markdown_lines(text)
    sections = visible_section_ranges(lines, level, heading)
    return normalize_section_ranges(lines, sections, replacement)


def normalize_section_ranges(
    lines: list[tuple[str, str]], sections: Iterable[tuple[int, int]], replacement: str
) -> str:
    """Replace only the bodies of selected visible heading sections."""
    sections = list(sections)
    starts = {start for start, _ in sections}
    skipped = {index for start, end in sections for index in range(start + 1, end)}
    result: list[str] = []
    for index, (raw, _) in enumerate(lines):
        if index in starts:
            result.extend((raw, "\n" if raw.endswith("\n") else "", replacement, "\n"))
        elif index not in skipped:
            result.append(raw)
    return "".join(result)


def declared_task_completion_evidence_sections(
    lines: list[tuple[str, str]], task_ids: Sequence[str]
) -> list[tuple[str, int, int]]:
    """Return only Completion Evidence headings within declared H2 task sections."""
    marker = re.compile(r"^ {0,3}###\s+Completion Evidence\s*$")
    result: list[tuple[str, int, int]] = []
    for task_id, start, end in declared_task_section_ranges(lines, task_ids):
        for index in range(start + 1, end):
            if not marker.match(lines[index][1].rstrip("\r\n")):
                continue
            evidence_end = next(
                (
                    following
                    for following in range(index + 1, end)
                    if re.match(r"^ {0,3}#{1,3}\s+", lines[following][1])
                ),
                end,
            )
            result.append((task_id, index, evidence_end))
    return result


def declared_task_section_ranges(
    lines: list[tuple[str, str]], task_ids: Sequence[str]
) -> list[tuple[str, int, int]]:
    """Locate H2 sections whose leading id is exactly a declared task id."""
    headings: list[tuple[int, str]] = []
    for index, (_, visible) in enumerate(lines):
        match = re.match(r"^ {0,3}##\s+(.+?)\s*$", visible.rstrip("\r\n"))
        if match:
            headings.append((index, match.group(1)))
    result: list[tuple[str, int, int]] = []
    for position, (start, heading) in enumerate(headings):
        task_id = next(
            (
                candidate
                for candidate in task_ids
                if re.match(rf"^{re.escape(candidate)}(?:\s*:|\s|$)", heading)
            ),
            None,
        )
        if task_id is None:
            continue
        end = headings[position + 1][0] if position + 1 < len(headings) else len(lines)
        result.append((task_id, start, end))
    return result


def duplicates(values: Iterable[str]) -> set[str]:
    seen: set[str] = set()
    repeated: set[str] = set()
    for value in values:
        if value in seen:
            repeated.add(value)
        seen.add(value)
    return repeated


def cycles(graph: dict[str, list[str]]) -> list[list[str]]:
    state: dict[str, int] = {}
    stack: list[str] = []
    found: set[tuple[str, ...]] = set()

    def visit(node: str) -> None:
        state[node] = 1
        stack.append(node)
        for neighbor in graph.get(node, []):
            if neighbor not in graph:
                continue
            if state.get(neighbor, 0) == 0:
                visit(neighbor)
            elif state.get(neighbor) == 1:
                start = stack.index(neighbor)
                body = stack[start:]
                rotations = [tuple(body[i:] + body[:i] + [body[i]]) for i in range(len(body))]
                found.add(min(rotations))
        stack.pop()
        state[node] = 2

    for node in graph:
        if state.get(node, 0) == 0:
            visit(node)
    return [list(value) for value in sorted(found)]


def normalized(value: Any) -> str:
    return " ".join(str(value or "").lower().split())


def chosen_rejected(chosen: dict[str, Any], rejecting: dict[str, Any]) -> bool:
    statement = normalized(chosen.get("statement"))
    rejected = rejecting.get("rejected", [])
    return isinstance(rejected, list) and any(normalized(item) and normalized(item) in statement for item in rejected if isinstance(item, str))


def definition_term(entry: dict[str, Any]) -> str | None:
    if entry.get("kind") != "definition":
        return None
    question = normalized(entry.get("question"))
    if question:
        match = re.search(r"(?:what (?:is|does)|define)\s+(.+?)(?:\?|$)", question)
        if match:
            return match.group(1).strip(" `\"'")
    statement = normalized(entry.get("statement"))
    match = re.match(r"(.+?)\s+(?:means|is defined as|refers to)\s+", statement)
    return match.group(1).strip(" `\"'") if match else None


def git_root(start: Path) -> Path | None:
    current = start.resolve()
    while True:
        if (current / ".git").exists():
            return current
        if current.parent == current:
            return None
        current = current.parent


def git_worktree_root(start: Path) -> Path | None:
    try:
        result = subprocess.run(
            ["git", "-C", str(start), "rev-parse", "--show-toplevel"],
            check=False,
            capture_output=True,
            text=True,
        )
    except OSError:
        return None
    if result.returncode != 0:
        return None
    value = result.stdout.strip()
    return Path(value).resolve() if value else None


def detected_scm(root: Path) -> str:
    """Detect only lifecycle transports for which the validator has adapters."""
    if git_worktree_root(root) is not None:
        return "git"
    try:
        info = subprocess.run(
            ["p4", "-d", str(root), "info"], check=False, capture_output=True
        )
        mapped = subprocess.run(
            ["p4", "-d", str(root), "where", "//..."], check=False, capture_output=True
        )
    except OSError:
        return "none"
    return "perforce" if info.returncode == 0 and bool(mapped.stdout.strip()) else "none"


def resolve_roots(start: Path, explicit: str | None) -> tuple[Path, Path]:
    start = start.resolve()
    vcs_root = git_root(start)
    repo = vcs_root or start
    if explicit:
        root = Path(explicit)
        resolved = (root if root.is_absolute() else start / root).resolve()
        return resolved, repo
    current = start
    while True:
        config = current / "planning-config.json"
        if config.is_file():
            try:
                data = json.loads(config.read_text(encoding="utf-8"))
            except (OSError, UnicodeError, json.JSONDecodeError) as exc:
                raise ValueError(f"cannot parse {config}: {exc}") from exc
            value = data.get("planningRoot", ".")
            if not isinstance(value, str):
                raise ValueError(f"{config}: planningRoot must be a string")
            root = Path(value)
            return (root if root.is_absolute() else current / root).resolve(), vcs_root or current
        if (vcs_root and current == vcs_root) or current.parent == current:
            return repo, repo
        current = current.parent


def path_matches_root(path: str, root: str) -> bool:
    return path == root or path.startswith(root.rstrip("/") + "/")


def scope_root_for_artifact(artifact: Artifact) -> str:
    parts = Path(artifact.rel).parts
    if len(parts) >= 2 and parts[0] == "Plans":
        return f"Plans/{parts[1]}"
    return artifact.rel


def resolve_scope(
    validator: Validator,
    scope: str | None,
    diagnostics: Sequence[Diagnostic] = (),
) -> ScopeSelection:
    if not scope:
        all_paths = frozenset(artifact.rel for artifact in validator.artifacts)
        return ScopeSelection(all_paths, ())
    raw_value = scope.strip()
    raw_path = Path(raw_value)
    if raw_path.is_absolute() or "\\" in raw_value:
        return ScopeSelection(
            frozenset(),
            (),
            f"scope `{scope}` is unsafe; use a planning-root-relative artifact path without `.`, `..`, or backslashes",
        )
    value = raw_value.strip("/")
    if not value:
        return ScopeSelection(frozenset(), (), "scope is empty")
    path = Path(value)
    if any(part in {"", ".", ".."} for part in path.parts):
        return ScopeSelection(
            frozenset(),
            (),
            f"scope `{scope}` is unsafe; use a planning-root-relative artifact path without `.`, `..`, or backslashes",
        )

    diagnostic_paths = {
        item.path
        for item in diagnostics
        if not item.path.startswith("@repo:") and not Path(item.path).is_absolute()
    }

    def has_diagnostic(root: str) -> bool:
        return any(path_matches_root(candidate, root) for candidate in diagnostic_paths)

    def recognized_artifact_path(candidate: str) -> bool:
        parts = Path(candidate).parts
        return bool(parts) and parts[0] in ARTIFACT_DIRS and candidate.endswith(".md")

    def root_for_unparsed(candidate: str) -> str:
        parts = Path(candidate).parts
        if len(parts) >= 2 and parts[0] == "Plans":
            return f"Plans/{parts[1]}"
        return candidate

    roots: list[str] = []
    physical_value = validator.root / value
    if physical_value.is_dir() and (
        any(path_matches_root(artifact.rel, value) for artifact in validator.artifacts)
        or has_diagnostic(value)
    ):
        roots = [value]
    direct = validator.resolve(value)
    if not roots and direct:
        roots = [scope_root_for_artifact(direct)]
    if not roots:
        explicit_roots = {
            artifact.rel
            for artifact in validator.artifacts
            if path_matches_root(artifact.rel, value)
        }
        if explicit_roots:
            roots = [value]
        else:
            physical_candidates = (value, f"{value}/README.md", f"{value}.md")
            physical = next(
                (
                    candidate
                    for candidate in physical_candidates
                    if recognized_artifact_path(candidate)
                    and (validator.root / candidate).is_file()
                    and has_diagnostic(candidate)
                ),
                None,
            )
            if physical:
                roots = [root_for_unparsed(physical)]
            else:
                historical = next(
                    (
                        candidate
                        for candidate in physical_candidates
                        if recognized_artifact_path(candidate)
                        and candidate in diagnostic_paths
                    ),
                    None,
                )
                if historical:
                    roots = [root_for_unparsed(historical)]
            if not roots and "/" not in value:
                aliases: list[str] = []
                for dirname in ARTIFACT_DIRS:
                    candidate = f"{dirname}/{value}"
                    resolved = validator.resolve(candidate)
                    if resolved:
                        aliases.append(scope_root_for_artifact(resolved))
                    elif any(
                        path_matches_root(artifact.rel, candidate)
                        for artifact in validator.artifacts
                    ):
                        aliases.append(candidate)
                    else:
                        alias_candidates = (candidate, f"{candidate}/README.md", f"{candidate}.md")
                        diagnostic_candidate = next(
                            (
                                path
                                for path in alias_candidates
                                if recognized_artifact_path(path)
                                and (
                                    path in diagnostic_paths
                                    or ((validator.root / path).is_file() and has_diagnostic(path))
                                )
                            ),
                            None,
                        )
                        if diagnostic_candidate:
                            aliases.append(root_for_unparsed(diagnostic_candidate))
                aliases = sorted(set(aliases))
                if len(aliases) > 1:
                    return ScopeSelection(
                        frozenset(),
                        tuple(aliases),
                        f"scope `{scope}` is ambiguous; use one of: {', '.join(aliases)}",
                    )
                if aliases:
                    roots = aliases
    if not roots:
        return ScopeSelection(frozenset(), (), f"scope `{scope}` does not resolve to an artifact")

    selected = {
        artifact.rel
        for artifact in validator.artifacts
        if any(path_matches_root(artifact.rel, root) for root in roots)
    }
    pending = list(selected)
    while pending:
        artifact = validator.by_path.get(pending.pop())
        if artifact is None:
            continue
        related = artifact.meta.get("related", [])
        if not isinstance(related, list):
            continue
        for reference in related:
            target = validator.resolve(reference) if isinstance(reference, str) else None
            if target and target.rel not in selected:
                target_root = scope_root_for_artifact(target)
                additions = {
                    candidate.rel
                    for candidate in validator.artifacts
                    if path_matches_root(candidate.rel, target_root)
                }
                for addition in additions - selected:
                    selected.add(addition)
                    pending.append(addition)
    return ScopeSelection(frozenset(selected), tuple(roots))


def select(
    diagnostics: list[Diagnostic],
    scope: str | None,
    artifacts_in_scope: set[str] | None = None,
    scope_roots: tuple[str, ...] | None = None,
    decision_paths: set[str] | None = None,
) -> list[Diagnostic]:
    if not scope:
        return diagnostics
    roots = scope_roots or (scope.strip().strip("/"),)

    def path_selected(path: str) -> bool:
        return (
            (artifacts_in_scope is not None and path in artifacts_in_scope)
            or any(path_matches_root(path, root) for root in roots)
        )

    return [
        item
        for item in diagnostics
        if item.severity == "operational"
        or item.code == "SDD000"
        or item.code.startswith("DLG")
        or item.path.startswith("@repo:")
        or item.path.startswith("Decisions/")
        or (decision_paths is not None and item.path in decision_paths)
        or path_selected(item.path)
        or any(path_selected(path) for path in item.implicated)
    ]


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--root", help="Planning root; defaults through planning-config.json")
    result.add_argument("--scope", help="Limit findings to an artifact/path and its transitive explicit related-artifact graph")
    result.add_argument("--format", choices=("text", "json"), default="text", dest="output")
    result.add_argument("--identity-mode", choices=("auto", "current", "historical"), default="auto", help="Use current-worktree checks (auto/current) or validate historical durable objects only")
    return result


def main(argv: Sequence[str] | None = None) -> int:
    args = parser().parse_args(argv)
    try:
        root, repo = resolve_roots(Path.cwd(), args.root)
    except ValueError as exc:
        print(f"sdd-validate: {exc}", file=sys.stderr)
        return 2
    validator = Validator(root, repo, args.identity_mode)
    all_diagnostics = validator.run()
    scope_selection = resolve_scope(validator, args.scope, all_diagnostics)
    operational_error = None
    if not validator.artifacts:
        operational_error = "planning root contains no discoverable SDD artifacts"
    elif scope_selection.error:
        operational_error = scope_selection.error
    if operational_error:
        if args.output == "json":
            print(json.dumps({"valid": False, "planning_root": str(root), "artifacts_inspected": len(validator.artifacts), "error": operational_error, "diagnostics": []}, indent=2, sort_keys=True))
        else:
            print(f"sdd-validate: {operational_error}", file=sys.stderr)
        return 2
    artifacts_in_scope = set(scope_selection.artifacts)
    diagnostics = select(
        all_diagnostics,
        args.scope,
        artifacts_in_scope,
        scope_selection.roots,
        {artifact.rel for artifact in validator.artifacts if artifact.kind == "decision-log"},
    )
    operational = [item for item in diagnostics if item.severity == "operational"]
    errors = [item for item in diagnostics if item.severity == "error"]
    if args.output == "json":
        print(json.dumps({"valid": not errors and not operational, "planning_root": str(root), "artifacts_inspected": len(validator.artifacts), "artifacts_in_scope": sorted(artifacts_in_scope), "diagnostics": [asdict(item) for item in diagnostics]}, indent=2, sort_keys=True))
    else:
        scope_summary = f", {len(artifacts_in_scope)} in scope" if args.scope else ""
        print(f"{'Valid' if not errors and not operational else 'Invalid'}: {root} ({len(validator.artifacts)} artifacts inspected{scope_summary})")
        for item in diagnostics:
            print(f"{item.severity.upper()} {item.code} {item.path}:{item.line}: {item.message}")
            print(f"  Required correction: {item.correction}")
        if not diagnostics:
            print("Checked structure, frontmatter, paths, identifiers, hierarchy, dependencies, reviews, decisions, and completion-evidence shape.")
    if operational:
        return 2
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
