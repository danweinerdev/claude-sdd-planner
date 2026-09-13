//go:build unix

package provision

import "syscall"

// blockingReadSource returns a path a shell `read` blocks on forever: a fifo
// nobody ever writes. It blocks in the kernel, so the hanging-git stub costs
// no CPU while the runner's deadline runs down.
func blockingReadSource(path string) (string, error) {
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
