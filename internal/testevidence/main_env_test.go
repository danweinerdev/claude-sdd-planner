package testevidence

import (
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/testenv"
)

// Install the shared hermetic policy before any capture or Git fixture runs.
// Child processes inherit test-owned configuration, never the developer's.
func TestMain(m *testing.M) { testenv.Main(m) }
