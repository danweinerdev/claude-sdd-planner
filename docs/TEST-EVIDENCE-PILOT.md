# Test Evidence Pilot

## Historical experiment

The test-design, test-generate, and test-assess skills compose through the existing planning and implementation flow. Their common quality standard remains [TDD-TEST-DESIGN.md](TDD-TEST-DESIGN.md); portable plugins receive a generated copy, not another editable standard.

The 2026-09-14 pilot evaluated an experimental, binary-owned `observed-v1` execution path:

1. `sdd test run` captures the selected Go execution and its candidate, profile, claim instance, and raw output.
2. `sdd test check` checks a finalized attempt without changing graph state.
3. `sdd graph sync --attempt` admits compatible evidence; the graph still owns observations, red history, and closure.
4. `sdd test cleanup` explicitly removes eligible inactive attempt storage without deleting admitted history.

These commands and attempt bundles describe the former experiment, not the production interface. Production SDD owns graph requirements and evidence validation, not test execution: repository tooling now produces native output plus strict metadata for `reported-v1`, and `sdd graph sync --report ... --metadata ...` validates it. There is no production `sdd test`, hidden execution flag, or new attempt admission. Existing `observed-v1` history remains readable; active gates require explicit amendment and fresh evidence. Existing legacy imports retain their actual semantics and do not acquire stronger provenance by being renamed.

## Exercise the corrected ownership flow

Run from the repository root:

```bash
go test -count=1 ./cmd/sdd -run '^TestReportedEvidencePilot$' -v
```

This current test uses a temporary Git repository and a repository-owned
producer that runs real Go tests. It captures before/after context, supplies
native output and metadata to SDD, verifies behavioral RED then GREEN,
fast-forwards the tested implementation, and checks historical replay does
not mutate the graph. It also refuses a real build failure without arming
RED and poisons the Go executable on the SDD context/admission path to catch
unintended execution or toolchain probes. This is an ownership/validation
exercise, not a new generated-test quality comparison or performance study.

The former `TestEvidencePilot` sources are archived under
`tools/experimental/owned-evidence/historical/`; they are not registered in
the current CLI suite. Reproducing that older controlled comparison needs
its historical source/binary contract, not the removed production commands.
The independently generated settings fixtures remain under
`internal/testevidence/testdata/settings-pilot/`. No test calls a remote model.

For the full repository gate on Windows, put the installed MinGW-w64 compiler on PATH and run:

```bash
CGO_ENABLED=1 CC=gcc CXX=g++ make test
```

No global environment configuration is required or changed. At the time of measurement, the pilot was a regular test rather than an opt-in skip. That records how the experiment was gathered; it does not make its runner API a production path.

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

The experimental graph workflow also completed three RED/GREEN work nodes, using eight actual attempts and four red admissions (one additional red sequence exercised claim replacement). It verified seven distinct refusals under that former protocol: foreign-node evidence, changed candidate, changed intent, replaced claim instance, raw-report import into an observed gate, an empty tests gate, and a live-graph input. These are retained measurements, not claims that old reports satisfy `reported-v1`.

The run verified that:

- Replaying admitted RED after GREEN changes no graph sequence, claim, or latest observation.
- An unrelated observation and a byte-identical commit/rebase do not require another test execution for unchanged evidence.
- Review and acceptance closure are derived before intentionally changing dependency bytes.
- A subsequent dependency-content change makes the consumer stale.

The four-lane records in the automated fixture are explicitly **synthetic protocol records**, not independent model reviews. They test the existing review storage/admission path. Generated-test quality was assessed separately; fixture records alone make no claim about that quality.

## Reading the measurements

The historical test emitted `PILOT_VARIANT_METRICS` and `PILOT_METRICS` JSON. Counts distinguished selected identities, concrete test identities, repeated concrete executions, targeted test runs, compile-only checks, broad runs, refusals, and defect detections. Its fixed mutant inventory prevented adding a file from silently changing the reported denominator. The current ownership exercise does not reproduce those measurement claims.

Timing is measured for each run. Variant process timing is wall time around child test invocations. Graph process timing combines captured attempt process durations with the measured acceptance invocation; remaining graph pipeline time includes Git, CLI checks, source/profile probes, and harness overhead. It is not an attribution of all overhead to any one component.

This is one small controlled workload, not a randomized or statistical study. Fewer cases and small timing differences are not superiority claims. Repeated runs, more workloads, and more precise cost attribution are needed before optimizing scheduling, integration, or evidence reuse.

## Boundaries

- The former observed adapter targeted ordinary packages in one Go module, with explicit supported flags and declared inputs; it remains experimental evidence-gathering history, not production execution guidance.
- Evidence validity is content/profile/claim-bound, not a proof that the assertions are meaningful. Semantic assessment and existing review remain necessary.
- Whole-file test/support identity is deliberately conservative; a changed test file can require replacement red evidence.
- No branch-integration redesign, verification cache, general framework adapter, or release/version publication is part of this pilot.
