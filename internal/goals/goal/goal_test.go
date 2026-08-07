package goal_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/goals/goal"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
)

// Goals used to own this list. It moved to internal/shared/lifedomain once
// habits needed the same vocabulary, and goal.Categories became an alias.
//
// This test exists because re-inlining the literal here would be an easy and
// invisible mistake: the two lists would agree on the day it was written and
// drift the first time a domain was added to one of them.
func TestCategoriesAreTheSharedLifeDomains(t *testing.T) {
	t.Parallel()

	if len(goal.Categories) != len(lifedomain.Domains) {
		t.Fatalf("goal.Categories = %v, lifedomain.Domains = %v", goal.Categories, lifedomain.Domains)
	}
	for i := range lifedomain.Domains {
		if goal.Categories[i] != lifedomain.Domains[i] {
			t.Errorf("index %d: goal.Categories has %q, lifedomain.Domains has %q",
				i, goal.Categories[i], lifedomain.Domains[i])
		}
	}
}
