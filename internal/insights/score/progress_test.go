package score

import "testing"

func TestProgressWeightsSumToOneHundred(t *testing.T) {
	total := 0
	for _, c := range Progress(ProgressInput{}).Components {
		total += c.Weight
	}
	if total != 100 {
		t.Errorf("weights sum to %d, want 100", total)
	}
}

func TestProgressWithoutGoalsHasNoData(t *testing.T) {
	// Somebody who has not set a goal is not failing at their goals.
	got := Progress(ProgressInput{WindowDays: 7})

	if got.HasData {
		t.Errorf("HasData = true, want false (coverage %d)", got.Coverage)
	}
}

func TestProgressScoresATendedSetOfGoals(t *testing.T) {
	got := Progress(ProgressInput{
		ActiveGoals: 2,
		Notes:       2,
		Overdue:     0,
		Streak:      7,
		WindowDays:  7,
	})

	if !got.HasData {
		t.Fatal("HasData = false, want true")
	}
	if got.Points != 100 {
		t.Errorf("Points = %d, want 100", got.Points)
	}
}

func TestProgressOverdueMilestonesCostPoints(t *testing.T) {
	clean := component(t, Progress(ProgressInput{ActiveGoals: 2, Overdue: 0, WindowDays: 7}), "overdue")
	slipping := component(t, Progress(ProgressInput{ActiveGoals: 2, Overdue: 4, WindowDays: 7}), "overdue")

	if clean.Earned != clean.Weight {
		t.Errorf("no overdue milestones earned %d of %d, want full marks", clean.Earned, clean.Weight)
	}
	if slipping.Earned >= clean.Earned {
		t.Errorf("4 overdue earned %d, 0 overdue earned %d — want overdue to score lower",
			slipping.Earned, clean.Earned)
	}
}

func TestProgressStreakIsKnownEvenWithoutGoals(t *testing.T) {
	// Checking in is its own habit. It is measurable whether or not the
	// person keeps goals, so it carries the domain's only known component
	// for somebody who has not set one — which is still too little to score.
	got := Progress(ProgressInput{Streak: 3, WindowDays: 7})

	if !component(t, got, "streak").Known {
		t.Error("streak is not Known despite a live streak")
	}
	if got.HasData {
		t.Error("HasData = true on streak alone, want false — 25 is below the floor")
	}
}
