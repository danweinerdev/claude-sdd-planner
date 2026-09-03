## Planning

Planning artifacts live at the planning root defined by `planning-config.json` (`planningRoot`, here `{{PLANNING_ROOT}}/`) — managed by the `sdd-planner` Claude Code plugin. Artifact directories: `Research/`, `Brainstorm/`, `Specs/<feature>/`, `Designs/<component>/`, `Plans/<PlanName>/`, `Decisions/`.

### Planning Skills
| Skill | Purpose |
|-------|---------|
| `/sdd-planner:research` | Investigate a topic → `Research/<topic>.md` |
| `/sdd-planner:brainstorm` | Explore possibilities → `Brainstorm/<topic>.md` |
| `/sdd-planner:specify` | Write requirements → `Specs/<feature>/README.md` |
| `/sdd-planner:design` | Technical architecture → `Designs/<component>/README.md` |
| `/sdd-planner:plan` | Decompose work into an executable plan graph → `Plans/<Name>/` + `<Name>-Graph.json` (v1 plans keep the old protocol until converted) |
| `/sdd-planner:implement` | Walk the plan graph — claim → red → green → sync → merge, observation-gated |
| `/sdd-planner:code-review` | Review code against the plan — drift, gaps, blind spots |
| `/sdd-planner:debrief` | After-action notes for completed phases |
| `/sdd-planner:decide` | Record, look up, audit, or reconcile decided truths → `Decisions/decisions.md` |
| `/sdd-planner:poke-holes` | Adversarial critical analysis of any artifact |
| `/sdd-planner:validate` | Deterministic + semantic validation of artifacts, evidence, and ledger (read-only) |
| `/sdd-planner:setup` | Set up a repo — generates planning-config.json, bootstraps directories |

Typical lifecycle: `setup → research → brainstorm → specify → design → plan → implement → code-review → debrief` (all `/sdd-planner:*`).

**Commit cadence (D-0024):** planning artifacts are written in flow and committed only at boundaries — one commit at phase open, one at phase close carrying every status, evidence, amendment, decision, review, and debrief, and one at the end of a spec/design/plan session. Never commit per task, per amendment, or per decision; implementation commits carry no planning bytes.
