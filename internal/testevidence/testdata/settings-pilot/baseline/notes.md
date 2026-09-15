# Test selection

- `TestParseContract`: valid string preservation, whitespace, non-nil empty objects, duplicate keys (including equivalent escaped keys), and each stated invalid-input family.
- `TestRenderContract`: canonical ordering and framing, nil maps, JSON escaping with independent decoding, input immutability, and deterministic repetition.
- `TestStoreContract`: real-file round trips with hostile strings, exact rendered persistence, callback-error propagation, destination integrity at the callback boundary, destination-directory temporary-file cleanup, and parser-backed load rejection.

The persistence boundary check assumes a prepared named temporary file is observable in the destination directory while the callback runs. No tests were executed; these files are intended for later materialization with the scaffold.
