# Test Evidence Pilot

## What was built

The test-design, test-generate, and test-assess skills compose through the existing planning and implementation flow. Their common quality standard remains [TDD-TEST-DESIGN.md](TDD-TEST-DESIGN.md); portable plugins receive a generated copy, not another editable standard.

Opted-in `observed-v1` tests gates use binary-owned execution and evidence:

1. `sdd test run` captures the selected Go execution and its candidate, profile, claim instance, and raw output.
2. `sdd test check` checks a finalized attempt without changing graph state.
3. `sdd graph sync --attempt` admits compatible evidence; the graph still owns observations, red history, and closure.
4. `sdd test cleanup` explicitly removes eligible inactive attempt storage without deleting admitted history.

These capabilities are in the local source build. They do not upgrade an installed binary automatically. Existing legacy gates retain their import workflow; they do not acquire execution-bound provenance by being renamed.

## Reproduce the controlled run

Run from the repository root:

```bash
go test -count=1 ./cmd/sdd -run '^TestEvidencePilot' -v
```

The harness uses temporary, test-owned Go modules and Git repositories. It runs the public CLI command handlers and real child test processes. It never calls a remote model during the test suite: independently generated test fixtures are retained under `internal/testevidence/testdata/settings-pilot/` so the experiment is repeatable.

For the full repository gate on Windows, put the installed MinGW-w64 compiler on PATH and run:

```bash
CGO_ENABLED=1 CC=gcc CXX=g++ make test
```

No global environment configuration is required or changed. The pilot is a regular test, not an opt-in skip that could silently disappear from the gate.

## Workload and comparison

The fixture contract specifies a JSON settings parser, deterministic renderer, and real-file save/load boundary. Parser and renderer are independent graph slices; persistence consumes both. Both test sets target the same three public test families and the same contract:

- **Baseline:** independently generated from the contract and ordinary test-first instructions.
- **Composed:** generated using the design/assessment/generation skill sequence, then corrected after independent assessment.

The baseline was not intentionally weakened. The original composed persistence fixture is preserved under `composed/initial/`; its omission is an observed limitation of first-pass generation, not hidden by replacing the evidence.

Five defect variants were fixed before the initial comparison: duplicate-key acceptance, unsafe JSON escaping, reverse key ordering, ignored callback errors, and destructive replacement before the failure boundary. Independent assessment identified a missing composed test for `Load` rejecting content that the parser rejects. A sixth, explicitly labeled review-derived holdout bypasses the public parser during loading.

Every mutant must compile before a failing test can count as detection. A compilation/setup failure is not a detected behavioral defect. Mutant misses are recorded rather than suppressed or converted into a passing score.

## Observed results

The standalone controlled run on 2026-09-14 produced:

| Result | Baseline | Composed after assessment |
|---|---:|---:|
| Selected top-level test families | 3 | 3 |
| Concrete test identities in the complete green run, including parents/subtests | 25 | 22 |
| Families producing meaningful RED against scaffolds | 3 | 3 |
| Correct implementation passes | Yes | Yes |
| Initial defect variants detected | 5 / 5 | 5 / 5 |
| Review-derived loader holdout detected | Yes | Yes |

The preserved initial composed persistence fixture **missed** the loader holdout. The refined fixture detected it. This supports the need for assessment and a correction loop; it does not establish that a particular skill sequence always generates better tests.

The public graph workflow also completed three RED/GREEN work nodes, using eight actual attempts and four red admissions (one additional red sequence exercised claim replacement). It verified seven distinct refusals: foreign-node evidence, changed candidate, changed intent, replaced claim instance, raw-report import into an observed gate, an empty tests gate, and a live-graph input.

The run verified that:

- Replaying admitted RED after GREEN changes no graph sequence, claim, or latest observation.
- An unrelated observation and a byte-identical commit/rebase do not require another test execution for unchanged evidence.
- Review and acceptance closure are derived before intentionally changing dependency bytes.
- A subsequent dependency-content change makes the consumer stale.

The four-lane records in the automated fixture are explicitly **synthetic protocol records**, not independent model reviews. They test the existing review storage/admission path. Generated-test quality was assessed separately; fixture records alone make no claim about that quality.

## Reading the measurements

The test emits `PILOT_VARIANT_METRICS` and `PILOT_METRICS` JSON. Counts distinguish selected identities, concrete test identities, repeated concrete executions, targeted test runs, compile-only checks, broad runs, refusals, and defect detections. The fixed mutant inventory is checked so adding a file cannot silently change the reported denominator.

Timing is measured for each run. Variant process timing is wall time around child test invocations. Graph process timing combines captured attempt process durations with the measured acceptance invocation; remaining graph pipeline time includes Git, CLI checks, source/profile probes, and harness overhead. It is not an attribution of all overhead to any one component.

This is one small controlled workload, not a randomized or statistical study. Fewer cases and small timing differences are not superiority claims. Repeated runs, more workloads, and more precise cost attribution are needed before optimizing scheduling, integration, or evidence reuse.

## Boundaries

- The observed adapter initially targets ordinary packages in one Go module, with explicit supported flags and declared inputs.
- Evidence validity is content/profile/claim-bound, not a proof that the assertions are meaningful. Semantic assessment and existing review remain necessary.
- Whole-file test/support identity is deliberately conservative; a changed test file can require replacement red evidence.
- No branch-integration redesign, verification cache, general framework adapter, or release/version publication is part of this pilot.
