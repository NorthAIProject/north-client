package app

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

func keys(items []TodayItem) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Key
	}
	return out
}

func englishCtx() context.Context {
	return i18n.WithLocale(context.Background(), string(users.LocaleEN))
}

// A rest day with no water target is check-in and sleep, and nothing else:
// the card must not offer things that cannot be done today.
func TestTodayListsOnlyWhatCanBeDoneToday(t *testing.T) {
	items := todayItems(englishCtx(), DashboardData{
		NextSession: &plan.PlanDay{Weekday: "Thursday", Focus: "Pull"},
		PlanID:      uuid.New(),
	})
	if got := strings.Join(keys(items), ","); got != "checkin,sleep" {
		t.Fatalf("rest day items = %s, want checkin,sleep", got)
	}
}

func TestTodayIncludesTheSessionOnAPlanDay(t *testing.T) {
	planID := uuid.New()
	items := todayItems(englishCtx(), DashboardData{
		NextSession:  &plan.PlanDay{Weekday: "Tuesday", Focus: "Push", StartTime: "18:30", Exercises: make([]plan.Exercise, 5)},
		PlanID:       planID,
		SessionToday: true,
		Hydration:    HydrationView{TargetML: 2000, TodayML: 500},
	})
	if got := strings.Join(keys(items), ","); got != "checkin,train,water,sleep" {
		t.Fatalf("plan day items = %s", got)
	}
	train := items[1]
	if train.Label != "Train: Push" || train.Detail != "18:30 · 5 exercises" {
		t.Errorf("train item = %q / %q", train.Label, train.Detail)
	}
	if train.Href != "/app/training/"+planID.String() {
		t.Errorf("train href = %q", train.Href)
	}
}

// The next step is the first thing not done, in list order — not the first
// thing in the list.
func TestNextIsTheFirstItemNotDone(t *testing.T) {
	items := todayItems(englishCtx(), DashboardData{
		CheckedInToday: true,
		NextSession:    &plan.PlanDay{Weekday: "Tuesday", Focus: "Push"},
		SessionToday:   true,
	})
	next, ok := nextTodayItem(items)
	if !ok || next.Key != TodayTrain {
		t.Fatalf("next = %q (ok %v), want train", next.Key, ok)
	}
	if doneCount(items) != 1 {
		t.Errorf("done = %d, want 1", doneCount(items))
	}
}

func TestWaterIsDoneAtTheTarget(t *testing.T) {
	items := todayItems(englishCtx(), DashboardData{Hydration: HydrationView{TargetML: 2000, TodayML: 2000}})
	for _, item := range items {
		if item.Key == TodayWater && !item.Done {
			t.Fatal("water at target is not done")
		}
	}
}

func TestTodayCardOffersReflectionWhenEverythingIsDone(t *testing.T) {
	html := renderDashboardIn(t, users.LocaleEN, DashboardData{
		Range:          weekRange(),
		CheckedInToday: true,
		Sleep:          SleepView{Logged: true, DurationMinutes: 450},
	})
	if !strings.Contains(html, "That is today done.") {
		t.Error("all-done card does not say so")
	}
	if !strings.Contains(html, `name="kind" value="reflection"`) {
		t.Error("all-done card does not offer the reflection")
	}
	if strings.Contains(html, "Next:") {
		t.Error("all-done card still names a next step")
	}
}

func TestTodayCardNamesTheNextStep(t *testing.T) {
	html := renderDashboardIn(t, users.LocaleEN, DashboardData{Range: weekRange()})
	if !strings.Contains(html, "Next: Check in") {
		t.Error("today card does not name check-in as next")
	}
	if !strings.Contains(html, `data-today-item="checkin"`) {
		t.Error("today card does not list check-in")
	}
}

// A fresh account gets the first-run card alone. The push step is hidden until
// script confirms the browser can deliver, so it must not take Today with it.
func TestTodayCardYieldsToTheFirstRunCard(t *testing.T) {
	firstRun := renderDashboardIn(t, users.LocaleEN, DashboardData{
		Range:       weekRange(),
		HasNextStep: true,
		NextStep:    NextStep{Kind: "goal", Title: "Name one thing you are working toward", CTA: "Add a goal", Href: "/app/goals"},
	})
	if strings.Contains(firstRun, `data-today="true"`) {
		t.Error("today card shown beside the first-run card")
	}

	push := renderDashboardIn(t, users.LocaleEN, DashboardData{
		Range:       weekRange(),
		HasNextStep: true,
		NextStep:    NextStep{Kind: NextStepKindPush, Title: "Turn on notifications", CTA: "Turn on", Href: "#"},
	})
	if !strings.Contains(push, `data-today="true"`) {
		t.Error("push step hid the today card")
	}
}

// A finished session has already moved NextSession on to a later day, so the
// Today card takes the session from DoneToday and shows it done.
func TestTodayShowsTheFinishedSessionAsDone(t *testing.T) {
	items := todayItems(englishCtx(), DashboardData{
		DoneToday:   &plan.PlanDay{Weekday: "Tuesday", Focus: "Push"},
		NextSession: &plan.PlanDay{Weekday: "Thursday", Focus: "Pull"},
	})
	for _, item := range items {
		if item.Key == TodayTrain {
			if !item.Done || item.Label != "Train: Push" {
				t.Fatalf("train item = %q done=%v, want Push done", item.Label, item.Done)
			}
			return
		}
	}
	t.Fatal("finished session missing from today")
}
