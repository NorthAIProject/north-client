package inbox_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/inbox"
	"github.com/NorthAIProject/north-client/internal/inbox/item"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

type queued struct{ kinds []jobs.Kind }

func (q *queued) Enqueue(_ context.Context, kind jobs.Kind, _ any) (jobs.Job, error) {
	q.kinds = append(q.kinds, kind)
	return jobs.Job{}, nil
}

type pickGoal struct{}

func (pickGoal) Suggest(_ context.Context, _ users.User, _ string, active []goals.Goal) (inbox.Suggestion, error) {
	return inbox.Suggestion{Destination: item.DestinationGoalNote, GoalID: &active[0].ID, GoalTitle: active[0].Title, Why: "progress"}, nil
}

type notes struct{ titles []string }

func (n *notes) CreateNote(_ context.Context, _ uuid.UUID, title, _ string) (documents.Document, error) {
	n.titles = append(n.titles, title)
	return documents.Document{ID: uuid.New()}, nil
}

type journal struct{ entries []string }

func (j *journal) Create(_ context.Context, _ uuid.UUID, in mind.Input) (mind.JournalEntry, error) {
	j.entries = append(j.entries, in.Content)
	return mind.JournalEntry{ID: uuid.New()}, nil
}

type accounts struct{ u users.User }

func (a accounts) ByID(context.Context, uuid.UUID) (users.User, error) { return a.u, nil }

// Saved at once with a suggestion queued; the suggestion arrives; filing
// writes where the person chose and takes it out of the inbox.
func TestInboxLoop(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "inbox@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Ana", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	goalSvc := goals.NewService(goals.NewRepository(pool))
	run, _ := goalSvc.Create(ctx, user.ID, goals.Input{Title: "Run a half marathon", Category: "fitness"})
	q, n, j := &queued{}, &notes{}, &journal{}
	svc := inbox.NewService(pool, inbox.Options{Queue: q, Suggester: pickGoal{}, Goals: goalSvc, Notes: n, Journal: j, Users: accounts{user}})

	ran, err := svc.Add(ctx, user.ID, item.SourceShare, "  Ran 12 km, the knee felt fine ")
	if err != nil || ran.Text != "Ran 12 km, the knee felt fine" || ran.Suggestion != nil {
		t.Fatalf("add = %+v, %v", ran, err)
	}
	if len(q.kinds) != 1 || q.kinds[0] != jobs.KindSuggestInbox {
		t.Fatalf("queued %v", q.kinds)
	}
	link, _ := svc.Add(ctx, user.ID, item.SourceApp, "https://example.com/zone-2\nRead about zone 2")

	if err = svc.Suggest(ctx, user.ID, ran.ID); err != nil {
		t.Fatal(err)
	}
	open, count, _ := svc.Open(ctx, user.ID)
	if count != 2 || len(open) != 2 {
		t.Fatalf("open = %d", count)
	}
	var suggested *inbox.Suggestion
	for _, it := range open {
		if it.ID == ran.ID {
			suggested = it.Suggestion
		}
	}
	if suggested == nil || suggested.GoalID == nil || *suggested.GoalID != run.ID {
		t.Fatalf("suggestion = %+v", suggested)
	}

	if _, err = svc.File(ctx, user.ID, ran.ID, inbox.Filing{Destination: item.DestinationGoalNote, GoalID: run.ID}); err != nil {
		t.Fatal(err)
	}
	updates, _ := goalSvc.Updates(ctx, run.ID, user.ID, 5)
	if len(updates) != 1 || updates[0].Note != "Ran 12 km, the knee felt fine" {
		t.Fatalf("goal notes = %+v", updates)
	}
	if _, err = svc.File(ctx, user.ID, link.ID, inbox.Filing{Destination: item.DestinationKnowledge}); err != nil {
		t.Fatal(err)
	}
	if len(n.titles) != 1 || n.titles[0] != "https://example.com/zone-2" {
		t.Fatalf("note titles = %v", n.titles)
	}
	if _, count, _ = svc.Open(ctx, user.ID); count != 0 {
		t.Fatalf("still open: %d", count)
	}

	// Filing twice, or with no goal, is refused.
	if _, err = svc.File(ctx, user.ID, ran.ID, inbox.Filing{Destination: item.DestinationJournal}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("filed twice: %v", err)
	}
	thought, _ := svc.Add(ctx, user.ID, item.SourceApp, "Felt calm today")
	if _, err = svc.File(ctx, user.ID, thought.ID, inbox.Filing{Destination: item.DestinationGoalNote}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("goal note without a goal: %v", err)
	}
	if err = svc.Dismiss(ctx, user.ID, thought.ID); err != nil {
		t.Fatal(err)
	}
	if len(j.entries) != 0 {
		t.Fatalf("something was filed without being asked: %v", j.entries)
	}
}

func TestAddRejects(t *testing.T) {
	pool := testdb.New(t)
	svc := inbox.NewService(pool, inbox.Options{})
	if _, err := svc.Add(context.Background(), uuid.New(), item.SourceApp, "   "); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := svc.Add(context.Background(), uuid.New(), "fax", "hello"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("source: %v", err)
	}
}
