# Python — Structural Verification

Apply when planning, implementing, or reviewing code detected by `.py`; `pyproject.toml`, `setup.cfg`, `ruff.toml`. Dispatched via `shared/language-verification.md`.

## Tools

| Tool | When | What it catches |
|------|------|-----------------|
| Type checker (mypy / pyright) | Project uses type annotations | Type mismatches, missing attributes, incorrect signatures |
| Linter (ruff / flake8) | If configured in project | Unused imports, undefined names, common bugs |

## Minimum Bar

Run the type checker if the project has type annotations or a `py.typed` marker. Run the linter if configured (check `pyproject.toml`, `setup.cfg`, `ruff.toml`).
