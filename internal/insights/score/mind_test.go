package score

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/mind/journal"
)

func logged(day, mood, energy int) checkin.CheckIn {
	return checkin.CheckIn{
		LocalDate: time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC),
		Mood:      mood,
		Energy:    energy,
	}
}

func wrote(day int) journal.Entry {
	return journal.Entry{CreatedAt: time.Date(2026, 9, day, 9, 0, 0, 0, time.UTC)}
}

func TestMindWeightsSumToOneHundred(t *testing.T) {
	total := 0
	for _, c := range Mind(nil, nil, 7).Components {
		total += c.Weight
	}
	if total != 100 {
		t.Errorf("weights sum to %d, want 100", total)
	}
}

func TestMindWithNothingLoggedHasNoData(t *testing.T) {
	// A person who has never checked in has not scored badly on their inner
	// life. They have not been measured.
	got := Mind(nil, nil, 7)

	if got.HasData {
		t.Errorf("HasData = true, want false (coverage %d)", got.Coverage)
	}
	for _, c := range got.Components {
		if c.Known {
			t.Errorf("component %q is Known with nothing logged", c.Key)
		}
	}
}

func TestMindScoresAWellLoggedWeek(t *testing.T) {
	checkIns := []checkin.CheckIn{
		logged(1, 5, 5), logged(2, 5, 5), logged(3, 5, 5),
		logged(4, 5, 5), logged(5, 5, 5), logged(6, 5, 5), logged(7, 5, 5),
	}
	entries := []journal.Entry{wrote(1), wrote(3), wrote(5)}

	got := Mind(checkIns, entries, 7)

	if !got.HasData {
		t.Fatal("HasData = false, want true")
	}
	if got.Points != 100 {
		t.Errorf("Points = %d, want 100", got.Points)
	}
}

func TestMindCountsCheckingInRarelyAsLowNotUnknown(t *testing.T) {
	// Checking in once in a month is a real, low, measured number. Only
	// never checking in at all is unknown.
	got := Mind([]checkin.CheckIn{logged(1, 3, 3)}, nil, 30)

	c := component(t, got, "checkin_coverage")
	if !c.Known {
		t.Fatal("checkin_coverage is not Known after a check-in was logged")
	}
	if c.Earned >= c.Weight/2 {
		t.Errorf("one check-in in 30 days earned %d of %d, want a low score", c.Earned, c.Weight)
	}
	if component(t, got, "journal").Known {
		t.Error("journal is Known with no entries written")
	}
}

func TestMindLowMoodScoresBelowHighMood(t *testing.T) {
	low := component(t, Mind([]checkin.CheckIn{logged(1, 1, 1), logged(2, 1, 1)}, nil, 7), "mood")
	high := component(t, Mind([]checkin.CheckIn{logged(1, 5, 5), logged(2, 5, 5)}, nil, 7), "mood")

	if low.Earned >= high.Earned {
		t.Errorf("mood 1/5 earned %d, mood 5/5 earned %d — want low to score lower",
			low.Earned, high.Earned)
	}
	if high.Earned != high.Weight {
		t.Errorf("mood 5/5 earned %d of %d, want full marks", high.Earned, high.Weight)
	}
}

func TestMindIgnoresCheckInsWithNoMoodRecorded(t *testing.T) {
	got := Mind([]checkin.CheckIn{logged(1, 0, 0), logged(2, 0, 0)}, nil, 7)

	if component(t, got, "mood").Known {
		t.Error("mood is Known when no check-in carried a rating")
	}
	if !component(t, got, "checkin_coverage").Known {
		t.Error("checkin_coverage should still be Known — the check-ins happened")
	}
}
