# Language-Specific Verification — What Good Looks Like

What "good and complete" looks like beyond passing tests. The per-language detail lives in one reference document per language (the table below), each covering **structural verification tools** (sanitizers, static analysis, type checkers) and **quality patterns** (idioms, safety conventions, review checkpoints) specific to that language. These run during implementation, not as deferred acceptance criteria.

## How to Use

1. Detect the project language from file extensions, build files, or project config.
<!-- claude-only -->
2. Apply the matching language reference. In the **primary context** the `<lang>-specifications` skill auto-loads by description when you're planning, implementing, or reviewing that language — its body arrives without an explicit read. In a **restricted agent context** (or to load it deliberately), read the skill body directly from the path in the table below, under the plugin directory.
<!-- /claude-only -->
<!-- portable-only
2. Read the matching language reference from the path in the table below, under the plugin's `shared/` directory. Nothing auto-loads it; the reading agent opens it when the detected language matches.
-->
3. Include the relevant checks in your output (verification fields, testing strategy, review findings).

When a project uses multiple languages, apply each relevant reference.

## Skill Integration

- **`/design`** — include relevant tools in the Testing Strategy section
- **`/plan`** — include in task `verification` fields where appropriate (both when creating a new plan and when expanding an existing one)
- **`/implement`** — run these checks as part of verifying task completion
- **`/code-review`** — check that structural verification was actually performed

## Languages

| Language | Reference |
|----------|-----------|
| C / C++ | `skills/cpp-specifications/SKILL.md` |
| Rust | `skills/rust-specifications/SKILL.md` |
| Go | `skills/go-specifications/SKILL.md` |
| Python | `skills/python-specifications/SKILL.md` |
| TypeScript / JavaScript | `skills/typescript-specifications/SKILL.md` |
| Java / Kotlin | `skills/java-specifications/SKILL.md` |
| Swift | `skills/swift-specifications/SKILL.md` |

## Unlisted Languages

For languages not listed: look for an existing CI config, Makefile, or package scripts to identify the project's structural checks. If the project already runs static analysis, linting, or sanitizers in CI, those same checks belong in task verification. Don't introduce new tooling the project doesn't already use — but do ensure existing tools are actually run.
