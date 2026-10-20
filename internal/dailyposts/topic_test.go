package dailyposts

import (
	"strings"
	"testing"
	"time"
)

var monday = time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)

func TestPickTopicSkipsThemesUsedInTheLastTwoWeeks(t *testing.T) {
	t.Parallel()

	var earlier []Earlier
	for _, th := range themes[:repeatWindow] {
		earlier = append(earlier, Earlier{Theme: th.Key})
	}
	for d := range 30 {
		got := PickTopic(monday.AddDate(0, 0, d), earlier, nil, nil)
		if usedIn(earlier, got.Theme.Key, repeatWindow) {
			t.Fatalf("day %d picked %q, used in the last %d posts", d, got.Theme.Key, repeatWindow)
		}
	}
}

func TestPickTopicAvoidsYesterdayWhenEveryThemeWasUsed(t *testing.T) {
	t.Parallel()

	earlier := make([]Earlier, 0, len(themes))
	for _, th := range themes {
		earlier = append(earlier, Earlier{Theme: th.Key})
	}
	// A window wider than the rotation: every theme counts as recent.
	for d := range 30 {
		got := PickTopic(monday.AddDate(0, 0, d), append(earlier, earlier...), nil, nil)
		if got.Theme.Key == earlier[0].Theme {
			t.Fatalf("day %d repeated yesterday's theme %q", d, got.Theme.Key)
		}
	}
}

func TestPickTopicFollowsASignalUnlessItWasJustUsed(t *testing.T) {
	t.Parallel()

	signal := &Signal{Theme: "sleep", Note: "short nights"}
	got := PickTopic(monday, nil, signal, nil)
	if got.Theme.Key != "sleep" || got.Signal != "short nights" {
		t.Fatalf("got %q with signal %q, want sleep from the signal", got.Theme.Key, got.Signal)
	}

	got = PickTopic(monday, []Earlier{{Theme: "walking"}, {Theme: "sleep"}}, signal, nil)
	if got.Theme.Key == "sleep" || got.Signal != "" {
		t.Fatalf("got %q with signal %q; sleep was two days ago", got.Theme.Key, got.Signal)
	}

	got = PickTopic(monday, nil, signal, []string{"sleep"})
	if got.Theme.Key == "sleep" {
		t.Fatal("a theme already tried today was picked again")
	}
}

func TestSignalNeedsEnoughPeople(t *testing.T) {
	t.Parallel()

	short := Aggregate{SleepPeople: minSignalPeople - 1, AvgSleepMinutes: 300}
	if _, ok := SignalFrom(short); ok {
		t.Fatal("a signal from fewer than the minimum number of people")
	}
	short.SleepPeople = minSignalPeople
	s, ok := SignalFrom(short)
	if !ok || s.Theme != "sleep" {
		t.Fatalf("got %+v, %v; want a sleep signal", s, ok)
	}
	if strings.ContainsAny(s.Note, "0123456789") {
		t.Fatalf("signal note %q carries a number", s.Note)
	}
	if _, ok := SignalFrom(Aggregate{CheckInPeople: 50, AvgEnergy: 3.6, AvgMood: 3.8, SleepPeople: 50, AvgSleepMinutes: 450}); ok {
		t.Fatal("an ordinary week produced a signal")
	}
}

func TestMomentNeverNamesASeason(t *testing.T) {
	t.Parallel()

	for d := range 366 {
		m := strings.ToLower(moment(monday.AddDate(0, 0, d)))
		for _, season := range []string{"spring", "summer", "autumn", "fall", "winter"} {
			if strings.Contains(m, season) {
				t.Fatalf("moment %q names a season", m)
			}
		}
	}
	if got := moment(monday); got != "Monday, the start of the week, 19 October" {
		t.Fatalf("moment = %q", got)
	}
}
