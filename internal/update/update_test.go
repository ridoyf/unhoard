package update

import "testing"

func TestNewer(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "0.1.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.10", "v0.1.9", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.9.9", "v1.0.0", false},
		{"v1.0.0-beta.1", "v0.9.0", true},
		{"garbage", "v0.1.0", false},
	}
	for _, tc := range tests {
		if got := Newer(tc.a, tc.b); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}
