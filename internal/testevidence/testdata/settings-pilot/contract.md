# Settings codec pilot contract

This is a controlled, test-owned workload. Its functions are scaffolding for an evidence-pipeline experiment, not a shipped application API.

## Shared API

Package `settings` exposes `Parse([]byte) (map[string]string, error)`, `Render(map[string]string) ([]byte, error)`, `Save(path string, settings map[string]string, beforeReplace func() error) error`, and `Load(path string) (map[string]string, error)`.

## Parse behavior

- Accept exactly one JSON object with string values; allow JSON whitespace around it.
- Preserve all key and value text, including quotes, backslashes, newlines, Unicode and empty strings. Return a non-nil map for an empty object.
- Reject duplicate keys instead of silently retaining either value.
- Reject arrays, null, nested/non-string values, malformed input and trailing non-whitespace data. Error text is not contractual.

## Render behavior

- Emit one valid JSON object with string values, ordered by ascending key, without a trailing newline. A nil map produces `{}`.
- Escape values according to JSON rules and preserve their text when decoded by an independent standard JSON decoder.
- Preserve the input map without mutation. Repeated rendering of the same contents produces identical bytes.

## Persistence behavior

- `Save` writes the rendered bytes through a temporary file in the destination directory and atomically replaces the destination on success. `Load` reads through the public parser.
- If supplied, `beforeReplace` runs after temporary-file preparation and before replacement. A returned error is propagated so `errors.Is` recognizes it; the prior destination remains byte-identical and readable, and no temporary file remains.
- A successful save/load round trip preserves the settings, including hostile-format strings. Tests must use real test-owned files, not a mocked persistence layer.
- The failure case must prove the callback was reached and that the old destination was intact at that boundary. Merely receiving any error is insufficient.

## Execution layout

The harness materializes `parse.go`, `render.go`, and `store.go` from the companion scaffold in one temporary Go module. Implementations are independently replaceable so parse and render can occupy disjoint graph nodes; persistence depends on both. Each generated test set uses exactly `TestParseContract`, `TestRenderContract`, and `TestStoreContract` as its top-level selected identities, with meaningful subcases.

The controlled fixture explicitly assigns `external-format` to the render family and `persists-state` to the persistence family. The parser's hazard list is explicitly empty: it consumes this format rather than emitting it, but still owes the stated invalid-input tests. This fixture triage is not a default for application plans.
