# VCS Detection

How to determine the version-control system rooted at a given directory, and which command to use for common file/history operations once you know.

This file is a shared spec — `setup` and any skill or agent that inspects files or history reads it before reaching for `git`. Don't hard-code `git` in skills; check the VCS first.

## Result labels

A detection produces one of these labels:

| Label | Meaning |
|---|---|
| `git` | Normal git working tree (`.git/` is a directory). |
| `git-worktree` | A linked git worktree (`.git` is a file pointing into a main repo's `worktrees/`). |
| `git-bare` | A bare git repository (`.bare/` directory present, or `git rev-parse --is-bare-repository` returns `true`). Bare repos have no working tree — most operations should run inside individual worktrees instead. |
| `perforce` | A Perforce client workspace mapped to this directory (`p4 info` succeeds and `p4 where` resolves the directory). |
| `none` | No VCS detected. Valid for an empty directory or a freshly created project the user hasn't yet initialized. |

## Detection algorithm

Run these checks in order against the target directory and return the first match. Keep stderr suppressed — failures are expected:

1. `[ -d "<dir>/.bare" ]` → `git-bare`
2. `[ -f "<dir>/.git" ]` → `git-worktree`
3. `[ -d "<dir>/.git" ]` → `git`
4. `git -C "<dir>" rev-parse --is-bare-repository 2>/dev/null` returns `true` → `git-bare`
5. `git -C "<dir>" rev-parse --git-dir 2>/dev/null` succeeds → `git` (covers edge cases where git considers the directory part of a repo via env vars or parent search)
6. `p4 -d "<dir>" info 2>/dev/null` exits 0 **and** `p4 -d "<dir>" where //... 2>/dev/null` produces at least one line → `perforce`
7. otherwise → `none`

The result is *not* cached on disk. Detection is cheap — call it at the start of any skill that needs it.

## VCS-aware operations

When a skill needs to inspect or change tracked files, choose the column for the detected VCS:

| Operation | `git` / `git-worktree` | `perforce` | `none` |
|---|---|---|---|
| List tracked files | `git ls-files` | `p4 files //... 2>/dev/null` | `find . -type f -not -path './.*'` |
| Working-tree status | `git status --short` | `p4 opened` (open files) + `p4 status` if available | (no concept — describe the filesystem state directly) |
| Move / rename a tracked file | `git mv <src> <dst>` | `p4 move <src> <dst>` | `mv <src> <dst>` |
| Recent history (orient) | `git log --oneline -20` | `p4 changes -m 20` | (skip — no history) |
| File history | `git log -p <file>` | `p4 filelog <file>` then `p4 print -q <file>#<rev>` per revision | (skip) |
| Diff of staged | `git diff --cached` | `p4 diff -dw <files>` (unsubmitted) | (skip) |
| Diff base..head | `git diff <base>..<head>` | `p4 diff2 -dw //path/...@<base> //path/...@<head>` | (skip) |
| Ignore file written by setup | `.gitignore` | `.p4ignore` | (skip — no ignore file) |

## Git integration of parallel graph nodes — linear history

For `git` / `git-worktree`, integrate worktree or topic-branch work from parallel
graph nodes by **rebasing the node branch onto the latest primary branch, then
fast-forwarding the primary branch**. Do not create merge commits. Run
`git rebase <primary-branch>` from the node branch and resolve conflicts
deliberately. On the primary branch, use `git merge --ff-only <node-branch>`;
this advances the branch without creating a merge commit.

**A rebase does not by itself cost verification.** Graph proof is anchored to
artifact digests; the commit id is supplementary provenance. A node whose
files come through the rebase byte-identical stays GREEN. Only a node whose
files changed in the rebase (conflict resolution) derives STALE by digest, and
only that node re-runs its gate and syncs a fresh observation. Check with
`sdd graph status` after the fast-forward; never re-verify on the commit id
alone. The rewritten commit ids are recorded as lineage (below) so provenance
stays followable.

Serialize integrations: after each fast-forward, rebase the next node branch
onto the updated primary branch. If the fast-forward refuses because primary
advanced, rebase again — never fall back to a merge commit. Keep per-node
commit boundaries, never rebase the primary branch itself, and do not
force-push shared history without explicit approval. The graph's logical
sync/merge (claim completion) is distinct from this Git integration step.

### Capturing and recording rewritten revisions

Run `sdd doctor` in the target Git worktree before rebasing. It installs or
repairs the managed `post-rewrite` dispatcher, honors `core.hooksPath`, restores
executable permissions, and preserves an existing user hook as
`post-rewrite.sdd-user` with the same input, arguments, and exit status. It never
changes Git configuration. Unsafe symlinks or backup collisions are reported,
not overwritten. `sdd doctor --check` diagnoses without writes: repairable Git
hook findings exit 1; operational/unsafe failures exit 2. Plain directories have
no Git hook to install. This Git hook is runtime-neutral, distinct from Claude
Code's plugin `hooks.json`.

The dispatcher invokes `sdd hook post-rewrite <rebase|amend>` using the
user-installed binary on `PATH`. It captures Git's native two-column
`OLD_COMMIT NEW_COMMIT` stream and prints the saved `.map` path. Maps live under
the current worktree's Git-private `sdd/rewrite-maps` directory, not in the plan
or a global decision log. Capture never mutates the graph. Preserve/import a map
before releasing its worktree: removing a linked worktree may remove its private
Git metadata. Capture retains squash/fixup rows, but remapping below deliberately
refuses many-to-one mappings rather than silently combining graph-node identities.

For already-recorded revisions, inspect the map and preview the lineage update:

```bash
sdd graph remap-revisions --plan Feature --map <captured-map> --dry-run --json
sdd graph remap-revisions --plan Feature --map <captured-map> --expect-digest <preview-graph-digest>
sdd graph show <node-id> --plan Feature --json
```

`remap-revisions` accepts full 40-hex commit IDs available in the plan's target
Git repository. It appends only mappings connected to recorded Git observations
or existing lineage, reports unreferenced rows, and refuses malformed, cyclic,
conflicting, or many-to-one mappings. Identical mappings are idempotent; the graph
digest fence refuses concurrent edits. Capture can also retain Git SHA-256 IDs,
but remapping currently follows the adapter's SHA-1-only revision contract.
Dropped/skipped commits cannot be inferred from a missing row, and unavailable
Git objects require recovery before remapping; the tool neither fetches nor
creates retention refs automatically.

Lineage records identity, **not proof**: `revision_lineage` preserves the old→new
chain while original observations, their provenance, `contract_rev`, sequence
numbers, and frozen reviews stay unchanged. `graph show` distinguishes recorded
and rewritten revisions. Remapping neither grants nor withdraws GREEN: a node
whose artifact digests still match its observation stays current across the
rewrite, and a node whose digests changed re-verifies through `sync` (or the
applicable `reverify` batch) regardless of lineage. If the first passing
observation is recorded only after rebase, it already names the new commit and
may need no lineage remap.

### Reclaiming worktrees and branches

Every worktree and branch created for a slice is temporary and is removed as soon
as its use is complete: a node workspace once its green sync has been
fast-forwarded into the primary branch, a scratch worktree for a parallel
builder or a review lane once its commits are fast-forwarded or discarded, and
an abandoned claim as soon as it is released. Never let them accumulate across
nodes or rounds. On Git targets:

- **Node workspaces**: after `git merge --ff-only`, `git branch -d graph/<id>-<suffix>`
  and `sdd graph gc --plan <Name>`; `sdd graph release` reaps an idle workspace and
  its branch itself. When a sandbox mount keeps `git worktree remove` from
  deleting the directory, run `git worktree remove --force` followed by
  `git worktree prune` outside the sandbox rather than leaving the entry behind.
- **Scratch worktrees** (anything not tracked by the graph): `git worktree remove
  --force <path>`, `git worktree prune`, then `git branch -d <branch>` once the
  fast-forward is in place. Preserve the rewrite map first (above) if the tree
  was rebased.
- **Check before ending a session**: `git worktree list` shows only the primary
  checkout and `git branch --list 'graph/*'` is empty. A stray entry is a defect
  in the walk, not housekeeping for later.

## Special cases

- **`git-bare`**: stop and tell the user to operate in a worktree instead. Most skills can't do meaningful work in a bare repo (no checked-out files).
- **`none`**: history-dependent operations (recent commits, file history, diff between revisions) are unavailable. Skills should skip those steps gracefully and note the limitation in any report rather than failing.
- **`perforce`** when `p4 info` succeeds but `p4 where` fails: the user has the `p4` client installed and authenticated, but the target directory isn't inside a workspace mapping. Treat as `none` for this directory.

## How skills should use this

A skill that needs VCS-aware behavior typically does:

1. Run the detection algorithm against the relevant directory (the planning root, the target code repo, or both).
2. If the result is `git-bare`, stop with the standard message.
3. Look up the operation it needs in the table above and use the matching command.
4. If the result is `none` and the operation has no row for `none`, skip that operation and note it in the output.

Skills should never assume git silently. The whole point of this file is that "what VCS is this?" has one canonical answer everyone agrees on.
