package stat_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/stats/stat"
)

// Thursday 8 October 2026.
var thursday = time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)

func ago(n int) time.Time { return thursday.AddDate(0, 0, -n) }

func days(ns ...int) map[time.Time]bool {
	out := map[time.Time]bool{}
	for _, n := range ns {
		out[ago(n)] = true
	}
	return out
}

func TestConsistencyStreaksGapsAndWeeks(t *testing.T) {
	t.Parallel()
	// Trained the last three days (not today yet), a five-day run three
	// weeks back, and a check-in alone 40 days ago.
	trained := days(1, 2, 3, 20, 21, 22, 23, 24)
	checked := days(1, 40)
	c := stat.ConsistencyOf(trained, checked, thursday, 12)

	// Monday eleven weeks back to today: whole week columns for a grid.
	if len(c.Days) != 11*7+4 || !c.Days[0].Day.Equal(ago(80)) || !c.Days[len(c.Days)-1].Day.Equal(thursday) {
		t.Fatalf("days = %d ending %v", len(c.Days), c.Days[len(c.Days)-1].Day)
	}
	// Today is not over: a streak ending yesterday is still current.
	if c.CurrentStreak != 3 || c.LongestStreak != 5 {
		t.Errorf("streaks = %d current, %d longest", c.CurrentStreak, c.LongestStreak)
	}
	// Between day 20 and day 3, sixteen days passed with nothing; the gap
	// still open today does not count until it closes.
	if c.LongestGap != 16 {
		t.Errorf("longest gap = %d", c.LongestGap)
	}
	// This week is Monday 5 Oct to today: days 1, 2 and 3 back.
	if c.ThisWeek != 3 || len(c.Weeks) != 12 || !c.Weeks[len(c.Weeks)-1].Start.Equal(ago(3)) {
		t.Errorf("this week = %d, weeks %d, last starts %v", c.ThisWeek, len(c.Weeks), c.Weeks[len(c.Weeks)-1].Start)
	}
	if c.BestWeek.Days != 5 {
		t.Errorf("best week = %+v", c.BestWeek)
	}
	if !c.Days[len(c.Days)-2].Trained || !c.Days[len(c.Days)-2].CheckedIn {
		t.Errorf("yesterday = %+v", c.Days[len(c.Days)-2])
	}
}

func TestConsistencyOfNothingIsZero(t *testing.T) {
	t.Parallel()
	c := stat.ConsistencyOf(nil, nil, thursday, 4)
	if len(c.Days) != 3*7+4 || len(c.Weeks) != 4 {
		t.Errorf("empty grid = %d days, %d weeks", len(c.Days), len(c.Weeks))
	}
	if c.CurrentStreak != 0 || c.LongestStreak != 0 || c.LongestGap != 0 || c.UsualPerWeek != 0 || c.BestWeek.Days != 0 {
		t.Errorf("empty = %+v", c)
	}
}

// Usual per week counts only finished weeks, so a Tuesday never drags the
// average down with a week that has barely started.
func TestUsualPerWeekLeavesOutTheCurrentWeek(t *testing.T) {
	t.Parallel()
	// Two complete weeks before this one: 4 and 2 active days.
	trained := days(4, 5, 6, 7, 11, 12)
	c := stat.ConsistencyOf(trained, nil, thursday, 3)
	if c.UsualPerWeek != 3 {
		t.Errorf("usual = %v, want 3", c.UsualPerWeek)
	}
}
