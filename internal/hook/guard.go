// Package hook implements the plugin's Claude Code hooks (FR-27, FR-28,
// FR-44), replacing hooks/reviewer-bash-guard.py and hooks/load-decisions.sh
// so the plugin carries no Python or POSIX-shell runtime dependency.
//
// The Bash guard's behavior is reproduced exactly, not reinterpreted: the same
// read-only git/p4 subcommand allowlists, the same write- and network-shaped
// denials, the same permission for test and lint runs, applying only to the
// seven read-only agents, and failing open for every other agent and the main
// session. Its parity fixtures live in guard_parity_test.go.
package hook

import (
	"regexp"
	"strings"
)

// readOnlyAgents are the seven agents whose value comes from intent isolation.
// Every other caller — including the main session — is unaffected by this
// guard.
var readOnlyAgents = map[string]bool{
	"drift-detector":    true,
	"spec-compliance":   true,
	"blind-spot-finder": true,
	"quality-scanner":   true,
	"researcher":        true,
	"plan-reviewer":     true,
	"spec-reviewer":     true,
}

// gitReadOnly are git subcommands that only read repository state.
var gitReadOnly = map[string]bool{
	"annotate": true, "blame": true, "cat-file": true, "check-ignore": true,
	"cherry": true, "count-objects": true, "describe": true, "diff": true,
	"diff-tree": true, "for-each-ref": true, "grep": true, "help": true,
	"log": true, "ls-files": true, "ls-tree": true, "merge-base": true,
	"name-rev": true, "reflog": true, "rev-list": true, "rev-parse": true,
	"shortlog": true, "show": true, "show-ref": true, "status": true,
	"var": true, "version": true, "whatchanged": true,
}

// gitRefFlag matches ref-consuming flags whose following bare argument is not
// a creation target, so `git branch --contains X` reads rather than writes.
var gitRefFlag = regexp.MustCompile(
	`^(--contains|--no-contains|--points-at|--merged|--no-merged|--sort|--format|--list|-l|-n\d*)$`)

// gitMutatingFlag makes `git branch`/`git tag` mutating even with no target.
var gitMutatingFlag = regexp.MustCompile(
	`^(-d|-D|-m|-M|-c|-C|-f|-a|-s|-u|--delete|--move|--copy|--force|--annotate` +
		`|--sign|--edit-description|--set-upstream-to(=.*)?|--unset-upstream)$`)

var p4ReadOnly = map[string]bool{
	"annotate": true, "changes": true, "counters": true, "describe": true,
	"diff": true, "diff2": true, "dirs": true, "files": true, "filelog": true,
	"fstat": true, "grep": true, "have": true, "help": true, "info": true,
	"opened": true, "print": true, "sizes": true, "users": true, "where": true,
}

// denyCommands mutate the filesystem, VCS state, or reach the network.
var denyCommands = map[string]bool{
	"rm": true, "mv": true, "cp": true, "mkdir": true, "rmdir": true,
	"touch": true, "chmod": true, "chown": true, "chgrp": true, "ln": true,
	"truncate": true, "dd": true, "rsync": true, "install": true, "tee": true,
	"patch": true, "shred": true,
	"curl": true, "wget": true, "ssh": true, "scp": true, "sftp": true,
	"nc": true, "ncat": true, "telnet": true,
	"apt": true, "apt-get": true, "dnf": true, "yum": true, "brew": true,
	"pacman": true, "sudo": true, "doas": true,
	"kill": true, "pkill": true, "killall": true, "reboot": true,
	"shutdown": true, "systemctl": true, "crontab": true,
}

// denyWithArgs pairs a command word with the argument shape that makes it
// mutating, so `sed` reads but `sed -i` does not.
var denyWithArgs = []struct{ head, args *regexp.Regexp }{
	{regexp.MustCompile(`^sed$`), regexp.MustCompile(`(^|\s)(-i|--in-place)\b`)},
	{regexp.MustCompile(`^perl$`), regexp.MustCompile(`(^|\s)-\S*i`)},
	{regexp.MustCompile(`^find$`), regexp.MustCompile(`(^|\s)(-delete\b|-exec\b.*\b(rm|mv|cp|chmod|sed)\b)`)},
	{regexp.MustCompile(`^xargs$`), regexp.MustCompile(`(^|\s)(rm|mv|cp|chmod|sed|tee)\b`)},
	{regexp.MustCompile(`^(python\d?(?:\.\d+)?|node|ruby|perl)$`), regexp.MustCompile(`(^|\s)(-c|-e|--eval)\b`)},
	{regexp.MustCompile(`^(sh|bash|zsh|dash|ksh)$`), regexp.MustCompile(`(^|\s)-c\b`)},
	{regexp.MustCompile(`^(npm|pnpm|yarn|bun)$`), regexp.MustCompile(`^(install|i|ci|add|remove|rm|update|up|upgrade|link|publish)\b`)},
	{regexp.MustCompile(`^(pip\d?(?:\.\d+)?)$`), regexp.MustCompile(`^(install|uninstall|download)\b`)},
	{regexp.MustCompile(`^cargo$`), regexp.MustCompile(`^(install|add|remove|publish|yank)\b`)},
	{regexp.MustCompile(`^go$`), regexp.MustCompile(`^(install|get)\b`)},
	{regexp.MustCompile(`^gh$`), regexp.MustCompile(`.`)},
}

// sddReadOnly are the sdd subcommands a read-only agent may run (FR-44).
//
// This is an allowlist rather than a denylist by requirement: the guard
// permits unrecognized command heads, so a mutating subcommand added later
// without an entry here would hand the read-only agents a sanctioned way to
// rewrite planning artifacts. TestSddAllowlistCoversEverySubcommand fails when
// the command tree grows past this list.
var sddReadOnly = map[string]bool{
	"validate": true, "show": true, "list": true, "next": true,
	"version": true, "doctor": true, "schema": true,
}

// sddDecideReadOnly are the `sdd decide` subcommands that only read.
var sddDecideReadOnly = map[string]bool{
	"list": true, "search": true, "validate": true,
}

var (
	redirectRe   = regexp.MustCompile(`\d?>>?\s*(&\d|\S+)`)
	quotedRe     = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	envAssignRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*=\S*$`)
	segmentSplit = regexp.MustCompile(`\|\||&&|;|\||\n|\$\(|` + "`")
)

var wrappers = map[string]bool{
	"env": true, "nice": true, "nohup": true, "time": true,
	"command": true, "builtin": true, "stdbuf": true,
}

// Decision is a guard verdict. Allowed is the fail-open default; a denial
// carries the reason shown to the agent.
type Decision struct {
	Deny   bool
	Reason string
}

const guardAdvice = " You are a read-only sdd-planner reviewer: inspect with " +
	"git/p4 read commands, Read/Grep/Glob, and run existing tests or " +
	"linters. Report what you found instead of changing it."

// CheckBash returns the guard's verdict for one Bash command run by agent.
//
// It fails open in every ambiguous case: an agent outside the read-only set, a
// blank command, or a segment it cannot parse all return allowed. A guard that
// broke Bash for the rest of a session would be worse than one that misses a
// case, and the behavioral prompt guidance remains the primary control.
func CheckBash(agent, command string) Decision {
	name := agent
	if i := strings.LastIndex(agent, ":"); i >= 0 {
		name = agent[i+1:]
	}
	if !readOnlyAgents[name] {
		return Decision{}
	}
	if strings.TrimSpace(command) == "" {
		return Decision{}
	}
	for _, segment := range segmentSplit.Split(command, -1) {
		if d := checkSegment(segment); d.Deny {
			return d
		}
	}
	return Decision{}
}

func deny(reason string) Decision {
	return Decision{Deny: true, Reason: reason + guardAdvice}
}

func checkSegment(segment string) Decision {
	stripped := strings.TrimSpace(segment)
	if stripped == "" {
		return Decision{}
	}
	// Quoted text cannot redirect, so blank it before scanning: `grep 'a->b'`
	// is not a redirection.
	unquoted := quotedRe.ReplaceAllString(stripped, "''")
	for _, m := range redirectRe.FindAllStringSubmatchIndex(unquoted, -1) {
		// Reproduce Python's (?<![0-9&<>]) lookbehind, which Go's RE2 lacks.
		if m[0] > 0 {
			switch unquoted[m[0]-1] {
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '&', '<', '>':
				continue
			}
		}
		target := unquoted[m[2]:m[3]]
		if strings.HasPrefix(target, "&") || target == "/dev/null" {
			continue
		}
		return deny("Blocked `" + stripped + "`: output redirection to `" + target + "` writes a file.")
	}

	tokens := strings.Fields(stripped)
	for len(tokens) > 0 && (envAssignRe.MatchString(tokens[0]) || wrappers[tokens[0]]) {
		tokens = tokens[1:]
	}
	if len(tokens) > 0 && tokens[0] == "timeout" {
		if len(tokens) > 1 {
			tokens = tokens[2:]
		} else {
			tokens = nil
		}
	}
	if len(tokens) == 0 {
		return Decision{}
	}

	head := tokens[0]
	if i := strings.LastIndex(head, "/"); i >= 0 {
		head = head[i+1:]
	}
	switch head {
	case "git":
		return checkGit(tokens, stripped)
	case "p4":
		return checkP4(tokens, stripped)
	case "sdd":
		return checkSdd(tokens, stripped)
	}
	if denyCommands[head] {
		return deny("Blocked `" + stripped + "`: `" + head + "` mutates the filesystem or reaches the network.")
	}
	rest := strings.Join(tokens[1:], " ")
	for _, rule := range denyWithArgs {
		if rule.head.MatchString(head) && rule.args.MatchString(rest) {
			return deny("Blocked `" + stripped + "`: this `" + head + "` invocation is write- or network-shaped.")
		}
	}
	return Decision{}
}

// stripGitGlobals drops git's pre-subcommand global flags.
func stripGitGlobals(tokens []string) []string {
	out := tokens
	for len(out) > 0 {
		tok := out[0]
		switch {
		case tok == "-C" || tok == "-c":
			if len(out) < 2 {
				return nil
			}
			out = out[2:]
		case strings.HasPrefix(tok, "--git-dir"), strings.HasPrefix(tok, "--work-tree"),
			strings.HasPrefix(tok, "--namespace"),
			tok == "--no-pager", tok == "-P", tok == "--no-optional-locks",
			tok == "--literal-pathspecs":
			out = out[1:]
		default:
			return out
		}
	}
	return out
}

func checkGit(tokens []string, segment string) Decision {
	tokens = stripGitGlobals(tokens[1:])
	if len(tokens) == 0 {
		return Decision{}
	}
	sub, args := tokens[0], tokens[1:]
	first := ""
	if len(args) > 0 {
		first = args[0]
	}
	if gitReadOnly[sub] {
		return Decision{}
	}
	if sub == "branch" || sub == "tag" {
		prev := ""
		for _, arg := range args {
			if gitMutatingFlag.MatchString(arg) {
				return deny("Blocked `" + segment + "`: `git " + sub + " " + arg + "` mutates refs.")
			}
			if !strings.HasPrefix(arg, "-") && !gitRefFlag.MatchString(prev) {
				return deny("Blocked `" + segment + "`: `git " + sub + "` with a target argument creates or deletes refs.")
			}
			prev = arg
		}
		return Decision{}
	}
	switch {
	case sub == "stash" && (first == "list" || first == "show"):
		return Decision{}
	case sub == "worktree" && first == "list":
		return Decision{}
	case sub == "remote" && (len(args) == 0 || first == "-v" || first == "--verbose" || first == "show" || first == "get-url"):
		return Decision{}
	case sub == "config" && (first == "--get" || first == "--get-all" || first == "--get-regexp" || first == "--list" || first == "-l"):
		return Decision{}
	case sub == "notes" && (len(args) == 0 || first == "list" || first == "show"):
		return Decision{}
	case sub == "submodule" && (len(args) == 0 || first == "status"):
		return Decision{}
	}
	return deny("Blocked `" + segment + "`: `git " + sub + "` is not on the read-only allowlist.")
}

func checkP4(tokens []string, segment string) Decision {
	var args []string
	for _, t := range tokens[1:] {
		if !strings.HasPrefix(t, "-") {
			args = append(args, t)
		}
	}
	if len(args) == 0 {
		return Decision{}
	}
	if !p4ReadOnly[args[0]] {
		return deny("Blocked `" + segment + "`: `p4 " + args[0] + "` is not on the read-only allowlist.")
	}
	return Decision{}
}

// checkSdd implements FR-44: sdd is guarded like git and p4, by allowlist.
func checkSdd(tokens []string, segment string) Decision {
	var args []string
	for _, t := range tokens[1:] {
		if !strings.HasPrefix(t, "-") {
			args = append(args, t)
		}
	}
	if len(args) == 0 {
		return Decision{}
	}
	sub := args[0]
	if sub == "decide" {
		if len(args) < 2 || !sddDecideReadOnly[args[1]] {
			verb := "add"
			if len(args) >= 2 {
				verb = args[1]
			}
			return deny("Blocked `" + segment + "`: `sdd decide " + verb + "` writes the decision ledger.")
		}
		return Decision{}
	}
	if !sddReadOnly[sub] {
		return deny("Blocked `" + segment + "`: `sdd " + sub + "` mutates planning artifacts.")
	}
	return Decision{}
}
