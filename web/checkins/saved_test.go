package checkins

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
)

func renderSaved(t *testing.T, v SavedView) string {
	t.Helper()
	var b bytes.Buffer
	if err := SavedPanel(v).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func weekOf(states ...checkin.DayState) [7]checkin.WeekDay {
	var w [7]checkin.WeekDay
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	for i := range w {
		s := checkin.DayUpcoming
		if i < len(states) {
			s = states[i]
		}
		w[i] = checkin.WeekDay{Date: monday.AddDate(0, 0, i), State: s}
	}
	return w
}

// A rest day is said plainly next to the streak it kept, and the week shows
// which day it covered without calling it missed.
func TestSavedPanelNamesRestDaysAndDrawsTheWeek(t *testing.T) {
	t.Parallel()
	html := renderSaved(t, SavedView{
		CheckIn: checkin.CheckIn{Mood: 4, Energy: 3},
		Run: checkin.Run{
			Current: 12, Best: 12, RestsUsed: 1, RestsHeld: 1,
			Week: weekOf(checkin.DayChecked, checkin.DayRest, checkin.DayChecked, checkin.DayNone, checkin.DayChecked),
		},
	})
	for _, want := range []string{"12-day streak", "1 rest day used", "1 rest day in hand", `data-day-state="rest"`, `data-day-state="none"`} {
		if !strings.Contains(html, want) {
			t.Errorf("saved panel missing %q", want)
		}
	}
	if n := strings.Count(html, "data-day-state="); n != 7 {
		t.Errorf("week has %d days, want 7", n)
	}
	if strings.Contains(strings.ToLower(html), "missed") {
		t.Error("a past day without a check-in must not read as missed")
	}
}

func TestSavedPanelMarksAPerfectWeek(t *testing.T) {
	t.Parallel()
	html := renderSaved(t, SavedView{Run: checkin.Run{
		Current: 3, PerfectWeek: true, Week: weekOf(checkin.DayChecked, checkin.DayChecked, checkin.DayChecked),
	}})
	if !strings.Contains(html, "Perfect week so far") {
		t.Error("a perfect week so far should say so")
	}
}

// The first day of a new run names the best one, not the gap.
func TestSavedPanelStartsANewRunWithTheBest(t *testing.T) {
	t.Parallel()
	html := renderSaved(t, SavedView{Run: checkin.Run{Current: 1, Best: 23, PreviousRun: 6}})
	if !strings.Contains(html, "Day one of a new run. Your best is 23 days.") {
		t.Errorf("new run line missing: %s", html)
	}
	// Every other day is not a comeback each time.
	if html := renderSaved(t, SavedView{Run: checkin.Run{Current: 1, Best: 23, PreviousRun: 1}}); strings.Contains(html, "new run") {
		t.Error("a one-day run before this one is not worth the new-run line")
	}
}

// The coach's note is fetched after the panel lands, for today's check-in
// only: an edit of an older day has no note to show.
func TestSavedPanelLoadsTheNoteForTodayOnly(t *testing.T) {
	t.Parallel()
	if html := renderSaved(t, SavedView{Run: checkin.Run{Current: 2}, Reply: true}); !strings.Contains(html, `hx-get="/app/check-ins/today/reply"`) {
		t.Error("today's save should load the note")
	}
	if html := renderSaved(t, SavedView{Run: checkin.Run{Current: 2}}); strings.Contains(html, "/app/check-ins/today/reply") {
		t.Error("an older day's edit should not load today's note")
	}
}
