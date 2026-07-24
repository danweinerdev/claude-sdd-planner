#!/usr/bin/env python3
"""Deterministically validate an SDD decision ledger and its archive siblings."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import re
import subprocess
import sys
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any, Sequence

try:
    import yaml
    from yaml.constructor import ConstructorError
except ImportError:
    print(
        "sdd-decision-validate: PyYAML is required; install it with "
        "`python3 -m pip install -r <plugin-root>/requirements.txt`",
        file=sys.stderr,
    )
    raise SystemExit(2)


DECISION_ID = re.compile(r"D-\d{4,9}")
ARCHIVE_NAME = re.compile(r"archive-\d{4}\.md")
KINDS = {"decision", "definition", "answered-question", "assumption"}
STATUSES = {"proposed", "accepted", "rejected", "superseded"}
DECIDERS = {"agent", "user", "user-approved"}
REVERSIBILITY = {"one-way", "two-way"}


@dataclass(frozen=True)
class Diagnostic:
    severity: str
    code: str
    path: str
    line: int
    message: str
    correction: str


@dataclass
class Ledger:
    path: Path
    source: str
    meta: dict[str, Any]
    body: str
    body_line: int

    def line(self, text: str) -> int:
        for number, value in enumerate(self.source.splitlines(), 1):
            if text in value:
                return number
        return 1


class UniqueKeyLoader(yaml.SafeLoader):
    pass


def construct_unique_mapping(loader: UniqueKeyLoader, node: yaml.MappingNode, deep: bool = False) -> dict[Any, Any]:
    mapping: dict[Any, Any] = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        try:
            hash(key)
        except TypeError as exc:
            raise ConstructorError("while constructing a mapping", node.start_mark, "unhashable mapping key", key_node.start_mark) from exc
        if key in mapping:
            raise ConstructorError("while constructing a mapping", node.start_mark, f"duplicate key: {key}", key_node.start_mark)
        mapping[key] = loader.construct_object(value_node, deep=deep)
    return mapping


UniqueKeyLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, construct_unique_mapping)


def diagnostic(ledger: Ledger | None, code: str, message: str, correction: str, line: int = 1, path: Path | None = None, severity: str = "error") -> Diagnostic:
    return Diagnostic(severity, code, str(path or (ledger.path if ledger else "<ledger>")), line, message, correction)


def parse_ledger(path: Path) -> tuple[Ledger | None, list[Diagnostic]]:
    out: list[Diagnostic] = []
    try:
        source = path.read_bytes().decode("utf-8")
    except (OSError, UnicodeError) as exc:
        return None, [diagnostic(None, "DLG001", f"Cannot read ledger as UTF-8: {exc}", "Store the ledger as readable UTF-8.", path=path, severity="operational")]
    if "\r\n" in source:
        out.append(diagnostic(None, "DLG002", "Ledger uses CRLF line endings.", "Normalize it to UTF-8 with LF endings.", path=path))
    lines = source.splitlines(keepends=True)
    if not lines or lines[0].strip() != "---":
        out.append(diagnostic(None, "DLG003", "Missing opening YAML frontmatter delimiter.", "Start the ledger with standalone `---`.", path=path))
        return None, out
    end = next((index for index, line in enumerate(lines[1:], 1) if line.strip() == "---"), None)
    if end is None:
        out.append(diagnostic(None, "DLG004", "Missing closing YAML frontmatter delimiter.", "Close frontmatter with standalone `---`.", path=path))
        return None, out
    try:
        meta = yaml.load("".join(lines[1:end]), Loader=UniqueKeyLoader)
    except yaml.YAMLError as exc:
        mark = getattr(exc, "problem_mark", None)
        out.append(diagnostic(None, "DLG005", f"Invalid YAML frontmatter: {exc}", "Correct the YAML syntax.", (mark.line + 2) if mark else 1, path))
        return None, out
    if not isinstance(meta, dict):
        out.append(diagnostic(None, "DLG006", "Frontmatter is not a mapping.", "Use key/value YAML frontmatter.", path=path))
        return None, out
    return Ledger(path, source, meta, "".join(lines[end + 1 :]), end + 2), out


def iso_date(value: Any) -> dt.date | None:
    if isinstance(value, dt.datetime):
        return None
    if isinstance(value, dt.date):
        return value
    if not isinstance(value, str) or not re.fullmatch(r"\d{4}-\d{2}-\d{2}", value):
        return None
    try:
        return dt.date.fromisoformat(value)
    except ValueError:
        return None


def nonempty_string(value: Any) -> bool:
    return isinstance(value, str) and bool(value.strip())


def validate_string_list(ledger: Ledger, entry_id: str, field: str, value: Any, out: list[Diagnostic]) -> None:
    if not isinstance(value, list):
        out.append(diagnostic(ledger, "DLG027", f"Decision `{entry_id}` field `{field}` is not a list.", f"Use `{field}: []` or a list of nonempty strings.", ledger.line(f"{field}:")))
        return
    if any(not nonempty_string(item) for item in value):
        out.append(diagnostic(ledger, "DLG028", f"Decision `{entry_id}` field `{field}` contains a non-string or empty value.", "Use only nonempty strings.", ledger.line(f"{field}:")))


def safe_scope(value: str) -> bool:
    if not value.strip() or value != value.strip() or value.startswith("/") or value.startswith("//"):
        return False
    if "\\" in value or re.match(r"^[A-Za-z]:", value):
        return False
    return all(part not in {"", ".", ".."} for part in value.split("/"))


def validate_ledger(ledger: Ledger) -> tuple[list[Diagnostic], list[dict[str, Any]]]:
    out: list[Diagnostic] = []
    meta = ledger.meta
    if ledger.path.is_symlink():
        out.append(diagnostic(ledger, "DLG019", "Ledger path is a symbolic link.", "Store the canonical ledger and archives as regular files in their owning repository."))
    for field in ("title", "type", "status", "created", "updated", "tags", "related", "decisions"):
        if field not in meta or meta[field] in (None, ""):
            out.append(diagnostic(ledger, "DLG010", f"Required ledger field `{field}` is missing or empty.", f"Add a valid `{field}` value.", ledger.line(f"{field}:")))
    if not nonempty_string(meta.get("title")):
        out.append(diagnostic(ledger, "DLG011", "Ledger `title` must be a nonempty string.", "Set a descriptive string title.", ledger.line("title:")))
    if meta.get("type") != "decision-log":
        out.append(diagnostic(ledger, "DLG012", "Ledger `type` is not `decision-log`.", "Set `type: decision-log`.", ledger.line("type:")))
    if meta.get("status") not in {"active", "archived"}:
        out.append(diagnostic(ledger, "DLG013", "Ledger status must be `active` or `archived`.", "Use the lifecycle status matching the file role.", ledger.line("status:")))
    created = iso_date(meta.get("created"))
    updated = iso_date(meta.get("updated"))
    for field, value in (("created", created), ("updated", updated)):
        if value is None:
            out.append(diagnostic(ledger, "DLG014", f"Ledger `{field}` must be a real YYYY-MM-DD date.", f"Set `{field}` to an ISO calendar date.", ledger.line(f"{field}:")))
    if created and updated and created > updated:
        out.append(diagnostic(ledger, "DLG015", "Ledger `created` is later than `updated`.", "Correct the ledger dates.", ledger.line("updated:")))
    for field in ("tags", "related"):
        validate_string_list(ledger, "<ledger>", field, meta.get(field), out)

    name = ledger.path.name
    archived_name = bool(ARCHIVE_NAME.fullmatch(name))
    canonical_name = name == "DECISIONS.md" or (name == "decisions.md" and ledger.path.parent.name == "Decisions")
    if not archived_name and not canonical_name:
        out.append(diagnostic(ledger, "DLG016", f"Noncanonical ledger filename `{name}`.", "Use `Decisions/decisions.md`, repository-root `DECISIONS.md`, or `archive-YYYY.md`."))
    if archived_name and meta.get("status") != "archived":
        out.append(diagnostic(ledger, "DLG017", "Archive filename does not have `status: archived`.", "Set the archive status to `archived`.", ledger.line("status:")))
    if canonical_name and meta.get("status") != "active":
        out.append(diagnostic(ledger, "DLG018", "Canonical ledger does not have `status: active`.", "Keep the canonical ledger active.", ledger.line("status:")))
    repository = git_root(ledger.path)
    if name == "DECISIONS.md" and repository is not None and ledger.path.parent.resolve() != repository:
        out.append(diagnostic(ledger, "DLG042", "External `DECISIONS.md` is not at the repository root.", "Move it to the root of the repository it represents."))

    entries = meta.get("decisions")
    if not isinstance(entries, list):
        out.append(diagnostic(ledger, "DLG020", "`decisions` must be a YAML list.", "Use `decisions: []` when empty.", ledger.line("decisions:")))
        return out, []
    valid_entries: list[dict[str, Any]] = []
    latest_date: dt.date | None = None
    for index, entry in enumerate(entries):
        if not isinstance(entry, dict):
            out.append(diagnostic(ledger, "DLG021", f"Decision at index {index} is not a mapping.", "Use the documented decision entry mapping.", ledger.line("decisions:")))
            continue
        entry_id = entry.get("id") if isinstance(entry.get("id"), str) else f"<index {index}>"
        for field in ("id", "kind", "status", "date", "decided_by", "statement", "rationale"):
            if field not in entry or entry[field] in (None, ""):
                out.append(diagnostic(ledger, "DLG022", f"Decision `{entry_id}` is missing `{field}`.", f"Add a nonempty `{field}`.", ledger.line(f"id: {entry_id}")))
        if not isinstance(entry.get("id"), str) or not DECISION_ID.fullmatch(entry.get("id", "")):
            out.append(diagnostic(ledger, "DLG023", f"Decision `{entry_id}` has an invalid id.", "Use `D-NNNN` with at least four digits.", ledger.line(f"id: {entry_id}")))
        if entry.get("kind") not in KINDS:
            out.append(diagnostic(ledger, "DLG024", f"Decision `{entry_id}` has an invalid kind.", f"Use one of: {', '.join(sorted(KINDS))}.", ledger.line("kind:")))
        if entry.get("status") not in STATUSES:
            out.append(diagnostic(ledger, "DLG025", f"Decision `{entry_id}` has an invalid status.", f"Use one of: {', '.join(sorted(STATUSES))}.", ledger.line("status:")))
        if entry.get("decided_by") not in DECIDERS:
            out.append(diagnostic(ledger, "DLG026", f"Decision `{entry_id}` has an invalid `decided_by`.", "Use `agent`, `user`, or `user-approved` as allowed by lifecycle status.", ledger.line("decided_by:")))
        if entry.get("decided_by") == "agent" and entry.get("status") != "proposed":
            out.append(diagnostic(ledger, "DLG041", f"Decision `{entry_id}` attributes a non-proposed entry to `agent`.", "Only an unconfirmed proposal may use `decided_by: agent`; user acceptance changes provenance to `user-approved`.", ledger.line("decided_by:")))
        if entry.get("decided_by") == "user-approved" and entry.get("status") not in {"accepted", "superseded"}:
            out.append(diagnostic(ledger, "DLG044", f"Decision `{entry_id}` uses `user-approved` with status `{entry.get('status')}`.", "Use `user-approved` only for an accepted agent proposal and its later superseded state.", ledger.line("decided_by:")))
        for field in ("statement", "rationale"):
            if not nonempty_string(entry.get(field)):
                out.append(diagnostic(ledger, "DLG029", f"Decision `{entry_id}` field `{field}` must be a nonempty string.", f"Record a nonempty `{field}` string.", ledger.line(f"{field}:")))
        entry_date = iso_date(entry.get("date"))
        if entry_date is None:
            out.append(diagnostic(ledger, "DLG030", f"Decision `{entry_id}` date must be a real YYYY-MM-DD date.", "Set an ISO calendar date.", ledger.line("date:")))
        elif latest_date is None or entry_date > latest_date:
            latest_date = entry_date
        if entry.get("kind") == "answered-question" and not nonempty_string(entry.get("question")):
            out.append(diagnostic(ledger, "DLG031", f"Answered question `{entry_id}` lacks a nonempty `question`.", "Record the question that was answered.", ledger.line(f"id: {entry_id}")))
        for field in ("question", "confirmation"):
            if field in entry and not nonempty_string(entry[field]):
                out.append(diagnostic(ledger, "DLG032", f"Decision `{entry_id}` field `{field}` must be a nonempty string when present.", f"Remove it or record a nonempty `{field}`.", ledger.line(f"{field}:")))
        for field in ("rejected", "scope", "tags"):
            if field in entry:
                validate_string_list(ledger, entry_id, field, entry[field], out)
        if isinstance(entry.get("scope"), list):
            for value in entry["scope"]:
                if isinstance(value, str) and not safe_scope(value):
                    out.append(diagnostic(ledger, "DLG033", f"Decision `{entry_id}` has unsafe scope `{value}`.", "Use a repository-relative path without backslashes, `.` or `..` segments.", ledger.line("scope:")))
        if "refresh_when" in entry:
            validate_string_list(ledger, entry_id, "refresh_when", entry["refresh_when"], out)
            if entry.get("kind") != "assumption":
                out.append(diagnostic(ledger, "DLG034", f"Non-assumption `{entry_id}` declares `refresh_when`.", "Use refresh triggers only on assumption entries.", ledger.line("refresh_when:")))
        if "reversibility" in entry and entry["reversibility"] not in REVERSIBILITY:
            out.append(diagnostic(ledger, "DLG035", f"Decision `{entry_id}` has invalid reversibility.", "Use `one-way` or `two-way`.", ledger.line("reversibility:")))
        for field in ("supersedes", "superseded_by"):
            if field in entry and (not isinstance(entry[field], str) or not DECISION_ID.fullmatch(entry[field])):
                out.append(diagnostic(ledger, "DLG036", f"Decision `{entry_id}` field `{field}` is not a decision id.", "Use an existing `D-NNNN` id.", ledger.line(f"{field}:")))
        if ledger.meta.get("status") == "archived" and entry.get("status") in {"accepted", "proposed"}:
            out.append(diagnostic(ledger, "DLG037", f"Archive contains live decision `{entry_id}` with status `{entry.get('status')}`.", "Keep accepted and proposed entries in the canonical active ledger.", ledger.line(f"id: {entry_id}")))
        valid_entries.append(entry)
    if updated and latest_date and updated < latest_date:
        out.append(diagnostic(ledger, "DLG038", "Ledger `updated` predates its newest decision.", "Advance `updated` to at least the newest decision date.", ledger.line("updated:")))

    body_ids = re.findall(r"^ {0,3}##\s+(D-\d{4,9})\b", visible_body(ledger.body), re.MULTILINE)
    known_ids = {entry.get("id") for entry in valid_entries if isinstance(entry.get("id"), str)}
    for entry_id in sorted({item for item in body_ids if body_ids.count(item) > 1}):
        out.append(diagnostic(ledger, "DLG039", f"Body section `{entry_id}` is duplicated.", "Keep at most one optional body section per decision.", ledger.body_line))
    for entry_id in sorted(set(body_ids) - known_ids):
        out.append(diagnostic(ledger, "DLG040", f"Body section `{entry_id}` has no frontmatter entry.", "Add the canonical frontmatter entry or remove the stale body section.", ledger.body_line))
    return out, valid_entries


def normalized(value: Any) -> str:
    return " ".join(str(value or "").lower().split())


def visible_body(value: str) -> str:
    without_comments = re.sub(r"<!--.*?-->", "", value, flags=re.DOTALL)
    lines: list[str] = []
    fence: tuple[str, int] | None = None
    for line in without_comments.splitlines():
        marker = re.match(r"^ {0,3}(`{3,}|~{3,})(.*)$", line)
        if marker and fence is None:
            token = marker.group(1)
            fence = (token[0], len(token))
            continue
        if fence is not None:
            closing = re.match(rf"^ {{0,3}}{re.escape(fence[0])}{{{fence[1]},}}\s*$", line)
            if closing:
                fence = None
            continue
        if fence is None:
            lines.append(line)
    return "\n".join(lines)


def scopes_overlap(left: Any, right: Any) -> bool:
    if not isinstance(left, list) or not left or not isinstance(right, list) or not right:
        return True
    for left_item in left:
        for right_item in right:
            if not isinstance(left_item, str) or not isinstance(right_item, str):
                return True
            left_path = left_item.rstrip("/")
            right_path = right_item.rstrip("/")
            if left_path == right_path or left_path.startswith(right_path + "/") or right_path.startswith(left_path + "/"):
                return True
    return False


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
    match = re.match(r"(.+?)\s+(?:means|is defined as|refers to)\s+", normalized(entry.get("statement")))
    return match.group(1).strip(" `\"'") if match else None


def cycle_paths(graph: dict[str, str]) -> list[list[str]]:
    found: set[tuple[str, ...]] = set()
    for start in graph:
        order: list[str] = []
        positions: dict[str, int] = {}
        current = start
        while current in graph and current not in positions:
            positions[current] = len(order)
            order.append(current)
            current = graph[current]
        if current in positions:
            body = order[positions[current] :]
            rotations = [tuple(body[index:] + body[:index]) for index in range(len(body))]
            found.add(min(rotations))
    return [list(item) for item in sorted(found)]


def validate_collection(ledgers: list[Ledger], entries_by_ledger: dict[Path, list[dict[str, Any]]]) -> list[Diagnostic]:
    out: list[Diagnostic] = []
    indexed: dict[str, tuple[Ledger, dict[str, Any]]] = {}
    for ledger in ledgers:
        for entry in entries_by_ledger.get(ledger.path, []):
            entry_id = entry.get("id")
            if not isinstance(entry_id, str) or not DECISION_ID.fullmatch(entry_id):
                continue
            if entry_id in indexed:
                out.append(diagnostic(ledger, "DLG050", f"Duplicate decision id `{entry_id}` across ledger files.", "Renumber the later entry and update all links.", ledger.line(f"id: {entry_id}")))
            else:
                indexed[entry_id] = (ledger, entry)

    numeric_ids = sorted(int(entry_id.split("-", 1)[1]) for entry_id in indexed)
    gap = next(((left, right) for left, right in zip([0, *numeric_ids], numeric_ids) if right != left + 1), None)
    if gap is not None:
        ledger, _ = indexed[min(indexed, key=lambda value: int(value.split("-", 1)[1]))]
        out.append(diagnostic(ledger, "DLG064", f"Decision id sequence jumps from `D-{gap[0]:04d}` to `D-{gap[1]:04d}`.", "Restore retained entries or renumber an uncommitted later entry to the next sequential id."))
    for ledger in ledgers:
        ordered = [int(entry["id"].split("-", 1)[1]) for entry in entries_by_ledger.get(ledger.path, []) if isinstance(entry.get("id"), str) and DECISION_ID.fullmatch(entry["id"])]
        if ordered != sorted(ordered):
            out.append(diagnostic(ledger, "DLG065", "Decision entries are not in ascending id order.", "Keep append-only entries ordered by their sequential ids.", ledger.line("decisions:")))

    graph: dict[str, str] = {}
    for entry_id, (ledger, entry) in indexed.items():
        status = entry.get("status")
        supersedes = entry.get("supersedes")
        superseded_by = entry.get("superseded_by")
        if status == "superseded" and not superseded_by:
            out.append(diagnostic(ledger, "DLG051", f"Superseded `{entry_id}` lacks `superseded_by`.", "Link the accepted replacement.", ledger.line(f"id: {entry_id}")))
        if superseded_by and status != "superseded":
            out.append(diagnostic(ledger, "DLG052", f"Decision `{entry_id}` has `superseded_by` but is not superseded.", "Set the lifecycle status correctly or remove the link.", ledger.line("superseded_by:")))
        if supersedes and status not in {"accepted", "superseded"}:
            out.append(diagnostic(ledger, "DLG053", f"Replacement `{entry_id}` with `supersedes` has invalid status `{status}`.", "A replacement is accepted initially and may later become superseded itself.", ledger.line("supersedes:")))
        for field, reverse in (("supersedes", "superseded_by"), ("superseded_by", "supersedes")):
            value = entry.get(field)
            if not isinstance(value, str) or not DECISION_ID.fullmatch(value):
                continue
            if value == entry_id:
                out.append(diagnostic(ledger, "DLG054", f"Decision `{entry_id}` links to itself through `{field}`.", "Link two distinct decisions.", ledger.line(f"{field}:")))
                continue
            target = indexed.get(value)
            if target is None:
                out.append(diagnostic(ledger, "DLG055", f"Decision `{entry_id}` {field} unknown `{value}`.", "Reference an existing decision in the canonical ledger or archives.", ledger.line(f"{field}:")))
            elif target[1].get(reverse) != entry_id:
                out.append(diagnostic(ledger, "DLG056", f"Decision `{entry_id}` {field} link to `{value}` is not reciprocated.", f"Add matching `{reverse}: {entry_id}`.", ledger.line(f"{field}:")))
            else:
                if field == "supersedes":
                    if target[1].get("status") != "superseded":
                        out.append(diagnostic(ledger, "DLG057", f"Decision `{entry_id}` supersedes `{value}`, but the old decision is not superseded.", "Set the old decision status to `superseded`.", ledger.line(f"supersedes: {value}")))
                    if int(entry_id.split("-", 1)[1]) <= int(value.split("-", 1)[1]):
                        out.append(diagnostic(ledger, "DLG067", f"Replacement `{entry_id}` does not have a newer id than `{value}`.", "Append the replacement with the next sequential decision id.", ledger.line(f"supersedes: {value}")))
                    replacement_date = iso_date(entry.get("date"))
                    replaced_date = iso_date(target[1].get("date"))
                    if replacement_date and replaced_date and replacement_date < replaced_date:
                        out.append(diagnostic(ledger, "DLG068", f"Replacement `{entry_id}` predates `{value}`.", "Correct the dates so replacement history moves forward.", ledger.line(f"supersedes: {value}")))
                if field == "superseded_by" and target[1].get("status") not in {"accepted", "superseded"}:
                    out.append(diagnostic(ledger, "DLG058", f"Decision `{entry_id}` is replaced by `{value}`, but the replacement has invalid status `{target[1].get('status')}`.", "A replacement must be accepted or part of a later supersession chain.", ledger.line(f"superseded_by: {value}")))
        if isinstance(supersedes, str) and DECISION_ID.fullmatch(supersedes):
            graph[entry_id] = supersedes
    for cycle in cycle_paths(graph):
        ledger, _ = indexed[cycle[0]]
        out.append(diagnostic(ledger, "DLG059", f"Supersession cycle detected: {' -> '.join(cycle + [cycle[0]])}.", "Break the cycle and restore one-way replacement history.", ledger.line(f"id: {cycle[0]}")))
    for entry_id, (ledger, entry) in indexed.items():
        if entry.get("status") != "superseded":
            continue
        seen: set[str] = set()
        current_id = entry_id
        while current_id not in seen:
            seen.add(current_id)
            current = indexed.get(current_id)
            if current is None:
                break
            replacement = current[1].get("superseded_by")
            if not isinstance(replacement, str):
                break
            target = indexed.get(replacement)
            if target is None:
                break
            if target[1].get("status") == "accepted":
                break
            current_id = replacement
        else:
            continue
        terminal = indexed.get(current_id)
        if terminal and terminal[1].get("status") == "superseded" and not terminal[1].get("superseded_by"):
            out.append(diagnostic(ledger, "DLG066", f"Supersession chain from `{entry_id}` has no accepted terminal replacement.", "Link the chain to its accepted replacement.", ledger.line(f"id: {entry_id}")))

    candidates = [(entry_id, ledger, entry) for entry_id, (ledger, entry) in indexed.items() if entry.get("status") in {"accepted", "proposed"}]
    rejected = [(entry_id, ledger, entry) for entry_id, (ledger, entry) in indexed.items() if entry.get("status") == "rejected"]
    for index, (left_id, ledger, left) in enumerate(candidates):
        for right_id, _, right in candidates[index + 1 :]:
            if not scopes_overlap(left.get("scope"), right.get("scope")):
                continue
            left_question = normalized(left.get("question"))
            if left_question and left_question == normalized(right.get("question")) and normalized(left.get("statement")) != normalized(right.get("statement")):
                out.append(diagnostic(ledger, "DLG060", f"`{left_id}` and `{right_id}` answer the same question differently.", "Judge whether they conflict, refine one another, or have disjoint scope.", ledger.line(f"id: {left_id}"), severity="candidate"))
            if chosen_rejected(left, right) or chosen_rejected(right, left):
                out.append(diagnostic(ledger, "DLG061", f"`{left_id}` and `{right_id}` choose and reject the same option.", "Judge whether they conflict or have disjoint scope.", ledger.line(f"id: {left_id}"), severity="candidate"))
            left_term = definition_term(left)
            right_term = definition_term(right)
            if left_term and left_term == right_term and normalized(left.get("statement")) != normalized(right.get("statement")):
                out.append(diagnostic(ledger, "DLG062", f"`{left_id}` and `{right_id}` define `{left_term}` differently.", "Judge whether the definitions conflict or have disjoint scope.", ledger.line(f"id: {left_id}"), severity="candidate"))
        for rejected_id, _, old in rejected:
            negative = normalized(old.get("statement")).rstrip(".!?")
            if scopes_overlap(left.get("scope"), old.get("scope")) and negative and negative in normalized(left.get("statement")):
                out.append(diagnostic(ledger, "DLG063", f"`{left_id}` may select rejected decision `{rejected_id}`.", "Judge whether the prior rejection applies to this scope.", ledger.line(f"id: {left_id}"), severity="candidate"))
    return out


def git_root(path: Path) -> Path | None:
    try:
        value = subprocess.run(["git", "-C", str(path.parent), "rev-parse", "--show-toplevel"], check=True, capture_output=True, text=True).stdout.strip()
        return Path(value).resolve()
    except (OSError, subprocess.CalledProcessError):
        return None


def git_text(root: Path, *args: str) -> str | None:
    try:
        return subprocess.run(["git", "-C", str(root), *args], check=True, capture_output=True, text=True).stdout
    except (OSError, subprocess.CalledProcessError):
        return None


def metadata_from_source(source: str) -> dict[str, Any] | None:
    lines = source.splitlines(keepends=True)
    if not lines or lines[0].strip() != "---":
        return None
    end = next((index for index, line in enumerate(lines[1:], 1) if line.strip() == "---"), None)
    if end is None:
        return None
    try:
        value = yaml.load("".join(lines[1:end]), Loader=UniqueKeyLoader)
    except yaml.YAMLError:
        return None
    return value if isinstance(value, dict) else None


def source_entries(source: str | None) -> list[dict[str, Any]]:
    meta = metadata_from_source(source) if source is not None else None
    entries = meta.get("decisions") if isinstance(meta, dict) else None
    return [entry for entry in entries if isinstance(entry, dict) and isinstance(entry.get("id"), str)] if isinstance(entries, list) else []


def ledger_filename(path: Path) -> bool:
    return path.name == "DECISIONS.md" or (path.name == "decisions.md" and path.parent.name == "Decisions") or bool(re.fullmatch(r"archive-.*\.md", path.name))


def git_ledger_paths(root: Path, directory: Path, treeish: str) -> list[str] | None:
    try:
        relative_dir = directory.resolve().relative_to(root).as_posix()
    except ValueError:
        return []
    if treeish == "HEAD":
        output = git_text(root, "ls-tree", "-r", "--name-only", "HEAD", "--", relative_dir)
    else:
        output = git_text(root, "ls-files", "--", relative_dir)
    if output is None:
        return None
    result: list[str] = []
    for value in output.splitlines():
        path = Path(value)
        if path.parent.as_posix() == relative_dir and ledger_filename(path):
            result.append(value)
    return sorted(set(result))


def compare_retained(
    old: dict[str, tuple[Path, dict[str, Any]]],
    new: dict[str, tuple[Path, dict[str, Any]]],
    surface: str,
) -> list[Diagnostic]:
    out: list[Diagnostic] = []
    for entry_id, (old_path, previous) in old.items():
        if previous.get("status") not in {"accepted", "rejected", "superseded"}:
            continue
        match = new.get(entry_id)
        if match is None:
            out.append(diagnostic(None, "DLG070", f"Retained decision `{entry_id}` is missing from the {surface} state.", "Restore it; accepted, rejected, and superseded entries are append-only history.", path=old_path))
            continue
        current_path, now = match
        if previous.get("status") in {"rejected", "superseded"}:
            if now != previous:
                out.append(diagnostic(None, "DLG073", f"Historical decision `{entry_id}` changed in the {surface} state after reaching `{previous.get('status')}`.", "Restore the retained entry; record a new decision for later changes.", path=current_path))
            continue
        old_immutable = {field: value for field, value in previous.items() if field not in {"status", "superseded_by"}}
        new_immutable = {field: value for field, value in now.items() if field not in {"status", "superseded_by"}}
        for field in sorted(set(old_immutable) | set(new_immutable)):
            if old_immutable.get(field) != new_immutable.get(field):
                out.append(diagnostic(None, "DLG071", f"Accepted decision `{entry_id}` changed immutable field `{field}` in the {surface} state.", "Restore the accepted value and record a superseding decision instead.", path=current_path))
        if now.get("status") not in {"accepted", "superseded"}:
            out.append(diagnostic(None, "DLG072", f"Accepted decision `{entry_id}` changed status to `{now.get('status')}` in the {surface} state.", "Accepted entries may only remain accepted or become superseded.", path=current_path))
    return out


def validate_history(primary: Path, ledgers: list[Ledger], entries_by_ledger: dict[Path, list[dict[str, Any]]]) -> list[Diagnostic]:
    root = git_root(primary)
    if root is None:
        return [diagnostic(None, "DLG075", "Git-backed history validation is unavailable.", "Run inside the owning Git repository or use `--no-history` only for an explicitly unversioned audit.", path=primary, severity="operational")]
    if git_text(root, "rev-parse", "--verify", "HEAD") is None:
        return []
    directories = {primary.parent.absolute(), root, root / "Decisions", *(ledger.path.parent.absolute() for ledger in ledgers)}
    head_results = [git_ledger_paths(root, directory, "HEAD") for directory in directories]
    if any(result is None for result in head_results):
        return [diagnostic(None, "DLG076", "Git failed while enumerating historical ledger files.", "Repair the repository/index and rerun validation.", path=primary, severity="operational")]
    head_paths = sorted({path for result in head_results for path in (result or [])})
    history_out: list[Diagnostic] = []
    old: dict[str, tuple[Path, dict[str, Any]]] = {}
    for relative in head_paths:
        source = git_text(root, "show", f"HEAD:{relative}")
        if source is None:
            return [diagnostic(None, "DLG076", f"Git could not read historical ledger `{relative}`.", "Repair the repository and rerun validation.", path=root / relative, severity="operational")]
        for entry in source_entries(source):
            if entry["id"] in old:
                history_out.append(diagnostic(None, "DLG077", f"Historical baseline contains duplicate decision id `{entry['id']}`.", "Preserve both entries, renumber the later one to the next free id, and update all links/citations before continuing.", path=root / relative))
                continue
            old[entry["id"]] = (root / relative, entry)
    worktree = {
        entry["id"]: (ledger.path, entry)
        for ledger in ledgers
        for entry in entries_by_ledger.get(ledger.path, [])
        if isinstance(entry.get("id"), str)
    }
    index: dict[str, tuple[Path, dict[str, Any]]] = {}
    index_results = [git_ledger_paths(root, directory, "index") for directory in directories]
    if any(result is None for result in index_results):
        return [diagnostic(None, "DLG076", "Git failed while enumerating staged ledger files.", "Repair the repository/index and rerun validation.", path=primary, severity="operational")]
    index_paths = sorted({path for result in index_results for path in (result or [])})
    for relative in index_paths:
        source = git_text(root, "show", f":{relative}")
        if source is None:
            return [diagnostic(None, "DLG076", f"Git could not read staged ledger `{relative}`.", "Repair the repository/index and rerun validation.", path=root / relative, severity="operational")]
        for entry in source_entries(source):
            index[entry["id"]] = (root / relative, entry)
    out = [*history_out, *compare_retained(old, index, "staged index"), *compare_retained(old, worktree, "worktree")]
    for entry_id, (path, staged) in index.items():
        if entry_id in old:
            continue
        current = worktree.get(entry_id)
        if current is None or current[1] != staged or current[0].absolute() != path.absolute():
            out.append(diagnostic(None, "DLG074", f"New staged decision `{entry_id}` is absent or different in the worktree.", "Restore or restage the ledger so the worktree and index agree before another writer chooses an id.", path=path))
    return out


def discover(primary: Path) -> list[Path]:
    primary = primary.absolute()
    candidates = {primary}
    repository = git_root(primary)
    canonical = [primary.parent / "DECISIONS.md", primary.parent / "decisions.md"]
    if repository is not None:
        canonical.extend([repository / "DECISIONS.md", repository / "Decisions" / "decisions.md"])
    for path in canonical:
        if path.is_file():
            candidates.add(path.absolute())
    directories = {path.parent for path in candidates}
    if repository is not None:
        directories.update({repository, repository / "Decisions"})
    for directory in directories:
        candidates.update(path.absolute() for path in directory.glob("archive-*.md") if path.is_file())
    return sorted(candidates)


def validate(primary: Path, history: bool = True) -> tuple[list[Diagnostic], int, int]:
    ledgers: list[Ledger] = []
    entries_by_ledger: dict[Path, list[dict[str, Any]]] = {}
    out: list[Diagnostic] = []
    for path in discover(primary):
        ledger, diagnostics = parse_ledger(path)
        out.extend(diagnostics)
        if ledger is None:
            continue
        ledgers.append(ledger)
        diagnostics, entries = validate_ledger(ledger)
        out.extend(diagnostics)
        entries_by_ledger[ledger.path] = entries
    active = [ledger for ledger in ledgers if ledger.meta.get("status") == "active" and ledger.path.name in {"decisions.md", "DECISIONS.md"}]
    if len(active) > 1:
        out.append(diagnostic(active[0], "DLG043", "Multiple active canonical decision ledgers exist for one repository surface.", "Keep exactly the ledger selected by the planning-root location rule and merge retained history before removing the other."))
    if ledgers and not active:
        out.append(diagnostic(ledgers[0], "DLG045", "Ledger archives exist without one active canonical ledger.", "Restore `Decisions/decisions.md` or repository-root `DECISIONS.md` and keep archives as siblings."))
    out.extend(validate_collection(ledgers, entries_by_ledger))
    if history:
        out.extend(validate_history(primary, ledgers, entries_by_ledger))
    return sorted(out, key=lambda item: (item.path, item.line, item.code, item.message)), len(ledgers), sum(len(items) for items in entries_by_ledger.values())


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("ledger", help="Canonical `Decisions/decisions.md`, external `DECISIONS.md`, or an archive sibling")
    result.add_argument("--format", choices=("text", "json"), default="text", dest="output")
    result.add_argument("--no-history", action="store_true", help="Skip Git-backed accepted-entry immutability checks")
    return result


def main(argv: Sequence[str] | None = None) -> int:
    args = parser().parse_args(argv)
    primary = Path(args.ledger).absolute()
    if not primary.is_file():
        message = f"ledger does not exist: {primary}"
        if args.output == "json":
            print(json.dumps({"valid": False, "ledger": str(primary), "error": message, "diagnostics": []}, indent=2, sort_keys=True))
        else:
            print(f"sdd-decision-validate: {message}", file=sys.stderr)
        return 2
    diagnostics, inspected, decisions = validate(primary, not args.no_history)
    operational = [item for item in diagnostics if item.severity == "operational"]
    errors = [item for item in diagnostics if item.severity == "error"]
    if args.output == "json":
        print(json.dumps({"valid": not errors and not operational, "ledger": str(primary), "ledgers_inspected": inspected, "decisions_inspected": decisions, "diagnostics": [asdict(item) for item in diagnostics]}, indent=2, sort_keys=True))
    else:
        print(f"{'Valid' if not errors and not operational else 'Invalid'}: {primary} ({inspected} ledgers, {decisions} decisions inspected)")
        for item in diagnostics:
            print(f"{item.severity.upper()} {item.code} {item.path}:{item.line}: {item.message}")
            print(f"  Required correction: {item.correction}")
        if not diagnostics:
            print("Checked frontmatter, entry schema and types, ids, dates, archives, scopes, supersession, structural collisions, body links, and accepted-entry immutability.")
    if operational:
        return 2
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
