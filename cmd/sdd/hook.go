package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/danweinerdev/claude-sdd-planner/internal/hook"
)

// `sdd hook` implements the plugin's Claude Code hooks (FR-27/28/44),
// replacing hooks/reviewer-bash-guard.py and hooks/load-decisions.sh.
//
// Both subcommands fail open on every error path and always exit 0. A hook
// that exits nonzero or emits malformed JSON degrades the session for every
// later call, which is a worse outcome than a missed denial — the behavioral
// guidance in each agent's prompt remains the primary control.

type hookPayload struct {
	AgentType string `json:"agent_type"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
	CWD string `json:"cwd"`
}

func cmdHook(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("hook: expected `pretooluse` or `sessionstart`")
	}
	switch args[0] {
	case "pretooluse":
		return hookPreToolUse()
	case "sessionstart":
		return hookSessionStart()
	default:
		return fmt.Errorf("hook: unknown event %q; expected `pretooluse` or `sessionstart`", args[0])
	}
}

func hookPreToolUse() error {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil // fail open
	}
	var payload hookPayload
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}

	var decision hook.Decision
	switch payload.ToolName {
	case "Bash":
		decision = hook.CheckBash(payload.AgentType, payload.ToolInput.Command)
	case "Write", "Edit", "NotebookEdit":
		decision = hook.CheckWrite(payload.AgentType, payload.ToolName,
			payload.ToolInput.FilePath, projectDir(payload))
	}
	if !decision.Deny {
		return nil
	}

	out, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": decision.Reason,
		},
	})
	if err != nil {
		return nil
	}
	fmt.Println(string(out))
	return nil
}

func hookSessionStart() error {
	raw, _ := io.ReadAll(os.Stdin)
	var payload hookPayload
	_ = json.Unmarshal(raw, &payload)

	context := hook.SessionStartContext(projectDir(payload))
	if context == "" {
		return nil
	}
	out, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": context,
		},
	})
	if err != nil {
		return nil
	}
	fmt.Println(string(out))
	return nil
}

// projectDir prefers the payload's cwd, then CLAUDE_PROJECT_DIR, then the
// process working directory — the same order the shell hook used.
func projectDir(p hookPayload) string {
	if p.CWD != "" {
		return p.CWD
	}
	if env := os.Getenv("CLAUDE_PROJECT_DIR"); env != "" {
		return env
	}
	wd, _ := os.Getwd()
	return wd
}
