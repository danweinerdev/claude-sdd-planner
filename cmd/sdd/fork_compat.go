package main

// Transitional helpers kept alive for apply/section/migrate until the
// global decision ledger is removed (Designs/PlanDecisions § Migration).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

var errNoPlanningConfig = errors.New("no planning-config.json found")

func decisionRepositoryRootFrom(dir string) (string, error) {
	var err error
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		configPath := filepath.Join(dir, "planning-config.json")
		if info, statErr := os.Stat(configPath); statErr == nil {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("%s is not a regular file", configPath)
			}
			return dir, nil
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w at or above %s", errNoPlanningConfig, dir)
		}
		dir = parent
	}
}

func forkCaptureInvalid(capture *decisionview.ConsumerCapture) bool {
	for _, d := range capture.Diagnostics {
		if d.Severity == decisionview.Error || d.Severity == decisionview.Operational {
			return true
		}
	}
	return false
}

func forkCaptureOperational(capture *decisionview.ConsumerCapture) bool {
	for _, d := range capture.Diagnostics {
		if d.Severity == decisionview.Operational {
			return true
		}
	}
	return false
}
