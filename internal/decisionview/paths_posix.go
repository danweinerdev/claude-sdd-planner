//go:build !windows

package decisionview

import (
	"os"
	"syscall"
)

func openCollectionMember(root *os.Root, name string) (*os.File, error) {
	// A racing FIFO replacement must not block before the post-open Stat.
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
