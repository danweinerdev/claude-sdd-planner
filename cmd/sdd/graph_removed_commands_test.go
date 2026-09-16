package main

import "testing"

func TestRemovedGraphCommandsAreAbsent(t *testing.T) {
	removed := map[string]bool{"acknowledge": true, "rehash": true, "repair-intent": true, "evidence-context": true, "evidence-contract": true}
	for _, cmd := range graphCmd().Commands() {
		if removed[cmd.Name()] {
			t.Errorf("graph command %q is still registered", cmd.Name())
		}
	}
}
