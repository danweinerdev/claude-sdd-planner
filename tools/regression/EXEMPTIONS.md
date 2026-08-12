# Frozen-coverage exemptions

`tools/parity/frozen-expectations.json` records the Python oracle's last
verdict for all 128 fixture roots — 705 diagnostics spanning 124 of the 126
registered SDD codes and 24 DLG codes. This file records the two SDD codes
with no frozen output, and why, so the gap is a stated exemption rather than
an unnoticed hole.

Both are structural: the example harness cannot construct their triggering
condition. Neither is a rule that went untested — each is covered by a direct
unit test instead.

| Code | Why no frozen output | Covered by |
|------|----------------------|------------|
| `SDD000` | Fires only when `planning-config.json` maps a plan to a repository that cannot be resolved. The fixture root exercises the surrounding path, but both validators agree the mapping resolves, so neither emits a diagnostic — there is nothing to freeze. Reproducing a genuinely unresolvable mapping needs machine-specific absolute paths, which a committed fixture must not carry. | `internal/rules` registry examples |
| `SDD001` | Fires only when the planning root is not a directory. The harness always materializes a real directory to hold an example's `Files`, so the condition is unreachable there — this is the rule carrying `UnexampledReason`. | `TestPlanningRootMustBeADirectory` |

## What this file is not

It is not an allowlist. `tools/parity/allow-missing.txt` and
`allow-message-drift.txt` tell the gate what to tolerate at comparison time;
both are currently empty of SDD and DLG codes. This file explains why two
codes never reach the comparison at all.

## Maintenance

If a future change makes either condition constructible as a fixture, add the
fixture and delete its row. Do **not** regenerate `frozen-expectations.json`
to add it: that file is the deleted Python validator's last word, and
rewriting it would change what "correct" means, which is precisely what
freezing exists to prevent.
