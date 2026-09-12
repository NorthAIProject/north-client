package fitness

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
)

// The bug this page shipped with: the handler set Unavailable, a comment
// promised the template would say the activities could not be read, and the
// template never tested for it. A failed read rendered as "nothing imported
// yet" — telling someone their history was empty when it was merely
// unreadable.
func TestUnavailableIsNotMistakenForEmpty(t *testing.T) {
	t.Parallel()

	got := trendState(strava.Status{Configured: true, Connected: true, Unavailable: true}, strava.Trend{})

	if got == stateEmpty {
		t.Fatal("an unreadable history rendered as an empty one")
	}
	if got != stateUnavailable {
		t.Errorf("state = %v, want stateUnavailable", got)
	}
}

func TestTrendStates(t *testing.T) {
	t.Parallel()

	busy := strava.Trend{Sessions: 3}
	quiet := strava.Trend{}

	tests := []struct {
		name   string
		status strava.Status
		trend  strava.Trend
		want   trendStateKind
	}{
		{"no credentials on the server", strava.Status{}, busy, stateUnconfigured},
		{"configured but not connected", strava.Status{Configured: true}, busy, stateDisconnected},
		{"connected with activities", strava.Status{Configured: true, Connected: true}, busy, stateChart},
		{"connected with nothing imported", strava.Status{Configured: true, Connected: true}, quiet, stateEmpty},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := trendState(tt.status, tt.trend); got != tt.want {
				t.Errorf("state = %v, want %v", got, tt.want)
			}
		})
	}
}

// The sentence is the thing that makes the chart mean anything. These pin what
// it actually says, because "shows no progress or change" was the complaint
// that started this.
func TestTheSentenceSaysWhatIsHappening(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		trend strava.Trend
		want  string
	}{
		{
			"a build week",
			strava.Trend{Shape: strava.ShapeBuilding, RecentMinutes: 400, Ratio: 1.33},
			"6h 40m this week — a third above your normal.",
		},
		{
			"steady",
			strava.Trend{Shape: strava.ShapeSteady, RecentMinutes: 300, Ratio: 1.02},
			"5h this week — about what you normally do.",
		},
		{
			"backed off",
			strava.Trend{Shape: strava.ShapeEasier, RecentMinutes: 150, Ratio: 0.6},
			"2h 30m this week — a half below your normal, an easier week.",
		},
		{
			"nothing at all",
			strava.Trend{Shape: strava.ShapeNoHistory},
			"Nothing recorded in this window yet.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := trendSentence(tt.trend); got != tt.want {
				t.Errorf("sentence = %q, want %q", got, tt.want)
			}
		})
	}
}

// A first fortnight gets a statement, never a verdict.
func TestAShortHistoryIsToldTheTruth(t *testing.T) {
	t.Parallel()

	got := trendSentence(strava.Trend{Shape: strava.ShapeTooNew, RecentMinutes: 95})

	if strings.Contains(got, "normal") && !strings.Contains(got, "start comparing") {
		t.Errorf("a two-week account was compared to a normal it does not have: %q", got)
	}
	if !strings.Contains(got, "1h 35m") {
		t.Errorf("sentence does not state what was actually done: %q", got)
	}
}

// One week is not a streak, it is a week.
func TestTheStreakLineOnlyAppearsWhenItIsWorthSaying(t *testing.T) {
	t.Parallel()

	if got := streakSentence(strava.Trend{StreakWeeks: 1}); got != "" {
		t.Errorf("a single week was announced as a streak: %q", got)
	}
	if got := streakSentence(strava.Trend{StreakWeeks: 9}); got != "9 weeks running without a gap." {
		t.Errorf("streak line = %q", got)
	}
}

// The unit that started all of this. MET-minutes were the vertical axis of the
// scene this replaced and the one figure on every row of the list, and nobody
// could read either. They are an internal idea now, if they are anything, and
// they must not reach a reader.
func TestMETMinutesNeverReachTheReader(t *testing.T) {
	t.Parallel()

	session := strava.Session{
		StravaID: 1, Name: "Morning loop", Sport: "Run",
		DistanceM: 10000, MovingTimeS: 3000,
		StartedAt: time.Date(2026, 9, 9, 7, 0, 0, 0, time.UTC),
	}
	page := strava.SessionPage{Page: 1, PerPage: 10, TotalPages: 1, Total: 1, Sessions: []strava.Session{session}}

	html := render(t, ActivitySessions(page, time.UTC))
	for _, banned := range []string{"MET", "met_min", "MET-min"} {
		if strings.Contains(html, banned) {
			t.Errorf("%q reached the page:\n%s", banned, html)
		}
	}
}

// The list is a flat log now, so every row has to carry its own date: there is
// no day heading above it to inherit one from.
func TestSessionRowsCarryTheirOwnDateAndLoad(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 9, 9, 7, 0, 0, 0, time.UTC)
	page := strava.SessionPage{
		Page: 1, PerPage: 10, TotalPages: 1, Total: 1,
		Sessions: []strava.Session{{
			StravaID:    987654,
			Name:        "Morning loop",
			Sport:       "Run",
			DistanceM:   10200,
			MovingTimeS: 3000,
			StartedAt:   started,
		}},
	}

	html := render(t, ActivitySessions(page, time.UTC))

	for _, want := range []string{
		"Morning loop",
		"Wed 9 Sep 07:00",
		"10.2 km",
		"https://www.strava.com/activities/987654",
		`data-day="2026-09-09"`,
		"1\u20131 of 1",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("session row is missing %q:\n%s", want, html)
		}
	}
}

// A route with no Strava id is not a link to nowhere.
func TestSessionWithoutAStravaIDIsNotLinked(t *testing.T) {
	t.Parallel()

	route := strava.Session{Name: "Logged by hand", Sport: "Workout"}

	if html := render(t, sessionRow(route, time.UTC)); strings.Contains(html, "strava.com/activities") {
		t.Errorf("linked a session that has no Strava id:\n%s", html)
	}
}

// The list renders the order the service handed it. The query sorts newest
// first; the template must not quietly reverse it.
func TestSessionsRenderInTheOrderGiven(t *testing.T) {
	t.Parallel()

	page := strava.SessionPage{
		Page: 1, PerPage: 10, TotalPages: 1, Total: 2,
		Sessions: []strava.Session{
			{Name: "Newer", Sport: "Run", StartedAt: time.Date(2026, 9, 10, 7, 0, 0, 0, time.UTC)},
			{Name: "Older", Sport: "Run", StartedAt: time.Date(2026, 9, 9, 7, 0, 0, 0, time.UTC)},
		},
	}

	html := render(t, ActivitySessions(page, time.UTC))
	if strings.Index(html, "Newer") > strings.Index(html, "Older") {
		t.Error("the list reordered the page it was given")
	}
}

// Turning a page must not rebuild the terrain: the scene is a WebGL context
// with weeks resident on the GPU. Every control swaps the list alone, and
// still carries a real href for a reader without JavaScript.
func TestPagerSwapsOnlyTheListAndStaysLinkable(t *testing.T) {
	t.Parallel()

	page := strava.SessionPage{Page: 2, PerPage: 10, TotalPages: 4, Total: 37}
	html := render(t, sessionsPager(page))

	for _, want := range []string{
		`hx-target="#activity-sessions"`,
		`hx-get="/app/fitness/activities/sessions?page=3"`,
		`hx-push-url="/app/fitness/activities?page=3"`,
		`href="/app/fitness/activities?page=3"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("pager is missing %q:\n%s", want, html)
		}
	}
}

// The ends of the run have nowhere to go, and a control that looks live but
// reloads the same page is worse than one that says it is spent.
func TestPagerStopsAtBothEnds(t *testing.T) {
	t.Parallel()

	first := strava.SessionPage{Page: 1, PerPage: 10, TotalPages: 3, Total: 25}
	if !strings.Contains(render(t, sessionsPager(first)), "disabled") {
		t.Error("page one offers a newer page")
	}
	if first.HasPrevious() {
		t.Error("page one reports a previous page")
	}

	last := strava.SessionPage{Page: 3, PerPage: 10, TotalPages: 3, Total: 25}
	if last.HasNext() {
		t.Error("the last page reports an older page")
	}
}

func TestSessionPageArithmetic(t *testing.T) {
	t.Parallel()

	// 37 sessions, ten to a page: the last page holds seven, and says so.
	last := strava.SessionPage{
		Page: 4, PerPage: 10, TotalPages: 4, Total: 37,
		Sessions: make([]strava.Session, 7),
	}
	if got := last.First(); got != 31 {
		t.Errorf("First() = %d, want 31", got)
	}
	if got := last.Last(); got != 37 {
		t.Errorf("Last() = %d, want 37", got)
	}

	// An empty page counts from nothing rather than from one.
	empty := strava.SessionPage{Page: 1, PerPage: 10}
	if empty.First() != 0 || empty.Last() != 0 {
		t.Errorf("an empty page reported rows %d–%d", empty.First(), empty.Last())
	}
}

// The run of numbers: short runs whole, long runs elided around the current
// page, and never a gap of exactly one — an ellipsis hiding a single number is
// wider than the number.
func TestSessionPageNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		page strava.SessionPage
		want []int
	}{
		{"one page needs no pager", strava.SessionPage{Page: 1, TotalPages: 1}, nil},
		{"short runs show whole", strava.SessionPage{Page: 3, TotalPages: 5}, []int{1, 2, 3, 4, 5}},
		{"near the start", strava.SessionPage{Page: 2, TotalPages: 20}, []int{1, 2, 3, ellipsis, 20}},
		{"in the middle", strava.SessionPage{Page: 10, TotalPages: 20}, []int{1, ellipsis, 9, 10, 11, ellipsis, 20}},
		{"near the end", strava.SessionPage{Page: 19, TotalPages: 20}, []int{1, ellipsis, 18, 19, 20}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sessionPageNumbers(tt.page)
			if len(got) != len(tt.want) {
				t.Fatalf("pages = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("pages = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf strings.Builder
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}
