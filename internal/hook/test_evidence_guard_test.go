package hook

import "testing"

func TestTestEvidenceGuard(t *testing.T) {
	for _, tc := range []struct {
		name    string
		agent   string
		command string
		deny    bool
	}{
		{
			name:    "retired test command is not allowlisted",
			agent:   "quality-scanner",
			command: "sdd test check --plan Sample --node n-1 --attempt attempt-1 --expect green --json",
			deny:    true,
		},
		{
			name:    "quality scanner may not run",
			agent:   "quality-scanner",
			command: "sdd test run --plan Sample --node n-1 --by scanner --phase green",
			deny:    true,
		},
		{
			name:    "quality scanner may not clean up",
			agent:   "quality-scanner",
			command: "sdd test cleanup --plan Sample --node n-1 --attempt attempt-1",
			deny:    true,
		},
		{
			name:    "quality scanner may not use unknown test verbs",
			agent:   "quality-scanner",
			command: "sdd test future-write --plan Sample",
			deny:    true,
		},
		{
			name:    "removed evidence contract is not allowlisted",
			agent:   "quality-scanner",
			command: "sdd graph evidence-contract --json",
			deny:    true,
		},
		{
			name:    "quality scanner may read graph status",
			agent:   "quality-scanner",
			command: "sdd graph status --plan Sample --json",
		},
		{
			name:    "unrestricted agent remains unaffected",
			agent:   "code-implementer",
			command: "sdd test run --plan Sample --node n-1 --by implementer --phase green",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CheckBash(tc.agent, tc.command); got.Deny != tc.deny {
				t.Errorf("CheckBash(%q, %q) denial = %t, want %t: %s", tc.agent, tc.command, got.Deny, tc.deny, got.Reason)
			}
		})
	}
}
