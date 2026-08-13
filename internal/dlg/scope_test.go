package dlg

import "testing"

// TestSafeScopeTrailingSlash pins the accepted directory-scope spelling: a
// single trailing slash (e.g. `portable/`) is valid, while absolute paths,
// empty segments, and dot segments stay rejected.
func TestSafeScopeTrailingSlash(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"portable/", true},
		{"portable", true},
		{"internal/portable/", true},
		{"portable//", false},
		{"/portable", false},
		{"/", false},
		{"a//b", false},
		{"a/./b", false},
		{"../a", false},
		{"", false},
	}
	for _, c := range cases {
		if got := safeScope(c.value); got != c.want {
			t.Errorf("safeScope(%q) = %v, want %v", c.value, got, c.want)
		}
	}
}
