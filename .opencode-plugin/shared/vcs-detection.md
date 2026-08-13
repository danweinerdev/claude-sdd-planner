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
