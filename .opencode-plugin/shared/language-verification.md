# Language-Specific Verification — What Good Looks Like

What "good and complete" looks like beyond passing tests. The per-language detail lives in one reference document per language (the table below), each covering **structural verification tools** (sanitizers, static analysis, type checkers) and **quality patterns** (idioms, safety conventions, review checkpoints) specific to that language. These run during implementation, not as deferred acceptance criteria.

## How to Use

1. Detect the project language from file extensions, build files, or project config.
2. Read the matching language reference from the path in the table below, under the plugin's `shared/` directory. Nothing auto-loads it; the reading agent opens it when the detected language matches.
3. Include the relevant checks in your output (verification fields, testing strategy, review findings).

When a project uses multiple languages, apply each relevant reference.

## Skill Integration

- **`sdd-design`** — include relevant tools in the Testing Strategy section
- **`sdd-plan`** — include in task `verification` fields where appropriate (both when creating a new plan and when expanding an existing one)
- **`sdd-implement`** — run these checks as part of verifying task completion
- **`sdd-code-review`** — check that structural verification was actually performed

## Languages

| Language | Reference |
|----------|-----------|
| C / C++ | `shared/language-specs/cpp.md` |
| Rust | `shared/language-specs/rust.md` |
| Go | `shared/language-specs/go.md` |
| Python | `shared/language-specs/python.md` |
| TypeScript / JavaScript | `shared/language-specs/typescript.md` |
| Java / Kotlin | `shared/language-specs/java.md` |
| Swift | `shared/language-specs/swift.md` |

## Unlisted Languages

For languages not listed: look for an existing CI config, Makefile, or package scripts to identify the project's structural checks. If the project already runs static analysis, linting, or sanitizers in CI, those same checks belong in task verification. Don't introduce new tooling the project doesn't already use — but do ensure existing tools are actually run.
