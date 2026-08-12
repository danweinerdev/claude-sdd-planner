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
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
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


# Setup replay runs with a fixed identity and timestamp so a fixture commit's
# SHA is reproducible, matching internal/rules/rules_test.go's runExample. A
# fixture may hardcode a SHA only because of this.
SETUP_ENV = {
    "GIT_AUTHOR_NAME": "sdd-fixture",
    "GIT_AUTHOR_EMAIL": "sdd-fixture@example.com",
    "GIT_COMMITTER_NAME": "sdd-fixture",
    "GIT_COMMITTER_EMAIL": "sdd-fixture@example.com",
    "GIT_AUTHOR_DATE": "2024-01-01T00:00:00+0000",
    "GIT_COMMITTER_DATE": "2024-01-01T00:00:00+0000",
    "GIT_CONFIG_NOSYSTEM": "1",
    "GIT_CONFIG_GLOBAL": "/dev/null",
}


def prepare(root: str, scratch: str) -> str:
    """Return a root ready to validate, materializing it when it needs run-time work.

    A fixture that carries a SETUP script or a {{REPO}} placeholder cannot be
    validated where it sits: the first needs a real git repository, and the
    second needs the absolute path of the directory it ends up in. Both are
    resolved by copying the root to a scratch directory and finishing it there,
    which leaves the committed corpus untouched and keeps these rules — every
    git-verifying one — inside the comparison instead of outside it.
    """
    source = pathlib.Path(root)
    setup = source / "SETUP"
    needs_repo = any(
        "{{REPO}}" in p.read_text(encoding="utf-8", errors="replace")
        for p in source.rglob("*.md")
    )
    if not setup.is_file() and not needs_repo:
        return root

    target = pathlib.Path(scratch) / source.name
    shutil.copytree(source, target)
    prepared_setup = target / "SETUP"
    commands = []
    if prepared_setup.is_file():
        for line in prepared_setup.read_text().splitlines():
            line = line.rstrip("\n")
            if not line or line.startswith("#"):
                continue
            commands.append(line.split("\t"))
        # SETUP is harness metadata, not part of the planning root.
        prepared_setup.unlink()

    if needs_repo:
        for path in target.rglob("*.md"):
            text = path.read_text(encoding="utf-8", errors="replace")
            if "{{REPO}}" in text:
                path.write_text(text.replace("{{REPO}}", str(target)))

    env = {**os.environ, **SETUP_ENV}
    for argv in commands:
        result = subprocess.run(argv, cwd=target, capture_output=True, env=env)
        if result.returncode != 0:
            raise SystemExit(
                f"parity: setup {argv} failed in {target}:\n"
                + result.stderr.decode("utf-8", errors="replace")
            )
    return str(target)


FROZEN_PATH = REPO / "tools" / "parity" / "frozen-expectations.json"
_frozen_cache: dict | None = None


def frozen_expectation(root: str) -> tuple[int, list[dict]] | None:
    """Return the recorded oracle verdict for a fixture root, or None.

    Roots are keyed by their manifest-relative path, so a frozen run works
    from any working directory and against the prepared copy of a root whose
    absolute path differs every time.
    """
    global _frozen_cache
    if _frozen_cache is None:
        if not FROZEN_PATH.is_file():
            raise SystemExit(
                "parity: --frozen needs tools/parity/frozen-expectations.json; "
                "run tools/parity/freeze.py while the Python oracle still exists"
            )
        _frozen_cache = json.loads(FROZEN_PATH.read_text())
    # Key on the manifest-relative path exactly. An earlier version fell back
    # to matching by basename, which silently collided: several rules use the
    # same example name (`missing-title`, `bad-status`), so a root could be
    # compared against another rule's expectation and report phantom `extra`
    # diagnostics.
    fixtures = (REPO / "tools" / "parity" / "fixtures").resolve()
    try:
        key = pathlib.Path(root).resolve().relative_to(fixtures).as_posix()
    except ValueError:
        return None
    value = _frozen_cache.get(key)
    return (value["exit"], value["diagnostics"]) if value else None


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


def load_allowlist(path: str | None) -> set[str]:
    """Codes Python may report that Go is not expected to, yet.

    The port is incremental, so a strict gate would be red for reasons that are
    already known and tracked. The allowlist names those codes explicitly:
    anything NOT in it that goes missing is a regression and fails the build.
    `extra` is never allowlisted — Go inventing a diagnostic Python does not
    emit is always a defect, whatever the port's state.
    """
    if not path:
        return set()
    codes = set()
    for line in pathlib.Path(path).read_text().splitlines():
        line = line.split("#", 1)[0].strip()
        if line:
            codes.add(line)
    return codes


def compare(py: list[dict], go: list[dict], allow: set[str] = frozenset(),
            allow_text: set[str] = frozenset()) -> dict:
    pk, gk = Counter(map(key, py)), Counter(map(key, go))
    missing = pk - gk   # Python found it, Go did not
    extra = gk - pk     # Go invented it
    shared = set(pk) & set(gk)

    pmsg = {key(d): d for d in py}
    gmsg = {key(d): d for d in go}
    text_diffs, sev_diffs = [], []
    for k in sorted(shared):
        # A frozen expectation records identity and severity but not message
        # text: two codes interpolate CPython and PyYAML exception strings and
        # diverge deliberately, so freezing text would bake in a difference we
        # chose to keep. Skip the text comparison when it is absent.
        if "message" not in pmsg[k]:
            continue
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
        # Gating verdict: unexpected misses, ANY extra, and unexpected message
        # drift. A miss or drift whose code is allowlisted is known, tracked
        # work rather than a regression.
        #
        # Message text gates because a diagnostic can be right about WHERE and
        # wrong about WHY: a rule that falls through to a later branch still
        # reports the same (code, path, line), so identity comparison alone
        # calls it a match. A validator that misdiagnoses a defect is a defect.
        "unexpected_missing": [{"key": list(k), "n": n}
                               for k, n in sorted(missing.items()) if k[0] not in allow],
        "unexpected_message_diffs": [t for t in text_diffs if t["key"][0] not in allow_text],
        "gate_ok": (not extra
                    and not any(k[0] not in allow for k in missing)
                    and not any(t["key"][0] not in allow_text for t in text_diffs)),
        "full_parity": not missing and not extra and not text_diffs and not sev_diffs,
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("roots", nargs="*")
    ap.add_argument("--manifest", help="file listing one planning root per line "
                                       "(blank lines and # comments ignored)")
    ap.add_argument("--binary", default=default_binary(),
                    help="path to the sdd binary (default: %(default)s)")
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--codes", action="store_true", help="summarize gaps by code")
    ap.add_argument("--allow", help="file listing diagnostic codes Python may "
                                    "report that Go need not (one per line); "
                                    "`extra` is never allowlisted")
    ap.add_argument("--allow-message-drift", help="file listing codes whose "
                                                  "MESSAGE text may differ; their "
                                                  "identity is still gated")
    ap.add_argument("--frozen", action="store_true",
                    help="compare against tools/parity/frozen-expectations.json "
                         "instead of running the Python oracle live; the only "
                         "mode available once scripts/ is deleted")
    a = ap.parse_args()

    allow = load_allowlist(a.allow)
    allow_text = load_allowlist(a.allow_message_drift)
    roots = list(a.roots)
    if a.manifest:
        base = pathlib.Path(a.manifest).resolve().parent
        for line in pathlib.Path(a.manifest).read_text().splitlines():
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            # Manifest entries are relative to the manifest's own directory,
            # so the corpus can be regenerated anywhere and still compare byte
            # for byte. Resolve against that directory, not the cwd.
            p = pathlib.Path(line)
            roots.append(str(p if p.is_absolute() else (base / p)))
    if not roots:
        ap.error("no roots given: pass paths, --manifest, or both")

    overall, results = True, {}
    scratch = tempfile.mkdtemp(prefix="sdd-parity-")
    try:
        overall, results = compare_roots(roots, a, allow, allow_text, scratch)
    finally:
        shutil.rmtree(scratch, ignore_errors=True)
    return report_results(overall, results, a)


def compare_roots(roots, a, allow, allow_text, scratch):
    overall, results = True, {}
    for root in roots:
        prepared = prepare(root, scratch)
        if a.frozen:
            recorded = frozen_expectation(root)
            if recorded is None:
                raise SystemExit(f"parity: no frozen expectation for {root}")
            prc, py = recorded
        else:
            prc, py = run_python(prepared)
        grc, go = run_go(prepared, a.binary)
        r = compare(py, go, allow, allow_text)
        r["python_exit"], r["go_exit"] = prc, grc
        r["exit_match"] = prc == grc
        results[root] = r
        overall &= r["gate_ok"] and r["exit_match"]

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

    return overall, results


def report_results(overall, results, a) -> int:
    if a.json:
        print(json.dumps(results, indent=2))
    return 0 if overall else 1


if __name__ == "__main__":
    sys.exit(main())
