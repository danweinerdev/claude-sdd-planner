package main

import "testing"

func TestGraphRepairIntentCommandRemoved(t *testing.T) {
	for _, command := range graphCmd().Commands() {
		if command.Name() == "repair-intent" {
			t.Fatal("repair-intent remains registered")
		}
	}
}
