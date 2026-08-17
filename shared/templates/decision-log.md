---
title: "Decision Ledger"
type: decision-log
status: active
created: {{DATE}}
updated: {{DATE}}
tags: [decisions]
related: []
# Accepted exceptions. Only DLG064/DLG065 (id-sequence gaps and ordering
# inherited from before sequencing was enforced) may be waived, each with a
# reason stating why — see shared/decision-log.md § Accepted exceptions.
# Adding one is a ledger write: it needs the user's explicit approval.
waivers: []
decisions: []
---

# Decision Ledger

Machine-readable record of decided truths that outlive the document they were made in — design choices, concept definitions, and answered design questions that constrain work elsewhere. Choices a spec, design, or plan already states in full stay in that artifact. The frontmatter `decisions[]` array is canonical; see `shared/decision-log.md` in the plugin for the admission test, entry schema, lifecycle rules, and collision procedure.

Entries are append-only: an accepted entry is never edited except to mark it superseded. A change of mind is a new entry that supersedes the old one.

<!-- Optional extended context per entry:

## D-0001 — Short Title

Options considered, links, deeper deliberation. The frontmatter entry remains canonical.
-->
