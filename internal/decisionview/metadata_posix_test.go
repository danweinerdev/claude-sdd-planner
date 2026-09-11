//go:build !windows

package decisionview_test

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func TestForkMetadataRejectsFIFO(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "source.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := decisionview.ReadCollectionMetadata(decisionview.Roots{Planning: root}, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "source.md"}); err == nil {
		t.Fatal("FIFO source metadata was accepted")
	}
}
