## Context
Controlled settings pilot; authority: `internal/testevidence/testdata/settings-pilot/contract.md`. Inspected `scaffold/parse.go.txt`, `scaffold/render.go.txt`, and `scaffold/store.go.txt`; the materialized tests use package `settings`, public APIs, the standard library, fixed top-level test identities, and test-owned temporary storage. Same-context design assessment: `Assessment: ready`; every case has a contract-derived oracle and a real scaffold seam, with no located findings.

## Behavior and source
`Parse` accepts exactly one string-valued JSON object while preserving text and rejecting duplicate, malformed, non-object, non-string, and trailing input. `Render` emits deterministic ascending-key JSON, preserves text and its input map, and renders nil as `{}`. `Save`/`Load` round-trip real files; a pre-replacement fault is recognizable, observes the old destination at the callback boundary, preserves it afterward, and leaves no temporary file. These obligations come from the pilot contract.

## Risk and level
Parser and renderer contract tests target silent duplicate acceptance, type coercion, text loss, unstable ordering, invalid escaping, and map mutation at unit level. Store component tests use the real filesystem because replacement, cleanup, and byte preservation cannot be established with a mock.

## Existing coverage
No existing tests are present in the inspected controlled scaffold. The three generated files provide the contract coverage without relying on prohibited baseline, implementation, mutant, or harness material.

## Cases and oracle
- `TestParseContract` in `parse_test.go`: `preserves_string_values_and_whitespace` compares special and empty strings directly; `empty_object_returns_non_nil_map` checks the empty boundary; named `rejects_*` subcases require errors for duplicate keys, wrong top-level/value types, malformed input, and trailing data. These detect lossy decoding and permissive parsing.
- `TestRenderContract` in `render_test.go`: `orders_keys` uses contract-written exact bytes; `escapes_preserves_and_is_stable` decodes with the independent standard JSON decoder, compares repeated bytes, checks no trailing newline, and snapshots the input; `nil_map_is_empty_object` checks exact `{}`. These detect ordering, escaping, mutation, and nondeterminism defects.
- `TestStoreContract` in `store_test.go`: `save_load_round_trip` uses hostile-format strings, independently decodes stored JSON, and checks public `Load`; `fault_before_replace_preserves_destination` uses a sentinel callback error, verifies callback reach, old bytes at that boundary, prepared temporary state, post-error readability and byte identity, and directory cleanup. These detect missing persistence, bypassed callbacks, non-atomic replacement, and leaked temporary files.

## Subject and seam
Tests call `Parse`, `Render`, `Save`, and `Load` exactly as declared by the scaffold. JSON validation uses `encoding/json`, not `Parse`, as the rendering oracle. Store tests use `t.TempDir`, an actual destination file, directory observations, and the supplied `beforeReplace` seam; no production or test-support changes are needed.

## Red and sensitivity
Prospective RED: the supplied stubs return nil values or success without parsing, rendering, writing, loading, invoking the callback, or rejecting invalid input, so behavioral assertions are expected to fail when the harness runs them. This is not an observed run. No extra sensitivity experiment is proposed because the ordinary contract cases directly exercise the named faults, including callback-boundary state.

## Execution
Selected identities: `TestParseContract`, `TestRenderContract`, and `TestStoreContract`. Exact proposed command in the materialized module: `go test ./... -run '^(TestParseContract|TestRenderContract|TestStoreContract)$' -count=1 -v`. Surrounding regression command: `go test ./...`. Runtime capabilities and all RED/GREEN outcomes are unchecked; the primary harness will execute and record evidence.
