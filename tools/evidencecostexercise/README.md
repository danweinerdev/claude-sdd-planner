# Historical real-feature evidence-cost exercise

This experimental developer utility builds a test-owned Git copy from the explicit
`cmd/sdd`, `internal`, and Go-module inventory at `0afcfc8`, including tests and
embedded assets. It overlays enumerated foundation and feature files, creates
the spec and plan through `sdd apply` and lifecycle commands, and drives real
graph, claim, observed-test, and Git integration to the review boundary. It
does not create or admit a review result. Raw CLI reports stay private;
`summary.json` records sanitized measurements, source groups and hashes,
source identity, graph status, outcomes, and the literal frozen review range.

It targets the former experiment binary and attempt protocol solely to preserve
and verify the recorded measurements. It is not a production graph-walking
path: production SDD does not execute tests, and this utility's commands must
not be presented as current `sdd` capabilities or as provenance for
`reported-v1` evidence.

The current checkout no longer contains the former executor at the overlay
paths this utility expects. Its `run` mode is therefore **not runnable against
the current source tree**: reproduction needs the historical experiment
source and controller together. Archived source is retained under
`tools/experimental/owned-evidence/historical/`; it is not a drop-in current
CLI installation. For the corrected ownership exercise, run
`go test -count=1 ./cmd/sdd -run '^TestReportedEvidencePilot$' -v` instead.

Run with explicit paths (the work root must not exist):

```text
go run ./tools/evidencecostexercise --mode run --source <repo> --work-root <new-temp-dir> --controller <baseline-sdd> --module-cache <go-env-GOMODCACHE>
```

Never refresh or resume a frozen run in place. After source or controller
corrections, invoke `--mode run` with a new work root. `--mode verify` recomputes
every declared hash from both `<work-root>/primary` and `--source`, reports
missing/stale files, and distinguishes a historical-primary-only match.

`--mode compare --candidate <candidate-sdd>` binds measurements to the frozen
primary copy and historical summary hashes; the working source repository may
already contain the candidate optimization. It records independent hashes for
both controller binaries. A separate generated plan uses two artifact-free,
read-only nodes with distinct live claims and the same four real integration
tests, plus an unrun full-review node. Three alternating check pairs must leave
source and graph state byte-identical. Each controller then admits its own
fresh attempt exactly once; historical replay is refused and the expected
bundle-read/legacy-anchor counters are asserted. Raw reports and attempts remain
available, residual claims are released through the CLI, and
`comparison.json` makes no timing or statistical speedup claim.

An interrupted comparison may resume only when the existing generated plan is
active, has exactly the expected declarations, and has no claims or
observations. The utility then runs `sdd graph gc` to reap only supported orphan
state; it never steals a claim or reinitializes an existing graph.

The child environment disables Go downloads/toolchain switching and Git
prompts, signing, hooks, and system/global configuration. No source-repository
commit or remote operation is performed. On Windows, child processes receive
the process-local Git environment override `core.longpaths=true` so nested
test-owned worktrees can materialize; no Git configuration file is changed.
