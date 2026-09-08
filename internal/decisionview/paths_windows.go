package decisionview

import "os"

func openCollectionMember(root *os.Root, name string) (*os.File, error) {
	// os.Root enforces handle-relative containment and rejects Windows devices.
	return root.Open(name)
}
