package checkins

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/moments"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

// sampleRun is Thursday 24 September 2026: six days in a row with one rest
// day spent on Tuesday, today done, and a best run of 23.
func sampleRun() checkin.Run {
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	states := []checkin.DayState{
		checkin.DayChecked, checkin.DayRest, checkin.DayChecked, checkin.DayChecked,
		checkin.DayUpcoming, checkin.DayUpcoming, checkin.DayUpcoming,
	}
	run := checkin.Run{Current: 6, Best: 23, RestsHeld: 0, RestsUsed: 1}
	for i, s := range states {
		run.Week[i] = checkin.WeekDay{Date: monday.AddDate(0, 0, i), State: s}
	}
	return run
}

func TestCheckInShapes(t *testing.T) {
	t.Parallel()

	stress, sleep := 2, 4
	goal := uuid.MustParse("b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2")
	today := CheckInView{
		ID: uuid.MustParse("e5e5e5e5-e5e5-e5e5-e5e5-e5e5e5e5e5e5"), LocalDate: "2026-09-24", Mood: 4, Energy: 3,
		Wins: "Long run done.", Challenges: "Slept badly.", RelatedGoalID: &goal, RelatedGoalTitle: "Run a half marathon",
		UpdatedAt: time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 9, 24, 7, 30, 0, 0, time.UTC),
		Source:    "siri", Stress: &stress, SleepQuality: &sleep, Tags: []string{"travel", "race week"},
	}
	// An entry from before metadata was recorded: unknown source, no extras,
	// and tags still an empty array rather than absent.
	older := CheckInView{
		ID: uuid.MustParse("e6e6e6e6-e6e6-e6e6-e6e6-e6e6e6e6e6e6"), LocalDate: "2026-09-23", Mood: 3, Energy: 3,
		UpdatedAt: time.Date(2026, 9, 23, 21, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 9, 23, 21, 0, 0, 0, time.UTC),
		Source:    "unknown", Tags: []string{},
	}
	list := CheckInList{Today: &today, Recent: []CheckInView{today, older}}
	list.setRun(sampleRun())
	apitest.AssertGolden(t, "check-ins.golden.json", list)
}

// The save answers with what it meant for the run, and a moment only when it
// created today's check-in.
func TestSavedCheckInShapes(t *testing.T) {
	t.Parallel()

	saved := Saved{
		CheckIn: CheckIn{
			ID:        uuid.MustParse("e5e5e5e5-e5e5-e5e5-e5e5-e5e5e5e5e5e5"),
			LocalDate: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC),
			Mood:      4, Energy: 3, Wins: "Long run done.",
			UpdatedAt: time.Date(2026, 9, 24, 7, 30, 0, 0, time.UTC),
			CreatedAt: time.Date(2026, 9, 24, 7, 30, 0, 0, time.UTC),
			Source:    checkin.SourceIOS,
		},
		Created: true,
		Run:     sampleRun(),
	}
	m := moments.Moment{Kind: moments.KindComeback, Title: "A fresh start", Body: "New run. Your best is 23 days."}
	apitest.AssertGolden(t, "check-in-saved.golden.json", projectSaved(saved, &m))

	edit := saved
	edit.Created = false
	apitest.AssertGolden(t, "check-in-saved-edit.golden.json", projectSaved(edit, nil))
}

// The app names the surface that filed the check-in, so the widget and the
// notification can be measured. Anything else it sends is recorded as ios.
// "web" is the SvelteKit web app, which files check-ins through this API.
func TestCheckInRequestRecordsTheNativeSurface(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]checkin.Source{
		"siri":         checkin.SourceSiri,
		"widget":       checkin.SourceWidget,
		"notification": checkin.SourceNotification,
		"":             checkin.SourceIOS,
		"web":          checkin.SourceWeb,
		"capture":      checkin.SourceIOS,
	} {
		if got := (CheckInRequest{Mood: 3, Energy: 3, Source: in}).input().Source; got != want {
			t.Errorf("source %q recorded as %q, want %q", in, got, want)
		}
	}
}
