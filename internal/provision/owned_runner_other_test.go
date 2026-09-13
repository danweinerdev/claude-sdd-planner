//go:build !unix

package provision

import "errors"

// blockingReadSource has no non-unix implementation; the behavioral subtest
// skips before calling it.
func blockingReadSource(string) (string, error) {
	return "", errors.New("no blocking read source on this platform")
}
