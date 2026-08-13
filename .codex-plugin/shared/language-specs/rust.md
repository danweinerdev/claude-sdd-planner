# Rust — Structural Verification

Apply when planning, implementing, or reviewing code detected by `.rs`; `Cargo.toml`. Dispatched via `shared/language-verification.md`.

## Tools

| Tool | When | What it catches |
|------|------|-----------------|
| `cargo clippy -- -D warnings` | Always | Common mistakes, anti-patterns, performance issues |
| `miri` | `unsafe` code, raw pointers, FFI boundaries | Undefined behavior, aliasing violations, memory errors |

## Minimum Bar

Clippy with warnings-as-errors on every phase.
