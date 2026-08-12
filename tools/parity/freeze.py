#!/usr/bin/env python3
"""Freeze the Python oracle's output for every fixture root.

The parity corpus holds inputs only. While both validators exist, that is
enough — the oracle is run live and the two are compared. Deleting the Python
(task 5.1) removes that oracle, and the fixtures would then prove nothing:
they would be 128 directories with no statement of what the right answer is.

This captures the oracle's verdict once, as committed data, so the corpus
keeps its meaning after the source of truth is gone. Run it BEFORE deleting
scripts/, with both validators present; from then on `parity.py --frozen`
compares Go against the recorded expectations instead of against a live
Python process.

The frozen file is the Python validator's last word. It is never regenerated
after the deletion — a change to it would be a change to what "correct" means,
which is exactly what freezing is meant to prevent.
"""
import json
import pathlib
import subprocess
import sys

REPO = pathlib.Path(__file__).resolve().parents[2]
FIXTURES = REPO / "tools" / "parity" / "fixtures"
FROZEN = REPO / "tools" / "parity" / "frozen-expectations.json"


def python_bin() -> str:
    venv = REPO / ".venv" / "bin" / "python3"
    return str(venv) if venv.exists() else "python3"


def run_oracle(root: str) -> dict:
    """Return the Python validator's diagnostics for one root."""
    proc = subprocess.run(
        [python_bin(), str(REPO / "scripts/sdd_validate.py"),
         "--root", root, "--format", "json"],
        capture_output=True, text=True,
    )
    diagnostics = []
    if proc.stdout.strip():
        try:
            doc = json.loads(proc.stdout)
            diagnostics = doc.get("diagnostics", doc if isinstance(doc, list) else [])
        except json.JSONDecodeError:
            pass
    # Only identity and severity are frozen, not message text. Message wording
    # is compared separately by parity.py and two codes already diverge
    # deliberately (they interpolate CPython and PyYAML exception strings), so
    # freezing text would bake in a difference we have chosen to keep.
    return {
        "exit": proc.returncode,
        "diagnostics": sorted(
            [
                {
                    "code": d.get("code", ""),
                    "path": (d.get("path") or "").replace("\\", "/"),
                    "line": d.get("line", 0),
                    "severity": d.get("severity", ""),
                }
                for d in diagnostics
            ],
            key=lambda d: (d["path"], d["line"], d["code"]),
        ),
    }


def main() -> int:
    manifest = FIXTURES / "MANIFEST"
    if not manifest.is_file():
        print("freeze: no fixture manifest; run `make gen-fixtures` first", file=sys.stderr)
        return 2

    sys.path.insert(0, str(REPO / "tools" / "parity"))
    import parity  # reuse prepare() so SETUP and {{REPO}} roots freeze correctly

    import tempfile
    import shutil

    frozen = {}
    scratch = tempfile.mkdtemp(prefix="sdd-freeze-")
    try:
        for line in manifest.read_text().splitlines():
            line = line.split("#", 1)[0].strip()
            if not line:
                continue
            root = str(FIXTURES / line)
            prepared = parity.prepare(root, scratch)
            frozen[line] = run_oracle(prepared)
    finally:
        shutil.rmtree(scratch, ignore_errors=True)

    FROZEN.write_text(json.dumps(frozen, indent=2, sort_keys=True) + "\n")
    total = sum(len(v["diagnostics"]) for v in frozen.values())
    print(f"froze {len(frozen)} roots, {total} diagnostics -> {FROZEN.relative_to(REPO)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
