package reports_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// TestInsightsContextReadsTheRecordedWeek is the regression this loader exists
// for. Reports shipped with ContextLoader unimplemented, so every generated
// review described an empty week no matter what the person had logged. A test
// that only checked "generation succeeded" passed throughout.
func TestInsightsContextReadsTheRecordedWeek(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "week@example.com",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Week Reader",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	goalSvc := goals.NewService(goals.NewRepository(pool))
	checkinSvc := checkins.NewService(checkins.NewRepository(pool), goalSvc)
	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))
	sleepSvc := sleep.NewService(sleep.NewRepository(pool))
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))

	if _, err = goalSvc.Create(ctx, user.ID, goals.Input{
		Title:    "Run a half marathon",
		Success:  "Finish under two hours",
		Category: "fitness",
	}); err != nil {
		t.Fatalf("create goal: %v", err)
	}

	if _, err = checkinSvc.UpsertToday(ctx, user, checkins.Input{
		Mood: 4, Energy: 3, Wins: "Ran 8km before work",
	}); err != nil {
		t.Fatalf("check in: %v", err)
	}

	if _, err = hydrationSvc.Log(ctx, user, 750); err != nil {
		t.Fatalf("log water: %v", err)
	}

	if _, err = sleepSvc.LogToday(ctx, user, sleep.Input{DurationMinutes: 430}); err != nil {
		t.Fatalf("log sleep: %v", err)
	}

	loader := reports.NewInsightsContext(
		insights.NewService(insights.Options{
			CheckIns:  checkinSvc,
			Hydration: hydrationSvc,
			Sleep:     sleepSvc,
			Habits:    habits.NewService(habits.NewRepository(pool)),
			Goals:     goalSvc,
			Mind:      mind.NewService(mind.NewRepository(pool), checkinSvc),
			Activity:  activity.NewService(activity.NewRepository(pool), biometricSvc),
		}),
		nil,
		memories.NewService(memories.NewRepository(pool)),
	)

	week := reports.WeekContaining(time.Now(), user.Location())
	review, err := loader.Load(ctx, user, week.Start, week.End)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	sections := map[string][]string{
		"Goals":     review.Goals,
		"CheckIns":  review.CheckIns,
		"Hydration": review.Hydration,
		"Sleep":     review.Sleep,
	}
	for name, lines := range sections {
		if len(lines) == 0 {
			t.Errorf("%s is empty, but the week has one recorded", name)
		}
	}

	if !strings.Contains(strings.Join(review.Goals, "\n"), "Run a half marathon") {
		t.Errorf("goals do not name the goal: %v", review.Goals)
	}
	if !strings.Contains(strings.Join(review.CheckIns, "\n"), "Ran 8km before work") {
		t.Errorf("check-ins do not carry the win: %v", review.CheckIns)
	}
}

// TestInsightsContextToleratesUnwiredSlices keeps the loader from becoming a
// reason a deployment cannot boot. A missing slice should cost that section,
// not the review.
func TestInsightsContextToleratesUnwiredSlices(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "sparse@example.com",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Sparse",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	loader := reports.NewInsightsContext(nil, nil, nil)

	week := reports.WeekContaining(time.Now(), user.Location())
	review, err := loader.Load(ctx, user, week.Start, week.End)
	if err != nil {
		t.Fatalf("load with nothing wired: %v", err)
	}
	if len(review.Goals) != 0 || len(review.CheckIns) != 0 {
		t.Errorf("expected an empty review, got %+v", review)
	}
}
