---
title: "Provisioning Vertical"
type: phase
plan: "SDD-Toolchain"
phase: 2
status: planned
created: 2026-08-03
updated: 2026-08-03
deliverable: "A real `sdd` binary users install with go install, discovered by skills and hooks, with /setup reworked to verify rather than build"
tasks:
  - id: "2.1"
    title: "Version single source and make bump wiring"
    status: planned
    verification: "`sdd version --json` reports correctly from both `go build` and `go install` builds; `make bump-patch` advances version and the generated file in one commit and tag while leaving minSddVersion untouched; `make test` fails on a stale generated file and when minSddVersion exceeds version"
    justifies: "FR-38, FR-39, AC-02, AC-32. Without a version the binary reports under `go install`, the FR-38 floor check cannot function at all, and the floor is the only thing preventing a wrong-schema binary from being used silently."
  - id: "2.2"
    title: "Executable resolution with caching and floor admission"
    status: planned
    verification: "AC-01 and AC-45 pass: ordering, floor rejection at and below the boundary, single cached probe per session, bounded timeout against a hanging fake `sdd`, and no PATH probe while the plugin-root location resolves"
    justifies: "FR-05, FR-49, FR-38, AC-01, AC-35, AC-45. This is the mechanism every skill and both hooks depend on, and finding F-09 established that an uncached resolver spawning probes per call defeats NFR-07 outright."
    depends_on: ["2.1"]
  - id: "2.3"
    title: "sdd doctor"
    status: planned
    verification: "AC-34 and AC-41 pass: every FR-05 candidate reported with presence, version, and admitted-or-why-rejected; the hook static path reported as its own item; a deleted plugin-root copy reported as a named defect while PATH still resolves"
    justifies: "FR-42, AC-34, AC-41. Finding F-02 showed a fully dead hook layer can coexist with a healthy candidate list, so reporting the hook path separately is the only thing that makes that state visible."
    depends_on: ["2.2"]
  - id: "2.4"
    title: "Hooks moved into the binary"
    status: planned
    verification: "AC-23 and AC-25 pass: bash-guard parity fixtures byte-comparable to reviewer-bash-guard.py across the allowlist, denials, test/lint passthrough, the seven agents, and fail-open for everyone else; sessionstart emits the same context as load-decisions.sh; both hooks.json entries registered with the absent one failing open"
    justifies: "FR-27, D-0013, D-0014, AC-23, AC-25. Removes the Python and POSIX-shell runtime dependencies and fixes an existing defect — load-decisions.sh has never worked on native Windows."
    depends_on: ["2.2"]
  - id: "2.5"
    title: "sdd allowlist in the reviewer guard"
    status: planned
    verification: "AC-39 passes: for each of the seven read-only agents every read-only sdd subcommand is permitted and every mutating one denied, proven per subcommand; a test enumerates the binary's actual mutating subcommands and fails when one lacks a guard entry"
    justifies: "FR-44, D-0014, AC-39. Prevents the concrete regression that introducing a mutating CLI would otherwise cause: the guard permits unrecognized command heads, so the read-only agents would gain a sanctioned path to rewrite planning artifacts."
    depends_on: ["2.4"]
  - id: "2.6"
    title: "/setup reworked to verify and copy"
    status: planned
    verification: "AC-33, AC-38, AC-41 pass: an admitted PATH binary yields a working plugin-root copy; absent and below-floor each stop with distinguishable actionable messages; a filesystem snapshot proves no mutation on the stop paths; a second run is a no-op"
    justifies: "FR-37, FR-40, FR-43, D-0015, AC-33, AC-38. Finding F-02 proved that conditional placement leaves both hooks silently dead forever, so unconditional placement is what makes the failure unreachable rather than merely unlikely."
    depends_on: ["2.3", "2.5"]
---

# Phase 2: Provisioning Vertical

## Overview
The shippable skeleton: a binary that does nothing but report its version and
diagnose its own installation, plus every mechanism around it. Deliberately
separated from validation logic so the distribution story is proven end to end
before any rule is ported. Nothing here depends on the FR-30 parity gate.

## 2.1: Version single source and make bump wiring

### Subtasks
- [ ] Add `minSddVersion` to `.claude-plugin/plugin.json`
- [ ] Extend `bump-version.py` to write the generated version source; wire into all three bump targets
- [ ] Add the drift check and the floor-exceeds-version check to `make test`

### Notes
Revision boundary: The binary reports a correct version under every build path, and release tooling keeps it in lockstep.

### Trap
Injecting the version with `-ldflags`. It is the obvious approach and it silently fails under `go install`, which FR-41 makes the only provisioning path — so the floor check would be dead in every real installation.

### Completion Evidence

Pending — not complete.

## 2.2: Executable resolution with caching and floor admission

### Subtasks
- [ ] Implement the two-candidate ordered resolution with floor admission
- [ ] Cache the resolution per session and expose it for reporting
- [ ] Probe PATH candidates with a fixed argument-free invocation and bounded timeout
- [ ] Document the algorithm once in `shared/path-resolution.md` so no skill re-derives it

### Notes
Revision boundary: Any caller can resolve an admitted binary once, cheaply, with rejection reasons available.

### Completion Evidence

Pending — not complete.

## 2.3: sdd doctor

### Subtasks
- [ ] Report each candidate path with presence, version, and admission verdict
- [ ] Report the hook static path as a distinct checked item naming `/sdd-planner:setup` when absent
- [ ] Add `--json` output

### Notes
Revision boundary: A user can diagnose any provisioning state from one command.

### Completion Evidence

Pending — not complete.

## 2.4: Hooks moved into the binary

### Subtasks
- [ ] Freeze parity fixtures for reviewer-bash-guard.py before porting any logic
- [ ] Implement `sdd hook pretooluse` preserving the git/p4 allowlist and denial behavior exactly
- [ ] Implement `sdd hook sessionstart` emitting accepted ledger entries
- [ ] Register both platform entries in hooks.json; delete the Python and shell hooks

### Notes
Revision boundary: Both plugin hooks run from the binary, with the old behavior proven preserved.

### Completion Evidence

Pending — not complete.

## 2.5: sdd allowlist in the reviewer guard

### Subtasks
- [ ] Add the read-only sdd subcommand allowlist to the guard
- [ ] Deny every mutating subcommand for the seven agents
- [ ] Add the enumeration test that fails when a new mutating subcommand lacks a guard entry

### Notes
Revision boundary: The read-only agents can query through sdd but cannot mutate through it.

### Completion Evidence

Pending — not complete.

## 2.6: /setup reworked to verify and copy

### Subtasks
- [ ] Move the provisioning check ahead of all filesystem mutation
- [ ] Place or refresh the plugin-root copy on every run along every non-stopping branch
- [ ] Verify through the plugin-root path via `sdd version` and `sdd doctor`
- [ ] Emit distinguishable not-found and below-floor stop messages with the exact go install command
- [ ] Update the setup skill, README, and CLAUDE files

### Notes
Revision boundary: A user can go install, run /setup, and have working skills and hooks — or be told precisely what to fix.

### Completion Evidence

Pending — not complete.

## Acceptance Criteria
- [ ] A user with a Go toolchain can `go install`, run `/setup`, and both hooks execute.
- [ ] Every provisioning failure state is distinguishable, actionable, and mutates nothing.
- [ ] `sdd doctor` makes the dead-hook state from finding F-02 visible.
- [ ] The Python and shell hook files are deleted, with the bash guard proven byte-comparable on parity fixtures.
- [ ] Reviewer agents can run read-only sdd subcommands and are denied every mutating one.

## Phase Completion Evidence

Pending — not complete.
