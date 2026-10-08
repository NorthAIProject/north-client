package workouts_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

// weekTracker answers CompletedWeekdays per Monday, so a test can finish
// different days in different weeks.
type weekTracker struct {
	done map[string][]string
}

func (w *weekTracker) CompletedWeekdays(_ context.Context, _ uuid.UUID, loc *time.Location, now time.Time) ([]string, error) {
	return w.done[activity.WeekStart(now, loc).Format(time.DateOnly)], nil
}

func (w *weekTracker) finish(week time.Time, days ...string) {
	w.done[week.Format(time.DateOnly)] = days
}

// upperLower is the four-day split the feature was asked for.
func upperLower() workouts.Plan {
	day := func(weekday, focus, exercise string) workouts.PlanDay {
		return workouts.PlanDay{Weekday: weekday, Focus: focus, Exercises: []workouts.Exercise{
			{Name: exercise, Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: "dumbbell"},
		}}
	}
	return workouts.Plan{
		Name: "Upper/Lower", Rationale: "Four days, each muscle twice a week.", WeeksTotal: 8,
		Days: []workouts.PlanDay{
			day("Monday", "Upper A", "Dumbbell Bench Press"),
			day("Tuesday", "Lower A", "Dumbbell Goblet Squat"),
			day("Thursday", "Upper B", "Dumbbell Row"),
			day("Friday", "Lower B", "Dumbbell Romanian Deadlift"),
		},
	}
}

// weekFixture is an account with an Upper/Lower plan and a three-day plan,
// following Upper/Lower, and a tracker the test finishes days on.
type weekFixture struct {
	svc        *workouts.Service
	user       users.User
	tracker    *weekTracker
	upperLower workouts.StoredPlan
	fullBody   workouts.StoredPlan
}

func newWeekFixture(t *testing.T) weekFixture {
	t.Helper()
	next := goodPlan()
	client := &fake.Client{}
	client.Handler = func(_ context.Context, _ ai.Request) (fake.Response, error) {
		return fake.Response{Text: planJSON(t, next)}, nil
	}
	svc, user := newService(t, client)
	tracker := &weekTracker{done: map[string][]string{}}
	svc.WithActivity(tracker)

	ctx := context.Background()
	fullBody, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create full body: %v", err)
	}
	next = upperLower()
	intake := dumbbellIntake()
	intake.DaysPerWeek = 4
	ul, err := svc.CreatePlan(ctx, user, intake)
	if err != nil {
		t.Fatalf("create upper/lower: %v", err)
	}
	return weekFixture{svc: svc, user: user, tracker: tracker, upperLower: ul, fullBody: fullBody}
}

func sessions(p workouts.WeekProgress) []string {
	out := make([]string, 0, len(p.Days))
	for _, d := range p.Days {
		out = append(out, d.Weekday+" "+d.Day.Focus)
	}
	return out
}

// lisbon returns a time in the fixture user's zone.
func lisbon(t *testing.T, year int, month time.Month, day, hour int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(year, month, day, hour, 0, 0, 0, loc)
}

func TestTheNewestPlanIsFollowedAndEditingAnotherDoesNotSwitch(t *testing.T) {
	f := newWeekFixture(t)
	ctx := context.Background()

	active, err := f.svc.ActivePlan(ctx, f.user.ID)
	if err != nil || active.IntakeID != f.upperLower.IntakeID {
		t.Fatalf("active = %v %v, want the plan just made", active.Plan.Name, err)
	}

	// Editing the plan not followed used to make it the followed one.
	if _, err := f.svc.SetStartTime(ctx, f.user, f.fullBody.ID, 0, "07:30"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	active, err = f.svc.ActivePlan(ctx, f.user.ID)
	if err != nil || active.IntakeID != f.upperLower.IntakeID {
		t.Fatalf("after editing another plan, active = %v %v", active.Plan.Name, err)
	}

	if _, err := f.svc.SetActivePlan(ctx, f.user, f.fullBody.ID); err != nil {
		t.Fatalf("follow: %v", err)
	}
	active, err = f.svc.ActivePlan(ctx, f.user.ID)
	if err != nil || active.IntakeID != f.fullBody.IntakeID {
		t.Fatalf("active = %v %v, want the plan chosen", active.Plan.Name, err)
	}
	plans, err := f.svc.ListCurrentPlans(ctx, f.user.ID, 0)
	if err != nil || plans[0].IntakeID != f.fullBody.IntakeID {
		t.Fatalf("the followed plan should be listed first: %v", err)
	}
	// The edit is the version followed, not the original.
	if active.Plan.Days[0].StartTime != "07:30" {
		t.Fatalf("followed version should be the newest: %+v", active.Plan.Days[0])
	}
}

func TestAShortWeekCarriesItsLeftoverSessionIntoTheNext(t *testing.T) {
	f := newWeekFixture(t)
	ctx := context.Background()
	monday := lisbon(t, 2026, 10, 5, 9)

	week, err := f.svc.SetWeek(ctx, f.user, monday, workouts.WeekChange{Days: 3})
	if err != nil {
		t.Fatalf("set week: %v", err)
	}
	if got, want := sessions(week), []string{"Monday Upper A", "Wednesday Lower A", "Friday Upper B"}; !slices.Equal(got, want) {
		t.Fatalf("three days = %v, want %v", got, want)
	}
	if !week.Custom {
		t.Fatal("a week someone set is custom")
	}

	f.tracker.finish(monday, "Monday", "Wednesday", "Friday")
	following, err := f.svc.WeekProgress(ctx, f.user, monday.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sessions(following), []string{"Monday Lower B", "Tuesday Upper A", "Thursday Lower A", "Friday Upper B"}; !slices.Equal(got, want) {
		t.Fatalf("next week = %v, want %v", got, want)
	}
	if following.Custom {
		t.Fatal("the week after goes back to the usual days")
	}
}

func TestChangingTheWeekMidwayKeepsWhatWasTrained(t *testing.T) {
	f := newWeekFixture(t)
	ctx := context.Background()
	monday := lisbon(t, 2026, 10, 5, 9)

	if _, err := f.svc.WeekProgress(ctx, f.user, monday); err != nil {
		t.Fatal(err)
	}
	f.tracker.finish(monday, "Monday")

	wednesday := monday.AddDate(0, 0, 2)
	week, err := f.svc.SetWeek(ctx, f.user, wednesday, workouts.WeekChange{
		Weekdays: []string{"Monday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Monday Upper A", "Wednesday Lower A", "Thursday Upper B", "Friday Lower B", "Saturday Upper A", "Sunday Lower A"}
	if got := sessions(week); !slices.Equal(got, want) {
		t.Fatalf("six days = %v, want %v", got, want)
	}
	if !week.Days[0].Completed || week.NextDay.Weekday != "Wednesday" {
		t.Fatalf("Monday done, Wednesday next: %+v", week.NextDay)
	}

	reset, err := f.svc.ResetWeek(ctx, f.user, wednesday, false)
	if err != nil {
		t.Fatal(err)
	}
	if reset.Custom || len(reset.Days) != 4 || reset.Days[0].Day.Focus != "Upper A" {
		t.Fatalf("reset = %v, want the usual four with Monday kept", sessions(reset))
	}
}

func TestADayCanTrainAnotherPlansSession(t *testing.T) {
	f := newWeekFixture(t)
	ctx := context.Background()
	monday := lisbon(t, 2026, 10, 5, 9)

	week, err := f.svc.SetWeek(ctx, f.user, monday, workouts.WeekChange{
		Weekdays: []string{"Tuesday", "Saturday"},
		Assign:   map[string]workouts.SessionRef{"Saturday": {PlanID: f.fullBody.ID, DayIndex: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(week.Days) != 2 || week.Days[1].PlanID != f.fullBody.ID || week.Days[1].Day.Exercises[0].Name != "Dumbbell Row" {
		t.Fatalf("Saturday should be the full-body plan's third day: %+v", week.Days)
	}
	if week.Days[0].Day.Focus != "Upper A" {
		t.Fatalf("Tuesday keeps the rotation: %s", week.Days[0].Day.Focus)
	}

	if _, err := f.svc.SetWeek(ctx, f.user, monday, workouts.WeekChange{
		Weekdays: []string{"Tuesday"},
		Assign:   map[string]workouts.SessionRef{"Tuesday": {PlanID: f.fullBody.ID, DayIndex: 9}},
	}); err == nil {
		t.Fatal("a day the plan does not have should be refused")
	}
}

func TestTheWeekDrivesTodayAndAdherence(t *testing.T) {
	f := newWeekFixture(t)
	ctx := context.Background()
	monday := lisbon(t, 2026, 10, 5, 9)

	if _, err := f.svc.SetWeek(ctx, f.user, monday, workouts.WeekChange{Weekdays: []string{"Wednesday", "Saturday"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, due, _ := f.svc.DueToday(ctx, f.user, monday); due {
		t.Fatal("Monday is not a training day this week")
	}
	title, _, due, err := f.svc.DueToday(ctx, f.user, monday.AddDate(0, 0, 2))
	if err != nil || !due || title != "Upper A" {
		t.Fatalf("Wednesday = %q %v %v, want Upper A", title, due, err)
	}

	focus, sets, ok, err := f.svc.Prescription(ctx, f.user, monday.AddDate(0, 0, 5), "Saturday")
	if err != nil || !ok || focus != "Lower A" || sets != 3 {
		t.Fatalf("Saturday prescription = %q %d %v %v", focus, sets, ok, err)
	}

	schedule, ok, err := f.svc.PlanSchedule(ctx, f.user, timerange.Between(monday.AddDate(0, 0, -7), monday.AddDate(0, 0, 7)))
	if err != nil || !ok {
		t.Fatal(err)
	}
	if got := schedule.For(activity.WeekStart(monday, monday.Location())); len(got) != 2 {
		t.Fatalf("this week counts 2 days, got %+v", got)
	}
	if len(schedule.Default) != 4 {
		t.Fatalf("other weeks count the usual 4, got %+v", schedule.Default)
	}
}
