package achievements_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
)

type noteSpy struct{ titles []string }

func (n *noteSpy) NoteWithPush(_ context.Context, _ uuid.UUID, _, _, title, _, _ string) error {
	n.titles = append(n.titles, title)
	return nil
}

func person(t *testing.T, pool *pgxpool.Pool, email, name string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: name, Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// follow makes follower follow followee, accepted.
func follow(t *testing.T, svc *social.Service, follower, followee users.User, handle string) {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.SetHandle(ctx, followee.ID, handle); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Follow(ctx, follower.ID, handle); err != nil {
		t.Fatal(err)
	}
	if err := svc.Accept(ctx, followee.ID, follower.ID); err != nil {
		t.Fatal(err)
	}
}

func titles(items []achievements.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

// A follower sees only the categories shared with them; your own feed always
// has everything of yours; a pending follower and a blocked one see nothing.
func TestFeedShowsOnlyWhatIsShared(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ach := achievements.NewService(pool)
	soc := social.NewService(social.NewRepository(pool))
	ana := person(t, pool, "ana@north.test", "Ana")
	leo := person(t, pool, "leo@north.test", "Leo")
	zoe := person(t, pool, "zoe@north.test", "Zoe")
	follow(t, soc, leo, ana, "ana")
	if _, err := soc.Follow(ctx, zoe.ID, "ana"); err != nil { // pending only
		t.Fatal(err)
	}

	now := time.Now()
	ach.WorkoutCompleted(ctx, ana.ID, uuid.New(), "Running", 32, now.Add(-time.Hour))
	ach.GoalCompleted(ctx, ana.ID, uuid.New(), "Run a 10k", now.Add(-2*time.Hour))
	ach.StreakReached(ctx, ana.ID, 7, "2026-09-30", now.Add(-3*time.Hour))

	if got, _ := ach.Feed(ctx, ana.ID, time.Time{}); len(got) != 3 {
		t.Fatalf("own feed = %v, want all three", titles(got))
	}
	if got, _ := ach.Feed(ctx, leo.ID, time.Time{}); len(got) != 0 {
		t.Fatalf("follower sees %v before anything is shared", titles(got))
	}

	if _, err := ach.SetSharing(ctx, ana.ID, achievements.Sharing{Training: true}); err != nil {
		t.Fatal(err)
	}
	got, _ := ach.Feed(ctx, leo.ID, time.Time{})
	if len(got) != 1 || got[0].Title != "Finished Running" || got[0].Detail != "32 min" || got[0].Mine {
		t.Fatalf("follower feed = %+v, want only the shared workout", got)
	}
	if got, _ := ach.Feed(ctx, zoe.ID, time.Time{}); len(got) != 0 {
		t.Fatalf("pending follower sees %v", titles(got))
	}

	if err := soc.Block(ctx, ana.ID, leo.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := ach.Feed(ctx, leo.ID, time.Time{}); len(got) != 0 {
		t.Fatalf("blocked follower sees %v", titles(got))
	}
}

// Streaks are moments only at their marks, and nothing is recorded twice.
func TestRecordingIsSparseAndOnce(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ach := achievements.NewService(pool)
	ana := person(t, pool, "ana@north.test", "Ana")
	session := uuid.New()

	for days := 1; days <= 8; days++ {
		ach.StreakReached(ctx, ana.ID, days, "2026-09-30", time.Now())
	}
	ach.WorkoutCompleted(ctx, ana.ID, session, "Running", 30, time.Now())
	ach.WorkoutCompleted(ctx, ana.ID, session, "Running", 30, time.Now())

	got, _ := ach.Feed(ctx, ana.ID, time.Time{})
	if len(got) != 2 {
		t.Fatalf("recorded %v, want one streak (7) and one workout", titles(got))
	}
}

// A crew challenge met is one moment per crew and week, filed with what the
// challenge counted.
func TestCrewChallengeMetIsOncePerWeek(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ach := achievements.NewService(pool)
	ana := person(t, pool, "ana@north.test", "Ana")
	runners, lifters := uuid.New(), uuid.New()
	week := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

	ach.CrewChallengeMet(ctx, ana.ID, runners, "Runners", "checkins", 5, week, time.Now())
	ach.CrewChallengeMet(ctx, ana.ID, runners, "Runners", "checkins", 5, week, time.Now())
	ach.CrewChallengeMet(ctx, ana.ID, lifters, "Lifters", "workouts", 3, week, time.Now())

	got, _ := ach.Feed(ctx, ana.ID, time.Time{})
	if len(got) != 2 {
		t.Fatalf("recorded %v, want one per crew", titles(got))
	}
	category := map[string]string{}
	for _, it := range got {
		category[it.Title] = it.Category
	}
	if category["Met the Runners challenge"] != "streaks" || category["Met the Lifters challenge"] != "training" {
		t.Fatalf("categories = %v", category)
	}
}

// Kudos go to things you can see that are not yours; the owner hears once.
func TestKudos(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	inbox := &noteSpy{}
	ach := achievements.NewService(pool).WithInbox(inbox)
	soc := social.NewService(social.NewRepository(pool))
	ana := person(t, pool, "ana@north.test", "Ana")
	leo := person(t, pool, "leo@north.test", "Leo")
	stranger := person(t, pool, "stranger@north.test", "Sam")
	follow(t, soc, leo, ana, "ana")
	_, _ = ach.SetSharing(ctx, ana.ID, achievements.Sharing{Training: true})
	ach.WorkoutCompleted(ctx, ana.ID, uuid.New(), "Rowing", 20, time.Now())
	item, _ := ach.Feed(ctx, ana.ID, time.Time{})
	id := item[0].ID

	if err := ach.GiveKudos(ctx, stranger.ID, id, "Sam"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a stranger gave kudos: %v", err)
	}
	if err := ach.GiveKudos(ctx, ana.ID, id, "Ana"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("kudos to yourself: %v", err)
	}
	for range 2 {
		if err := ach.GiveKudos(ctx, leo.ID, id, "Leo"); err != nil {
			t.Fatal(err)
		}
	}
	feed, _ := ach.Feed(ctx, leo.ID, time.Time{})
	if feed[0].Kudos != 1 || !feed[0].Kudoed {
		t.Fatalf("after kudos: %+v", feed[0])
	}
	if len(inbox.titles) != 1 || inbox.titles[0] != "Leo gave you kudos" {
		t.Fatalf("notes = %v", inbox.titles)
	}
	if err := ach.TakeKudos(ctx, leo.ID, id); err != nil {
		t.Fatal(err)
	}
	if feed, _ = ach.Feed(ctx, leo.ID, time.Time{}); feed[0].Kudos != 0 {
		t.Fatalf("kudos not taken back: %+v", feed[0])
	}
}

// Achieving a goal records it, through the goals service itself.
func TestGoalsRecordWhenAchieved(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ach := achievements.NewService(pool)
	goalSvc := goals.NewService(goals.NewRepository(pool)).WithAchievements(ach)
	ana := person(t, pool, "ana@north.test", "Ana")

	g, err := goalSvc.Create(ctx, ana.ID, goals.Input{Title: "Run a 10k", Category: "fitness"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = goalSvc.SetStatus(ctx, g.ID, ana.ID, goals.StatusAchieved); err != nil {
		t.Fatal(err)
	}
	got, _ := ach.Feed(ctx, ana.ID, time.Time{})
	if len(got) != 1 || got[0].Title != "Achieved a goal: Run a 10k" || got[0].Category != "goals" {
		t.Fatalf("feed = %+v", got)
	}
}
