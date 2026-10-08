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

	monday := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	wednesday := monday.AddDate(0, 0, 2).Add(18 * time.Hour)
	session := func(offset int, weekday, focus string, done bool) workouts.WeekDay {
		return workouts.WeekDay{
			Slot: workouts.Slot{Weekday: weekday}, Date: monday.AddDate(0, 0, offset),
			Day: workouts.PlanDay{Weekday: weekday, Focus: focus}, Completed: done,
		}
	}
	week := func(custom bool, next workouts.WeekDay, days ...workouts.WeekDay) workouts.WeekProgress {
		return workouts.WeekProgress{Start: monday, Custom: custom, Days: days, Next: next.Day, NextDay: next, HasNext: true}
	}

	cases := []struct {
		name      string
		progress  workouts.WeekProgress
		planStart time.Time
		want      string
	}{
		{
			name: "today still open",
			progress: week(false, session(2, "Wednesday", "Legs", false),
				session(0, "Monday", "Push", true), session(2, "Wednesday", "Legs", false), session(4, "Friday", "Pull", false)),
			want: "This week: Monday (Push) COMPLETED. Next: Wednesday (Legs), today, PENDING.",
		},
		{
			name: "today done",
			progress: week(false, session(4, "Friday", "Pull", false),
				session(0, "Monday", "Push", true), session(2, "Wednesday", "Legs", true), session(4, "Friday", "Pull", false)),
			want: "This week: Monday (Push) COMPLETED; Wednesday (Legs) COMPLETED. Next: Friday (Pull).",
		},
		{
			name: "week finished",
			progress: week(false, session(7, "Monday", "Push", false),
				session(0, "Monday", "Push", true), session(2, "Wednesday", "Legs", true), session(4, "Friday", "Pull", true)),
			want: "This week: Monday (Push) COMPLETED; Wednesday (Legs) COMPLETED; Friday (Pull) COMPLETED. Next: Monday (Push), next week — every session this week is done.",
		},
		{
			name: "a changed week says which days it trains",
			progress: week(true, session(3, "Thursday", "Legs", false),
				session(1, "Tuesday", "Push", true), session(3, "Thursday", "Legs", false)),
			want: "This week: Tuesday (Push) COMPLETED. They changed this week to 2 training days: Tuesday (Push), Thursday (Legs). Next: Thursday (Legs).",
		},
		{
			name: "earlier day skipped",
			progress: week(false, session(2, "Wednesday", "Legs", false),
				session(0, "Monday", "Push", false), session(2, "Wednesday", "Legs", false), session(4, "Friday", "Pull", false)),
			want: "This week: no plan day completed yet. MISSED: Monday (Push). Next: Wednesday (Legs), today, PENDING.",
		},
		{
			name: "plan made after the skipped day",
			progress: week(false, session(2, "Wednesday", "Legs", false),
				session(0, "Monday", "Push", false), session(2, "Wednesday", "Legs", false), session(4, "Friday", "Pull", false)),
			planStart: monday.AddDate(0, 0, 1),
			want:      "This week: no plan day completed yet. Next: Wednesday (Legs), today, PENDING.",
		},
	}
	for _, tc := range cases {
		if got := workouts.WeekStatus(tc.progress, wednesday, tc.planStart); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}
