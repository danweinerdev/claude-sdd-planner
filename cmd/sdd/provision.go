package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/provision"
)

// `sdd provision` performs FR-37/40: resolve a binary, admit it against the
// FR-38 floor, and refresh ${CLAUDE_PLUGIN_ROOT}/bin/sdd.
//
// /setup calls this FIRST, before any filesystem mutation, and stops the whole
// setup when it fails (FR-40). It never compiles, downloads, or installs —
// `go install` is the user's prerequisite (FR-41), so a missing binary is
// reported with the exact command that satisfies it rather than fixed here.
type provisionOpts struct {
	PluginRoot string
	JSON       bool
	Check      bool
}

func cmdProvision(o provisionOpts) error {

	root := o.PluginRoot
	if root == "" {
		root = os.Getenv("CLAUDE_PLUGIN_ROOT")
	}
	if root == "" {
		return fmt.Errorf("provision: no plugin root; pass --plugin-root or set CLAUDE_PLUGIN_ROOT")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("provision: %w", err)
	}

	floor, err := provision.Floor(root)
	if err != nil {
		return fmt.Errorf("provision: cannot read %s/.claude-plugin/plugin.json: %w", root, err)
	}

	var res provision.Result
	if o.Check {
		res, err = provision.Resolve(root, floor)
	} else {
		res, err = provision.Provision(root, floor)
	}
	if err != nil {
		return provisionFailure(res, floor, err)
	}

	if o.JSON {
		out, marshalErr := json.MarshalIndent(map[string]any{
			"source":      res.Source,
			"version":     res.Version,
			"floor":       floor,
			"plugin_copy": res.PluginCopy,
			"refreshed":   res.Refreshed,
			"hooks_path":  res.HooksPath,
			"hooks_wrote": res.HooksWrote,
		}, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("resolved sdd %s at %s\n", res.Version, res.Source)
	if res.PluginCopy != "" {
		state := "already current"
		if res.Refreshed {
			state = "refreshed"
		}
		fmt.Printf("  plugin copy: %s (%s)\n", res.PluginCopy, state)
	}
	if res.HooksPath != "" {
		state := "already current"
		if res.HooksWrote {
			state = "written for " + runtime.GOOS
		}
		fmt.Printf("  hooks: %s (%s)\n", res.HooksPath, state)
	}
	fmt.Printf("  floor: %s\n", floor)
	return nil
}

// provisionFailure reports why no binary was admitted, naming every candidate
// considered and the command that fixes it (FR-38, FR-41).
func provisionFailure(res provision.Result, floor string, cause error) error {
	msg := "provision: no usable sdd binary.\n"
	if len(res.Candidates) == 0 {
		msg += "  no candidate found at ${CLAUDE_PLUGIN_ROOT}/bin/sdd or on PATH\n"
	}
	for _, c := range res.Candidates {
		msg += fmt.Sprintf("  rejected %s: %s\n", c.Path, c.Reason)
	}
	msg += "  required floor: " + floor + "\n"
	msg += "  install it with:\n    " + provision.InstallCommand(floor)
	return fmt.Errorf("%s", msg)
}
