# Repository-owned test reports

## Ownership

SDD owns graph requirements and evidence validation, not test execution. The graph-walking agent invokes repository-owned test tooling and supplies reports to sdd, which validates that the required tests were present, executed, and satisfied the gate before recording an observation.

The recorded authority is `SddGraph:pd-769e9114`. Execution pilots are
experimental evidence-gathering tooling, not a production runner API. There
is no production `sdd test` command or new admission by SDD-issued attempt.

## Graph contract

New content-bound test evidence uses `evidence: reported-v1`. The gate owns
the required identities and evidence requirements, **not** an executable,
argv, timeout, package selector, or environment overrides:

```json
{
  "type": "tests",
  "evidence": "reported-v1",
  "report": {
    "format": "go-test-json-v1",
    "runner": "repository-unit-tests",
    "environment_keys": ["BUILD_CONTEXT"],
    "test_support_inputs": [],
    "test_support_artifacts": []
  },
  "tests": [
    {"id": "TestRejectsDuplicateKey", "package": "example.test/codec", "file": "codec/parse_test.go"}
  ]
}
```

`report` is required on reported-v1 tests gates and forbidden elsewhere;
`execution` is forbidden on reported-v1. The initial strict native format is
Go test JSON; this is a decoder limitation, not an SDD-owned Go runner.
All three profile lists are required arrays; use `[]`, not omission or
`null`, when empty. Duplicate environment/support entries refuse.
Required package names are explicit graph declarations, never inferred by
executing Go or accepting the producer's selected-test list as authority.
Existing test-ID uniqueness rules still apply. Each selected source file
must be a declared artifact or whole-file repository input. Support lists
are subsets of the node's declared artifacts/input keys. Build manifests,
runner scripts, fixtures, and transitive inputs must be declared/reviewed as
appropriate; SDD does not claim to discover a complete read set.

All declared source artifacts must exist when context is captured. Author
the selected tests and minimum callable scaffolding before baseline RED;
an unimplemented behavior can fail an assertion, but a missing file or
unbuildable subject cannot supply qualifying RED. This does not authorize
implementing the intended behavior before testing it.

This first version binds regular files, not directory artifacts. That also
applies to artifacts inherited from dependencies: a legacy dependency with
a directory artifact needs a suitable explicit file declaration before a
reported node can use it. Known directory declarations refuse at compile
or amendment; context export also refuses unresolved directory shapes
rather than silently claiming a complete tree digest. This does not change
the legacy directory-digest contract.

`runner` is a non-secret logical repository tooling identity. It identifies
the required evidence profile, not a command SDD will launch. Environment
keys require opaque, nonempty producer-supplied identities; never serialize
secret values. The selected source/support identities and supplied runner
context participate in red compatibility. SDD does not probe the ambient
toolchain or reset a repository's environment.

## Producer interface

The agent invokes the repository's documented test tooling. That tooling
captures the native report and a separate UTF-8 JSON metadata document.
It may obtain before/after context with:

```text
sdd graph evidence-context --plan <Plan> --node <id> --by <holder> --json
```

This command is read-only. It returns the current plan/node, claim instance,
holder, opaque workspace identity, declared selected tests, a
`declared_hazards` map keyed by `package::test-id`, and candidate
content identities: normative obligation, artifacts, dependency artifacts,
declared inputs, cited intent, and selected test sources. It does not issue
a ticket, create a run ID, renew a lease, write a bundle, probe a runner, or
execute tests. Its context has no run-dependent timestamp. The wire shape
is also usable by repository tooling implementing the same documented
hashing contract; calling the export is a convenience, not proof of a run.

Preserve exported context objects unchanged, including empty maps/lists and
the opaque obligation fingerprint. File identities are SHA-256 of exact
bytes, encoded as `sha256:` plus lowercase hex. Input keys use the existing
graph `InputKey` encoding; selected-source keys use `package::test-id`.
The export is the supported way to obtain the graph-specific obligation
and cited-intent encodings without duplicating SDD internals.

Required metadata fields:

| Field | Contract |
|---|---|
| `protocol` | Exactly `reported-v1` |
| `before`, `after` | Complete context objects sampled immediately before and after repository-owned execution |
| `phase` | `red`, `green`, or `diagnostic`; intent never supplies the verdict |
| `red_kind`, `fault` | RED uses `baseline` with no fault, or `sensitivity` with a nonempty explanation. Omit both otherwise; the schema requires omission, and the decoder treats an explicitly empty string as omitted |
| `started`, `completed` | Ordered RFC3339 timestamps; no future completion |
| `runner.identity` | Matches the graph's logical `report.runner` |
| `runner.environment_identities` | Exactly the required environment keys with nonempty opaque identities |
| `execution.started`, `execution.completed`, `execution.report_complete` | All explicitly true for admissible evidence |
| `execution.exit_code` | Explicit nonnegative integer from the completed test tool, not a shell pipeline or model assertion |
| `report_digest` | `sha256:` plus lowercase hexadecimal SHA-256 of the exact native report bytes |

The producer must mark timeout, cancellation, capture overflow, failed
drain, interrupted execution, or operational failure incomplete. It must
not regenerate metadata later to relabel an old report with current source
hashes. No model-written outcomes or summaries can replace native output.
Unknown fields, duplicate JSON keys, trailing JSON, missing required facts,
unsupported versions, inconsistent metadata, and oversized input refuse.
Metadata reads are bounded to 4 MiB and native report reads to 64 MiB.

This is a local evidence protocol, not remote attestation. SDD can validate
the supplied facts, report consistency, and current content bindings; it
cannot authenticate a dishonest producer or detect every transient source
edit from two snapshots. Repository tooling and exclusive workspace
discipline are part of the trust boundary. Claim nonces isolate work
sessions, not authenticated users. Report files stay outside declared
source inputs and ordinarily in ignored, repository-owned output paths.

## Admission

```text
sdd graph sync --plan <Plan> --node <id> --by <holder> --report <native-report> --metadata <metadata.json>
```

Both files are mandatory for reported-v1. Metadata is refused on other gate
types/protocols; there is no fallback to a weaker report import. Missing
tool capability stops the walk instead of silently changing the gate.

SDD checks the following before any observation or red record is written:

1. The metadata version, shape, report digest, completed execution, logical
   runner context, phase, and chronology are valid.
2. Both contexts match each other and the current declared tests,
   obligation, source/input/intent identities, and live holder/claim/workspace.
   A replaced or expired claim refuses even if its workspace path is reused.
3. The raw report contains actual run and terminal events for every required
   package-qualified test. Missing or skipped selections/subcases, duplicate
   terminal results, incomplete packages, build/setup failure, and
   unaccounted package or unselected-test failures refuse. The process exit
   status must agree with the parsed result; it is not a verdict by itself.
4. GREEN requires all required tests passing. RED requires completed,
   accounted test failures, and arms red only for tests actually failing.
   A diagnostic report grants neither. Semantic cause/adequacy remains the
   assessment and review responsibility.
5. Hazard-discharging tests have an earlier admitted failure with matching
   test identity, hazards, selected test source, declared test-support
   inputs/artifacts, report requirements, and supplied runner context.
   Implementation-only changes do not invalidate red; changed tests or
   support do. Legacy or experimental red cannot silently qualify.
6. Passing evidence meets existing clean-workspace/isolation requirements.
   Commit or rebase of byte-identical content is not evidence invalidation.
7. Source/intent/test compatibility, claim and obligation, and passing
   workspace cleanliness are rechecked inside every graph-store CAS
   publication attempt. An unrelated graph write is not a reason to rerun
   tests; changed tested inputs are.

Observation hashes come from the validated execution contexts, never from
fresh sync-time hashes pasted onto an old report. A compact reported-evidence
summary and consumption/red links are recorded atomically with the
observation. Identity is derived from the exact report and metadata bytes,
not allocated by SDD. Replaying the same admitted pair is a historical
acknowledgement, never a new sequence, RED demotion, lease renewal, or
workspace release. Later state derivation needs no retained raw report;
missing or malformed recorded linkage cannot derive GREEN.

## Compatibility and experimental tooling

Existing legacy report imports retain their documented semantics and do
not acquire reported-v1 guarantees merely by parsing. Historical observed-v1
graphs/observations remain readable without rewriting their history; new
attempt-based admission is retired. Active experimental gates need an
explicit graph amendment to reported-v1 and fresh qualifying evidence;
there is no automatic downgrade or history conversion. Batch reverify and
repair-red cannot bypass reported-v1 metadata, claims, or compatible red.

Execution helpers live only in developer/experimental tooling, outside the
production binary dependency graph. The former execution-cost comparisons
remain historical experiment results, not justification for a runner in SDD.
Validation-only cost diagnostics may remain output-only; no optimization
is an acceptance criterion for this correction.

## Verification

Exercise a repository-owned runner against a real temporary repository:
author all selected tests, capture before context, run the repository tool,
capture after context and metadata, admit actual RED, implement, repeat for
GREEN, commit identical bytes, and sync. Confirm historical replay does not
change the graph. Include refusal tests for missing/renamed/skipped tests,
wrong package, build failure, partial output, tampered report/metadata,
changed source/intent/support, takeover/expiry, missing compatible red,
dirty GREEN, and concurrent publication changes. Prove the production CLI
has no `test` command and admission/context export launches no test runner
or toolchain probe. Run the repository's full `make test` gate and generated
plugin checks; focused success alone is insufficient.
