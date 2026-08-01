package auth

import "testing"

// The `next` parameter comes straight from a URL, so this is the check standing
// between North's login page and being a working phishing redirector.
func TestSafeRedirect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		next string
		want bool
	}{
		{"/app", true},
		{"/app/chat/123", true},
		{"/app?tab=goals", true},

		{"", false},
		{"app", false},                      // not rooted
		{"//evil.example.com", false},       // scheme-relative: a different host
		{"//evil.example.com/steal", false}, //
		{"https://evil.example.com", false}, // absolute
		{"http://evil.example.com", false},  //
		{"/\\evil.example.com", false},      // backslash trick
		{"javascript:alert(1)", false},      // not a path at all
		{"\\\\evil.example.com", false},     // UNC-style
	}

	for _, tt := range tests {
		t.Run(tt.next, func(t *testing.T) {
			t.Parallel()

			if got := SafeRedirect(tt.next); got != tt.want {
				t.Fatalf("SafeRedirect(%q) = %v, want %v", tt.next, got, tt.want)
			}
		})
	}
}
