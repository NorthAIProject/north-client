package workouts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
	"github.com/NorthAIProject/north-client/web/shared/workoutsummary"
)

// A finished day has to read as finished on the web the same way it does on
// the phone, and the recap has to be there to say what finishing it meant.

func threeDays() plan.Plan {
	p := planWith(gobletSquat())
	p.Days = []plan.PlanDay{
		{Weekday: "Monday", Focus: "Push", Exercises: []plan.Exercise{gobletSquat()}},
		{Weekday: "Wednesday", Focus: "Legs", Exercises: []plan.Exercise{gobletSquat()}},
		{Weekday: "Friday", Focus: "Pull", Exercises: []plan.Exercise{gobletSquat()}},
	}
	return p
}

func renderView(t *testing.T, v PlanView) string {
	t.Helper()
	var b strings.Builder
	if err := PlanPage(users.User{DisplayName: "Ada"}, v).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// dayCard is the markup of one day card, found by its DOM id.
func dayCard(t *testing.T, html string, index int) string {
	t.Helper()
	start := strings.Index(html, `id="`+dayDOMID(index)+`"`)
	if start < 0 {
		t.Fatalf("day %d not rendered", index)
	}
	rest := html[start:]
	if end := strings.Index(rest, `id="`+dayDOMID(index+1)+`"`); end > 0 {
		return rest[:end]
	}
	return rest
}

func TestPlanPageBadgesTheFinishedDayAndTheNextOne(t *testing.T) {
	t.Parallel()

	html := renderView(t, PlanView{
		ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Plan: threeDays(),
		CreatedAt: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
		Completed: map[int]bool{0: true}, Next: 1, Active: true,
	})

	monday, wednesday, friday := dayCard(t, html, 0), dayCard(t, html, 1), dayCard(t, html, 2)
	if !strings.Contains(monday, `data-testid="day-completed"`) || strings.Contains(monday, `data-testid="day-next"`) {
		t.Error("Monday should read Completed and only Completed")
	}
	if !strings.Contains(wednesday, `data-testid="day-next"`) || strings.Contains(wednesday, `data-testid="day-completed"`) {
		t.Error("Wednesday should read Next")
	}
	if strings.Contains(friday, `data-testid="day-`) {
		t.Error("Friday should carry no status")
	}
	if strings.Contains(html, `data-testid="workout-recap"`) {
		t.Error("no recap without one")
	}
}

func TestPlanPageLeadsWithThisWeeksRecap(t *testing.T) {
	t.Parallel()

	earlier := []lift.Set{{ExerciseName: "Goblet squat", WeightKg: 20, Reps: 10, PerformedAt: time.Date(2026, 9, 21, 18, 0, 0, 0, time.UTC)}}
	current := []lift.Set{{ExerciseName: "Goblet squat", WeightKg: 24, Reps: 10, PerformedAt: time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)}}
	rc := lift.BuildRecap(40*time.Minute, 250, 3, current, earlier)
	rc.PlanWeekday, rc.Focus = "Monday", "Push"
	card := workoutsummary.NewRecap("plan-recap", rc, time.UTC)

	html := renderView(t, PlanView{
		ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Plan: threeDays(),
		Completed: map[int]bool{0: true}, Next: 1, Active: true, Recap: &card,
	})

	for _, want := range []string{`data-testid="workout-recap"`, "Monday · Push", rc.Sentence, "This session", "Last time"} {
		if !strings.Contains(html, want) {
			t.Errorf("recap missing %q", want)
		}
	}
}

func TestPlanPageShowsTheWeekOnTheFollowedPlanAndOffersToFollowAnyOther(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	week := WeekView{Start: monday, Custom: true, Days: []WeekDayView{
		{Weekday: "Tuesday", Date: monday.AddDate(0, 0, 1), Focus: "Push", Completed: true},
		{Weekday: "Saturday", Date: monday.AddDate(0, 0, 5), Focus: "Legs", IsNext: true},
	}}
	followed := renderView(t, PlanView{
		ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Plan: threeDays(),
		Next: -1, Active: true, Week: &week,
	})
	for _, want := range []string{`data-testid="training-week"`, "Tue 6 Oct", "Sat 10 Oct", "changed from your usual week", "Back to your usual week"} {
		if !strings.Contains(followed, want) {
			t.Errorf("followed plan page missing %q", want)
		}
	}

	other := renderView(t, PlanView{ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Plan: threeDays(), Next: -1})
	if !strings.Contains(other, "Follow this plan") || strings.Contains(other, `data-testid="training-week"`) {
		t.Error("another plan's page should offer to follow it, without the week")
	}
}

func TestWeekEditorRendersEveryChoice(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	plans := []PlanSummary{
		{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Name: "Upper/Lower", Days: 4},
		{ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Name: "Full body", Days: 3},
	}
	v := WeekView{
		Start: monday, Source: plans[0].ID, Plans: plans, Matching: plans[1:],
		Sessions: []SessionOption{{Value: SessionValue(plans[1].ID, 0), Label: "Full body · A"}},
		Days: []WeekDayView{
			{Weekday: "Monday", Date: monday, Focus: "Upper A", Completed: true},
			{Weekday: "Wednesday", Date: monday.AddDate(0, 0, 2), Focus: "Lower A"},
			{Weekday: "Friday", Date: monday.AddDate(0, 0, 4), Focus: "A", Pinned: SessionValue(plans[1].ID, 0)},
		},
	}
	var b strings.Builder
	if err := WeekEditor(v).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, want := range []string{
		`name="weekday"`, `value="Wednesday"`, `name="session_Wednesday"`, `name="session_Friday"`,
		"Next in your plan · Lower A", "Full body", "is built for 3 days", "Use it this week", `&#34;days&#34;: &#34;3&#34;`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("editor missing %q", want)
		}
	}
	if strings.Contains(html, `name="session_Monday"`) {
		t.Error("a finished day is history, not a choice")
	}
}
