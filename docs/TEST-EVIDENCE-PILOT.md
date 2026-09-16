# Test Evidence Pilot

## Historical experiment

The test-design, test-generate, and test-assess skills compose through the existing planning and implementation flow. Their common quality standard remains [TDD-TEST-DESIGN.md](TDD-TEST-DESIGN.md); portable plugins receive a generated copy, not another editable standard.

The 2026-09-14 pilot evaluated an experimental, binary-owned `observed-v1` execution path:

1. `sdd test run` captures the selected Go execution and its candidate, profile, claim instance, and raw output.
2. `sdd test check` checks a finalized attempt without changing graph state.
3. `sdd graph sync --attempt` admits compatible evidence; the graph still owns observations, red history, and closure.
4. `sdd test cleanup` explicitly removes eligible inactive attempt storage without deleting admitted history.

Decision `SddGraph:pd-d72f3bca` retired both this pilot path and its successor `reported-v1`. These commands, protocols, attempt bundles, context exports, and metadata are historical only and no longer exist in the production interface. Production SDD owns graph requirements and evidence validation, not test execution: repository tooling supplies untouched native reports to `sdd graph sync --report`, with optional `--report-exit`. There is no production `sdd test`.

The retired ownership exercise was named `TestReportedEvidencePilot`; consult Git history for its source and exact run contract. Its removal is intentional, so this document provides no runnable instructions for the retired interface. The independently generated settings fixtures remain under `internal/testevidence/testdata/settings-pilot/`. No test called a remote model.

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

The experimental graph workflow also completed three RED/GREEN work nodes, using eight actual attempts and four red admissions (one additional red sequence exercised claim replacement). It verified seven distinct refusals under that former protocol: foreign-node evidence, changed candidate, changed intent, replaced claim instance, raw-report import into an observed gate, an empty tests gate, and a live-graph input. These are retained measurements only; neither retired protocol is current evidence guidance.

The run verified that:

- Replaying admitted RED after GREEN changes no graph sequence, claim, or latest observation.
- Under that retired protocol, unrelated observations and byte-identical rewrites did not require another execution.
- Review and acceptance closure were derived before intentionally changing dependency bytes.
- Under that retired model, a subsequent dependency-content change made the consumer stale. Current state instead changes when a dependency is deliberately re-verified at a higher sequence or its `contract_rev` advances.

The four-lane records in the automated fixture are explicitly **synthetic protocol records**, not independent model reviews. They test the existing review storage/admission path. Generated-test quality was assessed separately; fixture records alone make no claim about that quality.

## Reading the measurements

The historical test emitted `PILOT_VARIANT_METRICS` and `PILOT_METRICS` JSON. Counts distinguished selected identities, concrete test identities, repeated concrete executions, targeted test runs, compile-only checks, broad runs, refusals, and defect detections. Its fixed mutant inventory prevented adding a file from silently changing the reported denominator. The current ownership exercise does not reproduce those measurement claims.

Timing is measured for each run. Variant process timing is wall time around child test invocations. Graph process timing combines captured attempt process durations with the measured acceptance invocation; remaining graph pipeline time includes Git, CLI checks, source/profile probes, and harness overhead. It is not an attribution of all overhead to any one component.

This is one small controlled workload, not a randomized or statistical study. Fewer cases and small timing differences are not superiority claims. Repeated runs, more workloads, and more precise cost attribution are needed before optimizing scheduling, integration, or evidence reuse.

## Boundaries

- The former observed adapter targeted ordinary packages in one Go module, with explicit supported flags and declared inputs; it remains experimental evidence-gathering history, not production execution guidance.
- The retired evidence binding did not prove that assertions were meaningful. Semantic assessment and review remain necessary.
- Whole-file test/support identity was a conservative property of the retired experiment, not current freshness guidance.
- No branch-integration redesign, verification cache, general framework adapter, or release/version publication is part of this pilot.
