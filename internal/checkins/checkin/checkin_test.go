package checkin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
)

func day(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
}

// Notes is where the user writes freely. It is the highest-signal text on a
// check-in, so the coach-facing summary has to carry it.
func TestSummaryIncludesNotes(t *testing.T) {
	t.Parallel()
	c := checkin.CheckIn{
		LocalDate: day(t),
		Mood:      4,
		Energy:    3,
		Notes:     "slept badly but trained anyway",
	}

	got := c.Summary()
	if !strings.Contains(got, "Notes: slept badly but trained anyway") {
		t.Fatalf("summary should carry the notes, got %q", got)
	}
}

func TestSummaryOmitsBlankNotes(t *testing.T) {
	t.Parallel()
	for name, notes := range map[string]string{
		"empty":      "",
		"whitespace": "   \n\t ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := checkin.CheckIn{LocalDate: day(t), Mood: 3, Energy: 3, Notes: notes}
			if got := c.Summary(); strings.Contains(got, "Notes:") {
				t.Fatalf("blank notes should add no clause, got %q", got)
			}
		})
	}
}

func TestSummaryTruncatesLongNotes(t *testing.T) {
	t.Parallel()
	c := checkin.CheckIn{
		LocalDate: day(t),
		Mood:      3,
		Energy:    3,
		Notes:     strings.Repeat("a", 200),
	}

	got := c.Summary()
	if !strings.Contains(got, "…") {
		t.Fatalf("long notes should be truncated with an ellipsis, got %q", got)
	}
	if strings.Contains(got, strings.Repeat("a", 200)) {
		t.Fatalf("long notes should not be sent whole, got %q", got)
	}
}

// The clause order is what the coach reads, so it is part of the contract.
func TestSummaryOrdersClauses(t *testing.T) {
	t.Parallel()
	c := checkin.CheckIn{
		LocalDate:        day(t),
		Mood:             5,
		Energy:           2,
		Wins:             "hit a PR",
		Challenges:       "left knee sore",
		Notes:            "deload week next",
		RelatedGoalTitle: "Run a marathon",
	}

	got := c.Summary()
	want := "2 Jan — mood 5/5, energy 2/5 (re: Run a marathon). Wins: hit a PR. Challenges: left knee sore. Notes: deload week next"
	if got != want {
		t.Fatalf("summary mismatch\n got: %q\nwant: %q", got, want)
	}
}
