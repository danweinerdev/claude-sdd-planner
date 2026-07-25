#!/usr/bin/env python3
"""PreToolUse guard: keep sdd-planner's read-only agents actually read-only.

The plugin's reviewer/researcher agents carry a behavioral "you are read-only"
guarantee, but Bash is a residual write channel. This hook turns that prompt-
level guarantee into a permission-level one: when one of those agents calls
Bash, the command is screened and write-shaped or network-shaped invocations
are denied with an explanation the agent can act on.

Policy (defense-in-depth against sloppiness, not adversarial sandboxing):
- Applies ONLY to the agents in READ_ONLY_AGENTS; every other Bash call in the
  session is allowed untouched (fail-open on missing/unparsable agent fields).
- `git` and `p4` segments are checked against a read-only subcommand allowlist
  (diff/log/show/blame/... yes; commit/push/checkout/reset/... no).
- Other segments are denied when they start with a mutating or network command
  (rm, mv, sed -i, curl, package-manager installs, interpreter eval flags, ...)
  or redirect output anywhere but /dev/null.
- Reviewers may still run test suites and linters (their instructions require
  it), so arbitrary project commands like `npm test` or `cargo clippy` pass.

Deny output uses hookSpecificOutput.permissionDecision per the hooks reference.
"""

import json
import re
import sys


def _agent_names(*names: str) -> frozenset:
    return frozenset(names)


READ_ONLY_AGENTS = _agent_names(
    "drift-detector",
    "spec-compliance",
    "blind-spot-finder",
    "quality-scanner",
    "researcher",
    "plan-reviewer",
    "spec-reviewer",
)

# git subcommands that only read repository state.
GIT_READ_ONLY = {
    "annotate", "blame", "cat-file", "check-ignore", "cherry", "count-objects",
    "describe", "diff", "diff-tree", "for-each-ref", "grep", "help", "log",
    "ls-files", "ls-tree", "merge-base", "name-rev", "reflog", "rev-list",
    "rev-parse", "shortlog", "show", "show-ref", "status", "var", "version",
    "whatchanged",
}

# Ref-consuming flags whose following bare argument is not a creation target
# (used for `git branch` / `git tag`, whose bare args otherwise create/delete).
GIT_REF_FLAG = re.compile(
    r"^(--contains|--no-contains|--points-at|--merged|--no-merged|--sort|--format|--list|-l|-n\d*)$"
)

# Flags that make `git branch` / `git tag` mutating even without a bare target.
GIT_MUTATING_FLAG = re.compile(
    r"^(-d|-D|-m|-M|-c|-C|-f|-a|-s|-u|--delete|--move|--copy|--force|--annotate"
    r"|--sign|--edit-description|--set-upstream-to(=.*)?|--unset-upstream)$"
)

P4_READ_ONLY = {
    "annotate", "changes", "counters", "describe", "diff", "diff2", "dirs",
    "files", "filelog", "fstat", "grep", "have", "help", "info", "opened",
    "print", "sizes", "users", "where",
}

# Commands that mutate the filesystem, VCS state, or reach the network.
DENY_COMMANDS = {
    "rm", "mv", "cp", "mkdir", "rmdir", "touch", "chmod", "chown", "chgrp",
    "ln", "truncate", "dd", "rsync", "install", "tee", "patch", "shred",
    "curl", "wget", "ssh", "scp", "sftp", "nc", "ncat", "telnet",
    "apt", "apt-get", "dnf", "yum", "brew", "pacman", "sudo", "doas",
    "kill", "pkill", "killall", "reboot", "shutdown", "systemctl", "crontab",
}

# command word -> regex over its arguments that makes the segment mutating.
DENY_WITH_ARGS = [
    (re.compile(r"^sed$"), re.compile(r"(^|\s)(-i|--in-place)\b")),
    (re.compile(r"^perl$"), re.compile(r"(^|\s)-\S*i")),
    (re.compile(r"^find$"), re.compile(r"(^|\s)(-delete\b|-exec\b.*\b(rm|mv|cp|chmod|sed)\b)")),
    (re.compile(r"^xargs$"), re.compile(r"(^|\s)(rm|mv|cp|chmod|sed|tee)\b")),
    (re.compile(r"^(python\d?(?:\.\d+)?|node|ruby|perl)$"), re.compile(r"(^|\s)(-c|-e|--eval)\b")),
    (re.compile(r"^(sh|bash|zsh|dash|ksh)$"), re.compile(r"(^|\s)-c\b")),
    (re.compile(r"^(npm|pnpm|yarn|bun)$"), re.compile(r"^(install|i|ci|add|remove|rm|update|up|upgrade|link|publish)\b")),
    (re.compile(r"^(pip\d?(?:\.\d+)?)$"), re.compile(r"^(install|uninstall|download)\b")),
    (re.compile(r"^cargo$"), re.compile(r"^(install|add|remove|publish|yank)\b")),
    (re.compile(r"^go$"), re.compile(r"^(install|get)\b")),
    (re.compile(r"^gh$"), re.compile(r".")),  # gh mutates or reaches network; reviewers don't need it
]

# Redirections other than to /dev/null or fd duplication.
REDIRECT = re.compile(r"(?<![0-9&<>])\d?>>?\s*(&\d|\S+)")

WRAPPERS = {"env", "nice", "nohup", "time", "command", "builtin", "stdbuf"}

SEGMENT_SPLIT = re.compile(r"(?:\|\||&&|;|\||\n|\$\(|`)")
ENV_ASSIGN = re.compile(r"^[A-Za-z_][A-Za-z_0-9]*=\S*$")


def deny(reason: str) -> None:
    print(json.dumps({
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": "deny",
            "permissionDecisionReason": (
                f"{reason} You are a read-only sdd-planner reviewer: inspect with "
                "git/p4 read commands, Read/Grep/Glob, and run existing tests or "
                "linters — never modify files, VCS state, or the network. Report "
                "what you wanted to change as a finding instead."
            ),
        }
    }))
    sys.exit(0)


def strip_git_globals(tokens: list) -> list:
    """Drop git's pre-subcommand global flags (-C <p>, -c <kv>, --git-dir=…)."""
    out = list(tokens)
    while out:
        tok = out[0]
        if tok in ("-C", "-c"):
            out = out[2:]
        elif tok.startswith(("--git-dir", "--work-tree", "--namespace")) or tok in ("--no-pager", "-P", "--no-optional-locks", "--literal-pathspecs"):
            out = out[1:]
        else:
            break
    return out


def check_git(tokens: list, segment: str) -> None:
    tokens = strip_git_globals(tokens[1:])
    if not tokens:
        return
    sub, args = tokens[0], tokens[1:]
    first = args[0] if args else ""
    if sub in GIT_READ_ONLY:
        return
    if sub in ("branch", "tag"):
        prev = ""
        for arg in args:
            if GIT_MUTATING_FLAG.match(arg):
                deny(f"Blocked `{segment.strip()}`: `git {sub} {arg}` mutates refs.")
            if not arg.startswith("-") and not GIT_REF_FLAG.match(prev):
                deny(f"Blocked `{segment.strip()}`: `git {sub}` with a target argument creates or deletes refs.")
            prev = arg
        return
    if sub == "stash" and first in ("list", "show"):
        return
    if sub == "worktree" and first == "list":
        return
    if sub == "remote" and (not args or first in ("-v", "--verbose", "show", "get-url")):
        return
    if sub == "config" and first in ("--get", "--get-all", "--get-regexp", "--list", "-l"):
        return
    if sub == "notes" and (not args or first in ("list", "show")):
        return
    if sub == "submodule" and (not args or first == "status"):
        return
    deny(f"Blocked `{segment.strip()}`: `git {sub}` is not on the read-only allowlist.")


def check_p4(tokens: list, segment: str) -> None:
    args = [t for t in tokens[1:] if not t.startswith("-")]
    if not args:
        return
    if args[0] not in P4_READ_ONLY:
        deny(f"Blocked `{segment.strip()}`: `p4 {args[0]}` is not on the read-only allowlist.")


QUOTED = re.compile(r"'[^']*'|\"[^\"]*\"")


def check_segment(segment: str) -> None:
    stripped = segment.strip()
    if not stripped:
        return
    # Quoted text can't redirect; strip it so `grep 'a->b'` isn't a false hit.
    unquoted = QUOTED.sub("''", stripped)
    for match in REDIRECT.finditer(unquoted):
        target = match.group(1)
        if target.startswith("&") or target == "/dev/null":
            continue
        deny(f"Blocked `{stripped}`: output redirection to `{target}` writes a file.")
    tokens = stripped.split()
    while tokens and (ENV_ASSIGN.match(tokens[0]) or tokens[0] in WRAPPERS):
        tokens = tokens[1:]
    if tokens and tokens[0] == "timeout":
        tokens = tokens[2:] if len(tokens) > 1 else []
    if not tokens:
        return
    head = tokens[0].rsplit("/", 1)[-1]
    if head == "git":
        check_git(tokens, stripped)
        return
    if head == "p4":
        check_p4(tokens, stripped)
        return
    if head in DENY_COMMANDS:
        deny(f"Blocked `{stripped}`: `{head}` mutates the filesystem or reaches the network.")
    rest = " ".join(tokens[1:])
    for head_re, args_re in DENY_WITH_ARGS:
        if head_re.match(head) and args_re.search(rest):
            deny(f"Blocked `{stripped}`: this `{head}` invocation is write- or network-shaped.")


def main() -> None:
    try:
        payload = json.load(sys.stdin)
    except (json.JSONDecodeError, ValueError):
        sys.exit(0)  # fail open: never break Bash for the rest of the session
    agent = str(payload.get("agent_type") or "")
    if agent.rsplit(":", 1)[-1] not in READ_ONLY_AGENTS:
        sys.exit(0)
    if payload.get("tool_name") != "Bash":
        sys.exit(0)
    command = payload.get("tool_input", {}).get("command")
    if not isinstance(command, str) or not command.strip():
        sys.exit(0)
    for segment in SEGMENT_SPLIT.split(command):
        check_segment(segment)
    sys.exit(0)


if __name__ == "__main__":
    main()
