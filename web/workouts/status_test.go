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
		Completed: []string{"Monday"}, Next: "Wednesday",
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
		Completed: []string{"Monday"}, Next: "Wednesday", Recap: &card,
	})

	for _, want := range []string{`data-testid="workout-recap"`, "Monday · Push", rc.Sentence, "This session", "Last time"} {
		if !strings.Contains(html, want) {
			t.Errorf("recap missing %q", want)
		}
	}
}
