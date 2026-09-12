---
name: sdd-decision-log
description: "Recording user decisions as durable truth in a plan's decisions file. Load whenever the user makes a design or architecture choice, defines a project concept or term, answers a design question, reverses an earlier decision, or when current work touches a topic a plan's decisions file may already govern — including plain conversation outside any sdd-planner skill."
disable-model-invocation: true
---

# Decision Log — Ad Hoc Capture

The full convention (entry schema, write protocol, cross-plan citation, conflicts) lives in `shared/decision-log.md` in the plugin directory — read it before your first decisions-file write of a session. This skill exists so decision moments *outside* the lifecycle skills still get recorded.

## When the user just decided something

1. Recognize the moment: a stated choice between alternatives, a definition of a project term, an answer to a design question, or an explicit reversal that will bind work in the plan going forward.
2. Write the statement you propose to record and **show it to the user verbatim**. If the user amends it, show the amended text.
3. Once the user approves the exact text, run `sdd decide add --plan <Name> --statement "..."` (with `--supersedes`/`--source` if applicable). The tool prints the entry. Done — there is no draft state and no accept step.

A statement the user did not approve is never passed to the tool.

## When about to act on a topic a plan's decisions may govern

Before drafting or implementing in an area a plan's decisions file may cover, run `sdd decide current --plan <Name>` (or without `--plan` for every plan under the root). Standing decisions are constraints: if the current ask contradicts one, surface the collision instead of silently following either side — the resolution is a fresh, user-approved entry with `--supersedes` naming the old one.
