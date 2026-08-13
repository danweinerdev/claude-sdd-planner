# Swift — Structural Verification

Apply when planning, implementing, or reviewing code detected by `.swift`; `Package.swift`, `.xcodeproj`. Dispatched via `shared/language-verification.md`.

## Tools

| Tool | When | What it catches |
|------|------|-----------------|
| SwiftLint | If configured in project | Style violations, force-unwraps, unused code |
| TSAN (`-sanitize=thread`) | Concurrency, actors, async/await with shared state | Data races |

## Minimum Bar

Run SwiftLint if configured. Consider TSAN for concurrent code.
