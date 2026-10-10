package insights_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Workouts yesterday and the day before, and a check-in today, are three
// active days in a row; the check-in streak is the check-in screen's own.
func TestConsistencyReadsWorkoutsAndCheckIns(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "consistency@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Ana", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	bio := biometrics.NewService(biometrics.NewRepository(pool))
	if _, err = bio.Record(ctx, user.ID, biometrics.Input{
		WeightKg: 70, HeightCm: 170, DateOfBirth: time.Now().AddDate(-30, 0, 0), Sex: biometrics.SexFemale,
	}); err != nil {
		t.Fatal(err)
	}
	activitySvc := activity.NewService(activity.NewRepository(pool), bio)
	checkinSvc := checkins.NewService(checkins.NewRepository(pool), nil)

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for _, back := range []int{1, 2} {
		if _, err = activitySvc.Log(ctx, user.ID, activity.LogInput{
			ActivityCode: "running_fast", StartedAt: today.AddDate(0, 0, -back).Add(7 * time.Hour), Duration: 30 * time.Minute,
		}); err != nil {
			t.Fatalf("log run: %v", err)
		}
	}
	if _, err = checkinSvc.UpsertToday(ctx, user, checkins.Input{Mood: 4, Energy: 4}); err != nil {
		t.Fatalf("check in: %v", err)
	}

	svc := insights.NewService(insights.Options{Activity: activitySvc, CheckIns: checkinSvc})
	c, err := svc.Consistency(ctx, user, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.CurrentStreak != 3 || c.LongestStreak != 3 || c.CheckInStreak != 1 {
		t.Errorf("streaks = current %d longest %d check-in %d", c.CurrentStreak, c.LongestStreak, c.CheckInStreak)
	}
	last := c.Days[len(c.Days)-1]
	if !last.CheckedIn || last.Trained || !c.Days[len(c.Days)-2].Trained {
		t.Errorf("last two days = %+v, %+v", c.Days[len(c.Days)-2], last)
	}
	if !strings.HasPrefix(c.Sentence(), "Consistency: active ") || !strings.Contains(c.Sentence(), "current streak 3 days") {
		t.Errorf("sentence = %q", c.Sentence())
	}
}
