#!/usr/bin/env python3
"""Differential oracle: compare `sdd validate` against sdd_validate.py.

This is the acceptance test for the Go port (FR-06, FR-07, FR-32). It runs both
implementations over the same planning root and diffs their diagnostics keyed by
(code, path, line), so "parity" is a measured number rather than a claim.

Message text is compared separately from diagnostic identity: a missing
diagnostic and a differently-worded one are different failures, and conflating
them hides which of the two you have.

Usage:
    tools/parity/parity.py <root>... [--binary PATH] [--json] [--codes]

--binary defaults to build/<goos>-<goarch>-debug/sdd, the layout `make build`
writes. The debug variant is deliberate: a failing comparison is something you
then attach a debugger to, and the release variant is stripped.
`make parity PARITY_ROOTS=<root>` builds the host binary first, so the
comparison never runs against a stale artifact.
Exit status is 0 only when every root matches on identity.
"""
import argparse
import json
import pathlib
import subprocess
import sys
from collections import Counter

REPO = pathlib.Path(__file__).resolve().parents[2]


def default_binary() -> str:
    """The host binary `make build` writes: build/<goos>-<goarch>-debug/sdd.

    Resolved via `go env` rather than Python's platform module, so the tuple
    always matches what the Go toolchain itself would name.
    """
    try:
        env = subprocess.run(["go", "env", "GOOS", "GOARCH"],
                             capture_output=True, text=True, check=True)
        goos, goarch = env.stdout.split()
    except (OSError, subprocess.CalledProcessError, ValueError):
        return "build/sdd"
    name = "sdd.exe" if goos == "windows" else "sdd"
    return str(pathlib.Path("build") / f"{goos}-{goarch}-debug" / name)


def run_python(root: str) -> tuple[int, list[dict]]:
    py = REPO / ".venv/bin/python3"
    if not py.exists():
        py = pathlib.Path("python3")
    p = subprocess.run(
        [str(py), str(REPO / "scripts/sdd_validate.py"), "--root", root, "--format", "json"],
        capture_output=True, text=True,
    )
    return p.returncode, parse(p.stdout)


def run_go(root: str, binary: str) -> tuple[int, list[dict]]:
    p = subprocess.run(
        [str(REPO / binary), "validate", "--root", root, "--format", "json"],
        capture_output=True, text=True,
    )
    return p.returncode, parse(p.stdout)


def parse(out: str) -> list[dict]:
    if not out.strip():
        return []
    try:
        doc = json.loads(out)
    except json.JSONDecodeError:
        return []
    diags = doc.get("diagnostics", doc if isinstance(doc, list) else [])
    norm = []
    for d in diags:
        norm.append({
            "code": d.get("code", ""),
            "path": (d.get("path") or "").replace("\\", "/"),
            "line": d.get("line", 0),
            "severity": d.get("severity", ""),
            "message": (d.get("message") or "").strip(),
            "correction": (d.get("correction") or "").strip(),
        })
    return norm


def key(d: dict) -> tuple:
    """Diagnostic identity: the same finding at the same place."""
    return (d["code"], d["path"], d["line"])


def compare(py: list[dict], go: list[dict]) -> dict:
    pk, gk = Counter(map(key, py)), Counter(map(key, go))
    missing = pk - gk   # Python found it, Go did not
    extra = gk - pk     # Go invented it
    shared = set(pk) & set(gk)

    pmsg = {key(d): d for d in py}
    gmsg = {key(d): d for d in go}
    text_diffs, sev_diffs = [], []
    for k in sorted(shared):
        if pmsg[k]["message"] != gmsg[k]["message"]:
            text_diffs.append({"key": list(k), "python": pmsg[k]["message"], "go": gmsg[k]["message"]})
        if pmsg[k]["severity"] != gmsg[k]["severity"]:
            sev_diffs.append({"key": list(k), "python": pmsg[k]["severity"], "go": gmsg[k]["severity"]})

    return {
        "python_total": len(py), "go_total": len(go),
        "matched": len(shared),
        "missing": [{"key": list(k), "n": n} for k, n in sorted(missing.items())],
        "extra": [{"key": list(k), "n": n} for k, n in sorted(extra.items())],
        "message_diffs": text_diffs,
        "severity_diffs": sev_diffs,
        "identity_parity": not missing and not extra,
        "full_parity": not missing and not extra and not text_diffs and not sev_diffs,
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("roots", nargs="+")
    ap.add_argument("--binary", default=default_binary(),
                    help="path to the sdd binary (default: %(default)s)")
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--codes", action="store_true", help="summarize gaps by code")
    a = ap.parse_args()

    overall, results = True, {}
    for root in a.roots:
        prc, py = run_python(root)
        grc, go = run_go(root, a.binary)
        r = compare(py, go)
        r["python_exit"], r["go_exit"] = prc, grc
        r["exit_match"] = prc == grc
        results[root] = r
        overall &= r["identity_parity"] and r["exit_match"]

        if a.json:
            continue
        print(f"=== {root}")
        print(f"  python {r['python_total']:5d} diagnostics (exit {prc})")
        print(f"  go     {r['go_total']:5d} diagnostics (exit {grc})")
        print(f"  matched {r['matched']:4d}   missing {len(r['missing']):4d}   extra {len(r['extra']):4d}")
        if r["message_diffs"]:
            print(f"  message differences: {len(r['message_diffs'])}")
        if r["severity_diffs"]:
            print(f"  severity differences: {len(r['severity_diffs'])}")
        if a.codes:
            miss = Counter(k["key"][0] for k in r["missing"])
            ext = Counter(k["key"][0] for k in r["extra"])
            if miss:
                print("  MISSING by code (python found, go did not):")
                for c, n in miss.most_common(30):
                    print(f"      {c}  {n}")
            if ext:
                print("  EXTRA by code (go invented):")
                for c, n in ext.most_common(30):
                    print(f"      {c}  {n}")
        verdict = "FULL PARITY" if r["full_parity"] else (
            "identity parity, message drift" if r["identity_parity"] else "NOT AT PARITY")
        print(f"  {verdict}")

    if a.json:
        print(json.dumps(results, indent=2))
    return 0 if overall else 1


if __name__ == "__main__":
    sys.exit(main())
