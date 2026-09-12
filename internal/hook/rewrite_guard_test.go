package hook

import "testing"

func TestRewriteAndDoctorGuardMutationBoundary(t *testing.T) {
	for _, command := range []string{
		"sdd doctor", "sdd doctor --check=false", "sdd doctor --check --check=false",
		"sdd hook post-rewrite rebase", "sdd hook post-rewrite amend",
	} {
		if !CheckBash("quality-scanner", command).Deny {
			t.Errorf("read-only reviewer may mutate through %q", command)
		}
	}
	for _, command := range []string{
		"sdd doctor --check", "sdd doctor --check=true --json",
		"sdd hook pretooluse", "sdd hook sessionstart",
	} {
		if result := CheckBash("quality-scanner", command); result.Deny {
			t.Errorf("read-only invocation refused: %q: %s", command, result.Reason)
		}
	}
}
