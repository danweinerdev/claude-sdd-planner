package model

import (
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/testenv"
)

// TestMain installs the hermetic Git policy (internal/testenv) for every
// test in this package before parallel tests start: a temporary HOME and
// config, no system/global/injected configuration, fsmonitor, hooks,
// signing and prompts off, the fixed fixture identity, and the Perforce
// probe disabled. Code under test inherits it from the process
// environment; child processes receive it through the same variables.
func TestMain(m *testing.M) { testenv.Main(m) }
