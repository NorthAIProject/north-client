package reports_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

type stubQueue struct {
	kind    jobs.Kind
	payload any
}

func (q *stubQueue) Enqueue(_ context.Context, kind jobs.Kind, payload any) (jobs.Job, error) {
	q.kind = kind
	q.payload = payload
	return jobs.Job{ID: uuid.New(), Kind: kind}, nil
}

func fixture(t *testing.T) (*reports.Service, users.User, users.User, *fake.Client, *stubQueue) {
	t.Helper()
	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))

	create := func(email, name string) users.User {
		u, err := userSvc.Register(context.Background(), users.Registration{
			Email:        email,
			PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
			DisplayName:  name,
			Timezone:     "Europe/Lisbon",
		})
		if err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		return u
	}

	client := &fake.Client{Responses: []fake.Response{{
		Text: "# Week of 10 Aug 2026\n\nYou checked in twice. Nothing else is recorded.\n",
	}}}
	queue := &stubQueue{}
	svc := reports.NewService(reports.Options{
		Repository: reports.NewRepository(pool),
		Users:      userSvc,
		Queue:      queue,
		Client:     client,
		Now:        func() time.Time { return time.Date(2026, 8, 13, 15, 0, 0, 0, time.UTC) },
	})
	return svc, create("fernando@north.test", "Fernando Correia"), create("stranger@north.test", "Someone Else"), client, queue
}

func TestGeneratePersistsUserScopedReport(t *testing.T) {
	svc, user, _, _, queue := fixture(t)
	ctx := context.Background()

	pending, err := svc.RequestGenerate(ctx, user.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != reports.StatusPending {
		t.Fatalf("status = %q", pending.Status)
	}
	if pending.Title != "Week of 10 Aug 2026" {
		t.Fatalf("title = %q", pending.Title)
	}
	if queue.kind != jobs.KindWeeklyReview {
		t.Fatalf("enqueued %q", queue.kind)
	}

	if err = svc.Generate(ctx, pending.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Get(ctx, pending.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != reports.StatusReady {
		t.Fatalf("status = %q", got.Status)
	}
	if !strings.Contains(got.Body, "checked in twice") {
		t.Fatalf("body = %q", got.Body)
	}
}

func TestGenerateRefusesSecondCallInsideCooldown(t *testing.T) {
	svc, user, _, _, _ := fixture(t)
	ctx := context.Background()

	pending, err := svc.RequestGenerate(ctx, user.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Generate(ctx, pending.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	_, err = svc.RequestGenerate(ctx, user.ID, time.Time{})
	if !apperr.Is(err, apperr.ErrConflict) {
		t.Fatalf("got %v, want conflict", err)
	}
}

func TestGenerateDoesNotSeeAnotherUsersData(t *testing.T) {
	svc, user, stranger, _, _ := fixture(t)
	ctx := context.Background()

	pending, err := svc.RequestGenerate(ctx, user.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Get(ctx, pending.ID, stranger.ID)
	if !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("got %v, want not found", err)
	}

	if err = svc.Generate(ctx, pending.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("generate as stranger: %v", err)
	}
}

func TestFailedGenerateMarksFailedAndKeepsPendingSlot(t *testing.T) {
	svc, user, _, client, _ := fixture(t)
	ctx := context.Background()
	client.Responses = []fake.Response{{Err: errors.New("provider down")}}

	pending, err := svc.RequestGenerate(ctx, user.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Generate(ctx, pending.ID, user.ID); err == nil {
		t.Fatal("expected generate to fail")
	}

	got, err := svc.Get(ctx, pending.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != reports.StatusFailed {
		t.Fatalf("status = %q", got.Status)
	}
	if got.ArchivedAt != nil {
		t.Fatal("a failed generate must not archive the slot")
	}

	// After cooldown would not apply (never generated). A retry should reuse the row.
	again, err := svc.RequestGenerate(ctx, user.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != pending.ID {
		t.Fatalf("retry created %s, want the failed row %s", again.ID, pending.ID)
	}
}

func TestListHidesArchivedByDefault(t *testing.T) {
	svc, user, _, _, _ := fixture(t)
	ctx := context.Background()

	pending, err := svc.RequestGenerate(ctx, user.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Archive(ctx, pending.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	list, err := svc.List(ctx, user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("default list = %d, want 0", len(list))
	}

	archived, err := svc.List(ctx, user.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 || !archived[0].Archived() {
		t.Fatalf("archived list = %+v", archived)
	}
}

func TestPromptDoesNotInventWhenContextEmpty(t *testing.T) {
	t.Parallel()

	body, err := prompts.Render(prompts.WeeklyReview, map[string]string{
		"Title":   "Week of 10 Aug 2026",
		"Context": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "you have no recorded week") {
		t.Fatalf("empty context should tell the model not to invent a week, got %q", body)
	}
	if strings.Contains(strings.ToLower(body), "squat") || strings.Contains(strings.ToLower(body), "deadlift") {
		t.Fatalf("empty prompt invented training: %q", body)
	}
}
