---
title: "SDD Reframing"
type: plan
status: draft
created: "2026-09-13"
updated: "2026-09-13"
tags: [graph, decisions, review, execution, sdd-cli]
related: [Designs/PlanDecisions, Designs/ReviewDrivenAmendment, Designs/GitRevisionLineage, Designs/VerificationFreshness]
phases: []
waivers: []
---

# SDD Reframing

## Overview

The record-only plan for the reframing built on branch `sdd-design`: the
graph as sole execution authority, per-plan decision records replacing the
global ledger, review findings applied as revise/extend amendments, Git
revision lineage separate from proof, and staleness keyed on what a proof
consumed. The work itself was built as ordinary commits on the branch, not
as graph nodes, because it changes the review and amendment machinery a
graph plan would have depended on. This plan exists so the four designs'
decisions are compiled into one decisions file (`SddReframing-Decisions.json`)
and become citable `pd-` ids, and so their declared supersessions of earlier
decisions (`SddGraph:DD-9`, this plan's own DD-8) are recorded.

## Non-Goals

- No graph nodes and no phases: nothing here is executed through `/implement`.
- No re-recording of the two decisions already appended by hand during the
  adversarial-review reconciliation; those stay in the plans that carry them.

## Architecture

See the related designs. Summary of what shipped:

- `Designs/PlanDecisions`: `Plans/<Name>/<Name>-Decisions.json`, `sdd decide add|list|current|lookup|render|sync`, rules SDD190–SDD193.
- `Designs/ReviewDrivenAmendment`: review-role nodes, `contract_rev`, reviewed sets, `sdd graph review` previews and `sdd graph amend`, the `Amend` verdict.
- `Designs/GitRevisionLineage`: rebase-then-fast-forward integration, `post-rewrite` capture via `sdd doctor`, `sdd graph remap-revisions`.
- `Designs/VerificationFreshness`: dependency digests on observations, run-time anchor snapshots, `sdd graph acknowledge`, provable input narrowing, per-axis stale reasons.

## Key Decisions

Compiled from the related designs into `SddReframing-Decisions.json` by
`sdd decide sync --plan SddReframing`; read them with
`sdd decide list --plan SddReframing` or the rendered `Design.md`.

## Dependencies

`Designs/SddGraph` for the graph model this reframing extends; its own
decisions are compiled into `Plans/SddGraph` so the supersession recorded
here resolves.

## Plan Completion Evidence

Pending — not complete. This plan carries decisions, not work; it closes
when the branch merges.

## Open Questions

- **Should this plan be marked complete at merge?** **non-blocking** — it
  has no nodes; completion is a bookkeeping status only.
