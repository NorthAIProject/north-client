package lifedomain_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
)

// The list is stored as plain text with no CHECK constraint, so this test is
// the only thing standing between a careless edit and existing rows silently
// becoming unrecognised. Order is asserted too: it is the UI's display order.
func TestDomainsAreTheExpectedSetInOrder(t *testing.T) {
	t.Parallel()

	want := []string{"fitness", "health", "work", "learning", "personal", "other"}

	if len(lifedomain.Domains) != len(want) {
		t.Fatalf("Domains = %v, want %v", lifedomain.Domains, want)
	}
	for i := range want {
		if lifedomain.Domains[i] != want[i] {
			t.Errorf("Domains[%d] = %q, want %q", i, lifedomain.Domains[i], want[i])
		}
	}
}

func TestValid(t *testing.T) {
	t.Parallel()

	for _, domain := range lifedomain.Domains {
		if !lifedomain.Valid(domain) {
			t.Errorf("Valid(%q) = false, want true", domain)
		}
	}

	for _, domain := range []string{"", "Fitness", "nutrition", "mind"} {
		if lifedomain.Valid(domain) {
			t.Errorf("Valid(%q) = true, want false", domain)
		}
	}
}
