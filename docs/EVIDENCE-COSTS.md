# Historical evidence-cost instrumentation

The execution pilot instrumented experimental `sdd test run`, `sdd test check`,
and `sdd graph sync` handlers with `--cost` to add anonymous per-invocation
timing and counter data. These measurements remain useful historical evidence;
they do not describe a production command or optional/hidden execution flag.
Production SDD does not execute tests. Repository tooling produces native
reports and strict metadata, and `sdd graph sync --report ... --metadata ...`
validates them. The pilot costs were output only and were not written into
attempts, graph verification records, receipts, or graphs.

`total_ns` begins with the command handler's first operation after flag parsing
and ends at the output-serialization boundary. It therefore includes root and
plan resolution, input/report reads, configuration loading, engine work, and
error handling up to serialization. Values in `phase_ns` are **inclusive**, phases may
nest, and phase values must never be summed. Counters are logical requests at
the named call sites. `root_hash_requests` counts explicit root-relative file
or directory hash requests, including module and test sources; a request may
fail before hashing and is not a claim about physical I/O. Values are
measurements of one invocation, not an
optimization result or performance guarantee.

`legacy_anchor_scans` counted the sync-time anchor path used by legacy gates;
it was essential work for those gates, not a deprecation signal. In the pilot,
comparing this counter with captured-candidate checks identified discarded
work without establishing an optimization or speedup. `vcs_query_requests` counted explicit repository detection,
provenance and cleanliness API calls, not spawned Git processes; arithmetic
isolation classification does not increment it.

`workspace_release_requests` counted provider release API calls after a merged
sync. `workspace_release` measured the call through its return, including a
failed release; neither value claims a number of Git subprocesses.
