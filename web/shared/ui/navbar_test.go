package ui

import (
	"context"
	"strings"
	"testing"
)

// The X link sits in the navbar for everyone, signed in or not, and opens in a
// new tab without handing the opener to x.com.
func TestTheNavbarLinksToXInANewTab(t *testing.T) {
	for _, signedIn := range []bool{false, true} {
		var b strings.Builder
		if err := Navbar(signedIn).Render(context.Background(), &b); err != nil {
			t.Fatalf("render: %v", err)
		}
		out := b.String()

		for _, want := range []string{
			`href="https://x.com/fatc88"`,
			`target="_blank"`,
			`rel="noopener noreferrer"`,
			`aria-label="Fernando on X"`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("signedIn=%v: navbar is missing %s", signedIn, want)
			}
		}
	}
}
