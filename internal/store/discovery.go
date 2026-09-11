package store

import (
	"path/filepath"
	"strings"
)

// IsGraphRuntimeDir reports whether path is the reserved runtime directory at
// Plans/<Plan>/.graph beneath planningRoot. It deliberately matches only that
// directory entry: unrelated hidden directories and graph-named files remain
// ordinary artifact-discovery candidates.
func IsGraphRuntimeDir(planningRoot, path string) bool {
	rel, err := filepath.Rel(planningRoot, path)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	return len(parts) == 3 && parts[0] == "Plans" && parts[1] != "" && parts[1] != "." && parts[1] != ".." && parts[2] == ".graph"
}
