package onboarding_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/firstweek"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
)

func answers(template string) onboarding.Answers {
	return onboarding.Answers{
		FocusAreas:        []string{lifedomain.Fitness},
		CoachingStyle:     onboarding.StyleText(onboarding.StyleDirect),
		NearTermGoal:      "Run a 10K",
		GoalCategory:      lifedomain.Fitness,
		FirstWeekTemplate: template,
	}
}

// Every client that completes onboarding gets a week, including the shipped
// iOS build that never sends a template: its week runs on the goal the person
// typed. A template that is sent picks the week's daily action instead.
func TestCompletingOnboardingStartsTheFirstWeek(t *testing.T) {
	cases := map[string]string{
		"":         firstweek.TemplateCustom, // the iOS 1.0 request
		"sleep":    firstweek.TemplateSleep,
		"meditate": firstweek.TemplateCustom, // unknown to this server
	}
	for sent, want := range cases {
		t.Run("template="+sent, func(t *testing.T) {
			pool := testdb.New(t)
			ctx := context.Background()
			user := seedUser(t, pool, "first-week@north.test")
			goalSvc := goals.NewService(goals.NewRepository(pool))
			week := firstweek.NewService(firstweek.Options{
				Repository: firstweek.NewRepository(pool), Goals: goalSvc, Enabled: true,
			})
			svc := newSvc(pool).WithFirstWeek(week)

			if _, _, err := svc.Complete(ctx, user, answers(sent)); err != nil {
				t.Fatalf("complete: %v", err)
			}
			today, ok, err := week.Today(ctx, user)
			if err != nil || !ok {
				t.Fatalf("no first week after onboarding: ok=%v err=%v", ok, err)
			}
			if today.Programme.Template != want {
				t.Errorf("template = %q, want %q", today.Programme.Template, want)
			}
			if today.Programme.GoalID == nil {
				t.Fatal("the week has no goal")
			}
			g, err := goalSvc.Get(ctx, *today.Programme.GoalID, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if g.Title != "Run a 10K" {
				t.Errorf("the week's goal is %q, want the one the person named", g.Title)
			}
		})
	}
}

func TestNoFirstWeekWhileTheSwitchIsOff(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "first-week-off@north.test")
	week := firstweek.NewService(firstweek.Options{Repository: firstweek.NewRepository(pool)})
	svc := newSvc(pool).WithFirstWeek(week)

	if svc.FirstWeekEnabled() {
		t.Error("FirstWeekEnabled with the switch off")
	}
	if _, _, err := svc.Complete(ctx, user, answers("sleep")); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, ok, _ := week.Today(ctx, user); ok {
		t.Error("a first week started with the switch off")
	}
}

