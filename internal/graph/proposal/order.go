package proposal

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// A durable high-water mark keeps sequential CLI invocations ordered even
// under a coarse clock or clock rollback. Reserving an ID is the linearization
// point; failed publishes may leave gaps but never reuse or overwrite an ID.
func reserveFragmentID(dir, candidate string) (string, error) {
	if _, err := parseFragmentID(candidate); err != nil {
		return "", err
	}
	statePath := filepath.Join(filepath.Dir(dir), "fragment-order")
	for attempt := 0; attempt < 128; attempt++ {
		state, err := istore.Read(statePath)
		if err != nil {
			return "", err
		}
		last := ""
		if state.Exists {
			last = strings.TrimSpace(state.Source)
			if _, err := parseFragmentID(last); err != nil {
				return "", fmt.Errorf("graph propose: invalid fragment-order state: %w", err)
			}
		}
		// Existing fragments can predate the allocator's introduction, or the
		// high-water file may have been removed by workspace cleanup.
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			id := strings.TrimSuffix(entry.Name(), ".json")
			if entry.IsDir() || id == entry.Name() {
				continue
			}
			if _, err := parseFragmentID(id); err == nil && id > last {
				last = id
			}
		}
		next := candidate
		if next <= last {
			next, err = nextFragmentID(last)
			if err != nil {
				return "", err
			}
		}
		err = istore.WriteAtomicExpecting(statePath, next+"\n", state.Digest)
		if err == nil {
			return next, nil
		}
		var collision *istore.ErrConcurrentWrite
		if !errors.As(err, &collision) {
			return "", err
		}
	}
	return "", fmt.Errorf("graph propose: fragment ordering allocation exhausted concurrent-write retries")
}

func parseFragmentID(id string) ([]byte, error) {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return nil, fmt.Errorf("invalid fragment UUID %q", id)
	}
	b, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	if err != nil || len(b) != 16 || b[6]>>4 != 7 || b[8]>>6 != 2 || id != strings.ToLower(id) {
		return nil, fmt.Errorf("invalid fragment UUIDv7 %q", id)
	}
	return b, nil
}

func nextFragmentID(last string) (string, error) {
	b, err := parseFragmentID(last)
	if err != nil {
		return "", err
	}
	// Increment the sortable payload, preserving the UUID version/variant
	// bits. Carry into logical timestamp bits only after payload exhaustion.
	for i := 15; i >= 0; i-- {
		mask, fixed := byte(0xff), byte(0)
		if i == 8 {
			mask, fixed = 0x3f, 0x80
		} else if i == 6 {
			mask, fixed = 0x0f, 0x70
		}
		if b[i]&mask < mask {
			b[i]++
			return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
		}
		b[i] = fixed
	}
	return "", fmt.Errorf("graph propose: fragment UUID space exhausted")
}
