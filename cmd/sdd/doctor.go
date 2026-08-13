package main

import (
	"fmt"
	"github.com/danweinerdev/claude-sdd-planner/internal/version"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
)

// schemaInfo is one embedded schema's diagnostic summary.
type schemaInfo struct {
	Type            string `json:"type"`
	Sections        int    `json:"sections,omitempty"`
	FrontmatterMode string `json:"frontmatter_mode,omitempty"`
	Count           int    `json:"artifact_count,omitempty"`
	CountNote       string `json:"count_note,omitempty"`
	Error           string `json:"error,omitempty"`
}

// doctorReport is the whole `sdd doctor` diagnostic surface (FR-42, adapted
// to what this spike actually has: no plugin config, no MCP servers, no
// review lanes — just the schema set, the planning root, and artifact counts).
type doctorReport struct {
	Version           string       `json:"version"`
	BinaryPath        string       `json:"binary_path,omitempty"`
	PlanningRoot      string       `json:"planning_root,omitempty"`
	PlanningRootError string       `json:"planning_root_error,omitempty"`
	Schemas           []schemaInfo `json:"schemas"`
	HookBinary        string       `json:"hook_binary,omitempty"`
	HookBinaryError   string       `json:"hook_binary_error,omitempty"`
}

// cmdDoctor reports the binary's own identity, the resolved planning root (or
// why it couldn't resolve one), and every embedded schema with how many
// artifacts of that type exist under the root.
func cmdDoctor(jsonOut bool) error {

	rep := doctorReport{Version: version.Version}
	rep.HookBinary, rep.HookBinaryError = checkHookBinary()
	if exe, err := os.Executable(); err == nil {
		if abs, err2 := filepath.Abs(exe); err2 == nil {
			rep.BinaryPath = abs
		} else {
			rep.BinaryPath = exe
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("doctor: %w", err)
	}
	root, rootErr := store.FindPlanningRoot(wd)
	if rootErr != nil {
		rep.PlanningRootError = rootErr.Error()
	} else {
		rep.PlanningRoot = relPath(root)
	}

	for _, t := range schema.Types() {
		s, err := schema.Load(t)
		if err != nil {
			rep.Schemas = append(rep.Schemas, schemaInfo{Type: t, Error: err.Error()})
			continue
		}
		info := schemaInfo{
			Type:            t,
			Sections:        len(s.Headings),
			FrontmatterMode: s.FrontmatterMode,
		}
		if rootErr == nil {
			if paths, err := store.List(root, t); err != nil {
				info.CountNote = "artifact counting not supported for this type"
			} else {
				info.Count = len(paths)
			}
		} else {
			info.CountNote = "planning root unresolved"
		}
		rep.Schemas = append(rep.Schemas, info)
	}

	if jsonOut {
		if err := writeJSON(rep); err != nil {
			return err
		}
	} else {
		printDoctorReport(rep)
	}

	if rootErr != nil {
		return fmt.Errorf("doctor: %w", rootErr)
	}
	return nil
}

func printDoctorReport(r doctorReport) {
	fmt.Printf("sdd %s\n  binary: %s\n", r.Version, r.BinaryPath)
	// The hook binary is reported even when healthy: its absence is the one
	// failure with no other symptom, so silence here would be ambiguous.
	if r.HookBinaryError != "" {
		fmt.Printf("  hook binary: %s — %s\n", r.HookBinary, r.HookBinaryError)
	} else if r.HookBinary != "" {
		fmt.Printf("  hook binary: %s (OK)\n", r.HookBinary)
	}
	if r.PlanningRootError != "" {
		fmt.Printf("  planning root: ERROR: %s\n", r.PlanningRootError)
	} else {
		fmt.Printf("  planning root: %s\n", r.PlanningRoot)
	}
	fmt.Println("  schemas:")
	for _, s := range r.Schemas {
		if s.Error != "" {
			fmt.Printf("    %-10s ERROR: %s\n", s.Type, s.Error)
			continue
		}
		count := fmt.Sprintf("%d artifacts", s.Count)
		if s.CountNote != "" {
			count = s.CountNote
		}
		fmt.Printf("    %-10s %2d sections  mode=%-8s %s\n", s.Type, s.Sections, s.FrontmatterMode, count)
	}
}

// checkHookBinary reports whether ${CLAUDE_PLUGIN_ROOT}/bin/sdd exists and
// runs — the path hooks.json executes.
//
// This is the dead-hook state F-02 identified: hooks.json can interpolate only
// ${CLAUDE_PLUGIN_ROOT} and cannot resolve PATH, so a user whose binary lives
// only on PATH has working skills and silently dead hooks. Nothing else
// surfaces that, because every skill keeps working — the failure has no
// symptom until a guard that should have denied something doesn't.
func checkHookBinary() (path, problem string) {
	root := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if root == "" {
		return "", "CLAUDE_PLUGIN_ROOT is unset; cannot check the hook binary path"
	}
	name := "sdd"
	if runtime.GOOS == "windows" {
		name = "sdd.exe"
	}
	p := filepath.Join(root, "bin", name)
	if _, err := os.Stat(p); err != nil {
		return p, "absent — hooks.json points here and will fail open silently; run `sdd provision`"
	}
	if err := exec.Command(p, "version").Run(); err != nil {
		return p, "present but not executable: " + err.Error()
	}
	return p, ""
}
