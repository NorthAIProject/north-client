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
	completed bool
	title     string
}

func (f *fakeActivityTracker) CompletedToday(ctx context.Context, userID uuid.UUID, loc *time.Location) (bool, string, error) {
	return f.completed, f.title, nil
}

func TestContextSource_CompletedAndPending(t *testing.T) {
	today := time.Now().Weekday().String()
	plan := workouts.Plan{
		Name: "Test 3-Day",
		Days: []workouts.PlanDay{
			{
				Weekday: today,
				Focus:   "Upper A",
				Exercises: []workouts.Exercise{
					{Name: "Bench Press", Sets: 3, Reps: "8-10"},
				},
			},
		},
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
	tracker := &fakeActivityTracker{completed: false}
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
	tracker.completed = true
	cCompleted := &coach.Context{User: user}
	if err := source.Collect(ctx, req, cCompleted); err != nil {
		t.Fatalf("collect context completed: %v", err)
	}

	if !strings.Contains(cCompleted.WorkoutPlan, "COMPLETED") {
		t.Errorf("expected WorkoutPlan to report COMPLETED, got:\n%s", cCompleted.WorkoutPlan)
	}
}
