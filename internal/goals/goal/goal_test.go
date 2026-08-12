package goal_test

import (
	"strings"
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

func TestProgressFromMilestones(t *testing.T) {
	t.Parallel()

	g := goal.Goal{}.WithMilestones([]goal.Milestone{
		{Status: goal.MilestoneCompleted},
		{Status: goal.MilestoneOpen},
		{Status: goal.MilestoneCompleted},
	})

	pct, ok := g.Progress()
	if !ok || pct != 66 {
		t.Fatalf("Progress() = %d, %v; want 66, true", pct, ok)
	}
	if g.ProgressLabel() != "2 of 3" {
		t.Fatalf("ProgressLabel() = %q", g.ProgressLabel())
	}
}

func TestProgressFromLatestNoteWhenNoMilestones(t *testing.T) {
	t.Parallel()

	n := 40
	g := goal.Goal{LatestUpdate: &goal.Update{Progress: &n}}

	pct, ok := g.Progress()
	if !ok || pct != 40 {
		t.Fatalf("Progress() = %d, %v; want 40, true", pct, ok)
	}
	if g.ProgressLabel() != "40%" {
		t.Fatalf("ProgressLabel() = %q", g.ProgressLabel())
	}
}

func TestProgressUnsetWithoutMilestonesOrNote(t *testing.T) {
	t.Parallel()

	g := goal.Goal{}
	if pct, ok := g.Progress(); ok {
		t.Fatalf("Progress() = %d, %v; want unset", pct, ok)
	}
	if g.ProgressLabel() != "" {
		t.Fatalf("ProgressLabel() = %q", g.ProgressLabel())
	}
}

func TestSummaryNamesMilestoneCounts(t *testing.T) {
	t.Parallel()

	g := goal.Goal{Title: "Run 10k"}.WithMilestones([]goal.Milestone{
		{Status: goal.MilestoneCompleted},
		{Status: goal.MilestoneOpen},
	})

	got := g.Summary()
	if !strings.Contains(got, "1 of 2 milestones") {
		t.Fatalf("summary = %q", got)
	}
}
