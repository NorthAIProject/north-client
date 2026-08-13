package dashboard_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/memories/extract"
	"github.com/NorthAIProject/north-client/internal/memories/memory"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func newDashboard(t *testing.T, pool *pgxpool.Pool) *dashboard.Service {
	t.Helper()
	goalSvc := goals.NewService(goals.NewRepository(pool))
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	return dashboard.NewService(dashboard.Options{
		CheckIns:      checkins.NewService(checkins.NewRepository(pool), goalSvc),
		Goals:         goalSvc,
		Conversations: conversations.NewService(conversations.NewRepository(pool)),
		Workouts:      workouts.NewService(workouts.Options{Repository: workouts.NewRepository(pool)}),
		Memories:      memories.NewService(memories.NewRepository(pool)),
		Habits:        habits.NewService(habits.NewRepository(pool)),
		Hydration:     hydration.NewService(hydration.NewRepository(pool)),
		Sleep:         sleep.NewService(sleep.NewRepository(pool)),
		Activity:      activity.NewService(activity.NewRepository(pool), biometricSvc),
	})
}

func TestLoadEmptyNewUser(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "empty@north.test")
	svc := newDashboard(t, pool)

	snap, err := svc.Load(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if snap.CheckedInToday {
		t.Fatal("new user has not checked in")
	}
	if snap.Streak != 0 {
		t.Fatalf("streak = %d", snap.Streak)
	}
	if snap.PendingMemories != 0 {
		t.Fatalf("pending = %d", snap.PendingMemories)
	}
	if len(snap.Goals) != 0 {
		t.Fatalf("goals = %d", len(snap.Goals))
	}
	if snap.LastThread != nil {
		t.Fatal("expected no thread")
	}
	if snap.NextSession != nil || snap.PlanID != uuid.Nil {
		t.Fatal("expected no plan")
	}
	if snap.CheckIns.HasData() {
		t.Fatal("expected empty check-in series")
	}
	if len(snap.CheckIns.Days) != 14 {
		t.Fatalf("check-in window = %d want 14", len(snap.CheckIns.Days))
	}
	if snap.Habits.HasHabits {
		t.Fatal("expected no habits")
	}
	if snap.Hydration.HasData() {
		t.Fatal("expected empty hydration")
	}
	if len(snap.Hydration.Days) != 7 {
		t.Fatalf("hydration window = %d want 7", len(snap.Hydration.Days))
	}
	if snap.Sleep.Logged {
		t.Fatal("expected no sleep")
	}
}

func TestLoadAssemblesToday(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "today@north.test")
	stranger := seedUser(t, pool, "stranger-dash@north.test")
	svc := newDashboard(t, pool)

	goalSvc := goals.NewService(goals.NewRepository(pool))
	checkinSvc := checkins.NewService(checkins.NewRepository(pool), goalSvc)
	convoSvc := conversations.NewService(conversations.NewRepository(pool))
	memorySvc := memories.NewService(memories.NewRepository(pool))
	workoutRepo := workouts.NewRepository(pool)

	if _, err := checkinSvc.UpsertToday(ctx, user, checkins.Input{Mood: 4, Energy: 4, Wins: "walked"}); err != nil {
		t.Fatal(err)
	}

	for _, title := range []string{"Run a 10k", "Sleep by 23:00", "Ship the dashboard", "Ignored fourth"} {
		if _, err := goalSvc.Create(ctx, user.ID, goals.Input{
			Title:    title,
			Category: goals.CategoryPersonal,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := goalSvc.Create(ctx, stranger.ID, goals.Input{
		Title:    "Not yours",
		Category: goals.CategoryPersonal,
	}); err != nil {
		t.Fatal(err)
	}

	thread, err := convoSvc.Start(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = convoSvc.SetTitle(ctx, thread.ID, "Morning training"); err != nil {
		t.Fatal(err)
	}

	if _, err = memorySvc.InsertExtractions(ctx, user.ID, thread.ID, []extract.Candidate{
		{Category: memory.CategoryHabit, Content: "Trains early before work hours", Confidence: 0.9},
	}); err != nil {
		t.Fatal(err)
	}

	intake, err := workoutRepo.CreateIntake(ctx, user.ID, workouts.Intake{
		Goal:           "general strength",
		Experience:     "beginner",
		DaysPerWeek:    3,
		SessionMinutes: 45,
		Equipment:      []string{"dumbbell"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := workoutRepo.CreatePlan(ctx, workouts.StoredPlan{
		UserID:   user.ID,
		IntakeID: intake.ID,
		Plan: workouts.Plan{
			Name: "Dumbbell foundations", Rationale: "Three full-body days.", WeeksTotal: 8,
			Days: []workouts.PlanDay{
				{Weekday: "Monday", Focus: "full body", Exercises: []workouts.Exercise{
					{Name: "Goblet Squat", Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: "dumbbell"},
				}},
				{Weekday: "Wednesday", Focus: "full body", Exercises: []workouts.Exercise{
					{Name: "Push-up", Sets: 3, Reps: "AMRAP", RestSeconds: 90, Equipment: "none"},
				}},
				{Weekday: "Friday", Focus: "full body", Exercises: []workouts.Exercise{
					{Name: "Dumbbell Row", Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: "dumbbell"},
				}},
			},
		},
		Model:    "test",
		Provider: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	snap, err := svc.Load(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.CheckedInToday {
		t.Fatal("expected today's check-in")
	}
	if snap.Streak < 1 {
		t.Fatalf("streak = %d", snap.Streak)
	}
	if snap.PendingMemories != 1 {
		t.Fatalf("pending = %d", snap.PendingMemories)
	}
	if len(snap.Goals) != 3 {
		t.Fatalf("goals capped at 3, got %d", len(snap.Goals))
	}
	for _, g := range snap.Goals {
		if g.Title == "Not yours" {
			t.Fatal("stranger goal leaked")
		}
	}
	if snap.LastThread == nil || snap.LastThread.ID != thread.ID {
		t.Fatalf("last thread = %+v", snap.LastThread)
	}
	if snap.LastThread.DisplayTitle() != "Morning training" {
		t.Fatalf("title = %q", snap.LastThread.DisplayTitle())
	}
	if snap.PlanID != stored.ID {
		t.Fatalf("plan id = %s want %s", snap.PlanID, stored.ID)
	}
	if snap.NextSession == nil {
		t.Fatal("expected a next session")
	}

	other, err := svc.Load(ctx, stranger)
	if err != nil {
		t.Fatal(err)
	}
	if other.CheckedInToday {
		t.Fatal("stranger has not checked in")
	}
	if len(other.Goals) != 1 || other.Goals[0].Title != "Not yours" {
		t.Fatalf("stranger goals = %+v", other.Goals)
	}
	if other.LastThread != nil {
		t.Fatal("stranger has no thread")
	}
	if other.PlanID == stored.ID {
		t.Fatal("stranger saw the owner's plan")
	}
}

func TestLoadCheckInAndHydrationSeries(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "series@north.test")
	checkinRepo := checkins.NewRepository(pool)
	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))
	svc := newDashboard(t, pool)

	today := checkins.LocalDate(user, time.Now())
	for i := range 14 {
		mood := (i % 5) + 1
		if _, err := checkinRepo.Upsert(ctx, user.ID, checkins.Write{
			LocalDate: today.AddDate(0, 0, -i),
			Mood:      mood,
			Energy:    mood,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := hydrationSvc.Log(ctx, user, hydration.Glass); err != nil {
		t.Fatal(err)
	}

	snap, err := svc.Load(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.CheckIns.Days) != 14 {
		t.Fatalf("check-in days = %d", len(snap.CheckIns.Days))
	}
	withMood := 0
	for _, d := range snap.CheckIns.Days {
		if d.Mood > 0 {
			withMood++
		}
	}
	if withMood != 14 {
		t.Fatalf("mood points = %d want 14", withMood)
	}
	if len(snap.Hydration.Days) != 7 {
		t.Fatalf("hydration days = %d", len(snap.Hydration.Days))
	}
	zeros := 0
	for _, d := range snap.Hydration.Days {
		if d.TotalML == 0 {
			zeros++
		}
	}
	if zeros != 6 {
		t.Fatalf("hydration zero-fill = %d want 6", zeros)
	}
	if snap.Hydration.TodayML != hydration.Glass {
		t.Fatalf("today ml = %d", snap.Hydration.TodayML)
	}
}
