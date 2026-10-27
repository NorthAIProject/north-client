package checkinreply_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/checkinreply"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// stubWriter stands in for the model. It records what it was asked.
type stubWriter struct {
	text  string
	err   error
	block bool
	calls int
	last  checkinreply.Prompt
}

func (w *stubWriter) Write(ctx context.Context, _ users.User, p checkinreply.Prompt) (string, error) {
	w.calls++
	w.last = p
	if w.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return w.text, w.err
}

type stubQuota struct{ allowed bool }

func (q stubQuota) Consume(context.Context, uuid.UUID, string, quota.Action) (quota.Decision, error) {
	return quota.Decision{Allowed: q.allowed}, nil
}

type fixture struct {
	pool     *pgxpool.Pool
	user     users.User
	checkins *checkins.Service
	repo     *checkins.Repository
	writer   *stubWriter
	svc      *checkinreply.Service
}

func setup(t *testing.T) fixture {
	t.Helper()
	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "reply@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test User",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := checkins.NewRepository(pool)
	checkinSvc := checkins.NewService(repo, nil)
	writer := &stubWriter{text: "You trained on four hours of sleep. Tomorrow could be lighter."}
	svc := checkinreply.NewService(checkinSvc, checkinreply.NewRepository(pool), writer)
	return fixture{pool: pool, user: user, checkins: checkinSvc, repo: repo, writer: writer, svc: svc}
}

func (f fixture) checkIn(t *testing.T, in checkins.Input) {
	t.Helper()
	if _, err := f.checkins.UpsertToday(context.Background(), f.user, in); err != nil {
		t.Fatal(err)
	}
}

func TestNoCheckInMeansNoReply(t *testing.T) {
	f := setup(t)
	if _, ok, err := f.svc.ForToday(context.Background(), f.user); err != nil || ok {
		t.Fatalf("ok=%v err=%v, want no reply and no error", ok, err)
	}
	if f.writer.calls != 0 {
		t.Fatal("nothing to answer should not reach the model")
	}
}

// Reopening the page or the app shows the same note and pays for nothing.
func TestAReplyIsWrittenOnceAndReused(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.checkIn(t, checkins.Input{Mood: 3, Energy: 2, Notes: "slept four hours, trained anyway"})

	first, ok, err := f.svc.ForToday(ctx, f.user)
	if err != nil || !ok || first.Kind != checkinreply.KindDaily || first.Text != f.writer.text {
		t.Fatalf("first = %+v ok=%v err=%v", first, ok, err)
	}
	if !strings.Contains(f.writer.last.Today, "slept four hours") {
		t.Errorf("the model was not shown today's check-in: %q", f.writer.last.Today)
	}
	again, ok, err := f.svc.ForToday(ctx, f.user)
	if err != nil || !ok || again != first {
		t.Fatalf("again = %+v ok=%v err=%v, want the stored note", again, ok, err)
	}
	if f.writer.calls != 1 {
		t.Fatalf("calls = %d, want 1", f.writer.calls)
	}
}

// A changed check-in gets a note about what it now says. Saving the same
// content again is not a change.
func TestAReplyIsRewrittenOnlyWhenTheCheckInChanges(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.checkIn(t, checkins.Input{Mood: 3, Energy: 2, Notes: "tired"})
	if _, _, err := f.svc.ForToday(ctx, f.user); err != nil {
		t.Fatal(err)
	}

	f.checkIn(t, checkins.Input{Mood: 3, Energy: 2, Notes: "tired"})
	if _, _, err := f.svc.ForToday(ctx, f.user); err != nil {
		t.Fatal(err)
	}
	if f.writer.calls != 1 {
		t.Fatalf("an unchanged re-save cost a call: %d", f.writer.calls)
	}

	f.checkIn(t, checkins.Input{Mood: 4, Energy: 4, Notes: "napped, feel better"})
	if _, _, err := f.svc.ForToday(ctx, f.user); err != nil {
		t.Fatal(err)
	}
	if f.writer.calls != 2 || !strings.Contains(f.writer.last.Today, "napped") {
		t.Fatalf("calls = %d, last today %q; want a second note about the new content", f.writer.calls, f.writer.last.Today)
	}
}

func TestAQuotaRefusalMeansNoReply(t *testing.T) {
	f := setup(t)
	f.checkIn(t, checkins.Input{Mood: 3, Energy: 3})
	svc := f.svc.WithQuota(stubQuota{allowed: false})

	if _, ok, err := svc.ForToday(context.Background(), f.user); err != nil || ok {
		t.Fatalf("ok=%v err=%v, want no reply", ok, err)
	}
	if f.writer.calls != 0 {
		t.Fatal("a refused quota should not reach the model")
	}
}

// A failure stores nothing, so the next request tries again.
func TestAFailureMeansNoReplyAndNothingStored(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.checkIn(t, checkins.Input{Mood: 3, Energy: 3})

	f.writer.err = errors.New("provider overloaded")
	if _, ok, err := f.svc.ForToday(ctx, f.user); err != nil || ok {
		t.Fatalf("ok=%v err=%v, want no reply and no error", ok, err)
	}
	f.writer.err = nil
	if _, ok, _ := f.svc.ForToday(ctx, f.user); !ok || f.writer.calls != 2 {
		t.Fatalf("ok=%v calls=%d, want the retry to write a note", ok, f.writer.calls)
	}
}

func TestASlowModelMeansNoReply(t *testing.T) {
	f := setup(t)
	f.checkIn(t, checkins.Input{Mood: 3, Energy: 3})
	f.writer.block = true
	svc := f.svc.WithTimeout(50 * time.Millisecond)

	start := time.Now()
	if _, ok, err := svc.ForToday(context.Background(), f.user); err != nil || ok {
		t.Fatalf("ok=%v err=%v, want no reply", ok, err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the deadline did not stop the wait")
	}
}

// The note is plain text under a card: markdown is stripped, and a reply that
// ignores the length rule is dropped rather than shown.
func TestTheNoteIsCleanedOrDropped(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.checkIn(t, checkins.Input{Mood: 3, Energy: 3})

	f.writer.text = "  **Good** call on the walk.  "
	got, ok, _ := f.svc.ForToday(ctx, f.user)
	if !ok || got.Text != "Good call on the walk." {
		t.Fatalf("got %q ok=%v, want the markdown stripped", got.Text, ok)
	}

	f.checkIn(t, checkins.Input{Mood: 2, Energy: 3})
	f.writer.text = strings.Repeat("too long ", 30)
	if _, ok, _ := f.svc.ForToday(ctx, f.user); ok {
		t.Fatal("a note over 200 characters should be dropped")
	}
}

// On the seventh check-in Go looks for a pattern and hands the model the one
// it found; on the sixth it does not look.
func TestThePatternArrivesOnTheSeventhCheckIn(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	today := checkins.LocalDate(f.user, time.Now())
	good, bad := 5, 1
	for i, sleep := range []*int{&good, &good, &good, &bad, &bad, &bad} {
		energy := 5
		if *sleep == bad {
			energy = 2
		}
		if _, err := f.repo.Upsert(ctx, f.user.ID, checkins.Write{
			LocalDate: today.AddDate(0, 0, -(i + 1)), Mood: 3, Energy: energy, SleepQuality: sleep,
		}); err != nil {
			t.Fatal(err)
		}
	}

	f.checkIn(t, checkins.Input{Mood: 3, Energy: 4, SleepQuality: &good})
	got, ok, err := f.svc.ForToday(ctx, f.user)
	if err != nil || !ok || got.Kind != checkinreply.KindPattern {
		t.Fatalf("got %+v ok=%v err=%v, want a pattern note", got, ok, err)
	}
	if !strings.Contains(f.writer.last.Pattern, "sleep") || len(f.writer.last.Recent) != 6 {
		t.Fatalf("prompt pattern %q, %d recent; want the sleep pattern and six earlier days", f.writer.last.Pattern, len(f.writer.last.Recent))
	}
}

// The coach's context shortens each field to a line; the note under today's
// check-in must read all of it. The end of a long note is where somebody may
// say the thing that matters most.
func TestTheModelReadsAllOfToday(t *testing.T) {
	f := setup(t)
	long := strings.Repeat("Long day at work, gym was fine. ", 18) + "I don't see the point of any of it anymore."
	f.checkIn(t, checkins.Input{Mood: 1, Energy: 1, Notes: long, Wins: strings.Repeat("walked ", 30) + "and called my sister"})
	if _, _, err := f.svc.ForToday(context.Background(), f.user); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"I don't see the point of any of it anymore.", "and called my sister", "mood 1/5"} {
		if !strings.Contains(f.writer.last.Today, want) {
			t.Errorf("the model was not shown %q", want)
		}
	}
}

func TestNoPatternBeforeTheSeventh(t *testing.T) {
	f := setup(t)
	f.checkIn(t, checkins.Input{Mood: 3, Energy: 3})
	got, ok, _ := f.svc.ForToday(context.Background(), f.user)
	if !ok || got.Kind != checkinreply.KindDaily || f.writer.last.Pattern != "" {
		t.Fatalf("got %+v, pattern %q; want a daily note with no pattern", got, f.writer.last.Pattern)
	}
}
