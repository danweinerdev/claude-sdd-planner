//go:build !windows

package main

import (
	"os"
	"testing"
)

func obstructReviewsRead(t *testing.T, reviews string) {
	t.Helper()
	if err := os.Chmod(reviews, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(reviews, 0o755); err != nil {
			t.Errorf("restoring reviews directory permissions: %v", err)
		}
	})
}
