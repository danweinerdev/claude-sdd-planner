//go:build !unix

package main

import "errors"

// blockingReadSource has no non-unix implementation; the behavioral subtest
// skips before calling it.
func blockingReadSource(string) (string, error) {
	return "", errors.New("no blocking read source on this platform")
}
