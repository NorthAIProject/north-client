package workouts_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

type fakeActivityTracker struct {
	completed []string
}

func (f *fakeActivityTracker) CompletedWeekdays(context.Context, uuid.UUID, *time.Location, time.Time) ([]string, error) {
	return f.completed, nil
}

func TestContextSource_CompletedAndPending(t *testing.T) {
	today := time.Now().Weekday().String()
	// A plan that passes validation, with its first day moved to today and
	// the others kept off it.
	plan := goodPlan()
	plan.Days[0].Weekday, plan.Days[0].Focus = today, "Upper A"
	others := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
	next := 0
	for i := 1; i < len(plan.Days); i++ {
		for others[next] == today {
			next++
		}
		plan.Days[i].Weekday = others[next]
		next++
	}

	client := &fake.Client{}
	client.Handler = func(_ context.Context, _ ai.Request) (fake.Response, error) {
		return fake.Response{Text: planJSON(t, plan)}, nil
	}
	svc, user := newService(t, client)

	ctx := context.Background()
	_, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// 1. When session is pending
	tracker := &fakeActivityTracker{}
	svc.WithActivity(tracker)
	source := workouts.NewContextSource(svc)

	req := coach.ContextRequest{User: user}
	c := &coach.Context{User: user}
	if err := source.Collect(ctx, req, c); err != nil {
		t.Fatalf("collect context: %v", err)
	}

	if !strings.Contains(c.WorkoutPlan, "PENDING") {
		t.Errorf("expected WorkoutPlan to report PENDING, got:\n%s", c.WorkoutPlan)
	}
	if !strings.Contains(c.WorkoutPlan, "Upper A") {
		t.Errorf("expected WorkoutPlan to mention Upper A, got:\n%s", c.WorkoutPlan)
	}

	// 2. When session is completed
	tracker.completed = []string{today}
	cCompleted := &coach.Context{User: user}
	if err := source.Collect(ctx, req, cCompleted); err != nil {
		t.Fatalf("collect context completed: %v", err)
	}

	if !strings.Contains(cCompleted.WorkoutPlan, "COMPLETED") {
		t.Errorf("expected WorkoutPlan to report COMPLETED, got:\n%s", cCompleted.WorkoutPlan)
	}
}

func TestWeekStatusNamesWhatIsDoneAndWhatIsNext(t *testing.T) {
	t.Parallel()

	p := workouts.Plan{Days: []workouts.PlanDay{
		{Weekday: "Monday", Focus: "Push"},
		{Weekday: "Wednesday", Focus: "Legs"},
		{Weekday: "Friday", Focus: "Pull"},
	}}
	wednesday := time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		progress workouts.WeekProgress
		want     string
	}{
		{
			name:     "today still open",
			progress: workouts.WeekProgress{Completed: []string{"Monday"}, Next: p.Days[1], HasNext: true},
			want:     "This week: Monday (Push) COMPLETED. Next: Wednesday (Legs), today, PENDING.",
		},
		{
			name:     "today done",
			progress: workouts.WeekProgress{Completed: []string{"Monday", "Wednesday"}, Next: p.Days[2], HasNext: true},
			want:     "This week: Monday (Push) COMPLETED; Wednesday (Legs) COMPLETED. Next: Friday (Pull).",
		},
		{
			name:     "week finished",
			progress: workouts.WeekProgress{Completed: []string{"Monday", "Wednesday", "Friday"}, Next: p.Days[0], HasNext: true},
			want:     "This week: Monday (Push) COMPLETED; Wednesday (Legs) COMPLETED; Friday (Pull) COMPLETED. Next: Monday (Push), next week — every session this week is done.",
		},
	}
	for _, tc := range cases {
		if got := workouts.WeekStatus(p, tc.progress, wednesday); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}
