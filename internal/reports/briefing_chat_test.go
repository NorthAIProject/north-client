package reports_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/nudges/nudge"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

type noteCall struct{ kind, dedupe, href string }

type stubInbox struct{ notes []noteCall }

func (s *stubInbox) NoteWithPush(_ context.Context, _ uuid.UUID, kind, dedupe, _, _, href string) error {
	s.notes = append(s.notes, noteCall{kind, dedupe, href})
	return nil
}

// The morning briefing lands in the latest chat as the coach speaking first,
// captioned "Daily briefing", and the bell note opens that thread.
func TestBriefingIsAppendedToTheLatestChat(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "briefing-chat@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}

	convos := conversations.NewService(conversations.NewRepository(pool))
	older, err := convos.Start(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := convos.Start(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Activity, not creation, decides "latest": the older thread is the one
	// last spoken in.
	if _, err = convos.AppendUserMessage(ctx, latest.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = convos.AppendUserMessage(ctx, older.ID, "back to this one", nil); err != nil {
		t.Fatal(err)
	}

	inbox := &stubInbox{}
	svc := reports.NewService(reports.Options{
		Repository: reports.NewRepository(pool),
		Users:      userSvc,
		Queue:      &stubQueue{},
		Client:     &fake.Client{Responses: []fake.Response{{Text: "Sleep more. Lift today."}}},
		Inbox:      inbox,
		Chats:      convos,
		Now:        func() time.Time { return time.Date(2026, 8, 13, 7, 0, 0, 0, time.UTC) },
	})

	day := reports.DayContaining(time.Date(2026, 8, 13, 7, 0, 0, 0, time.UTC), time.UTC)
	pending, _, err := svc.EnsureBriefing(ctx, user.ID, day)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Generate(ctx, pending.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	history, err := convos.History(ctx, older.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := history[len(history)-1]
	if last.Content != "Sleep more. Lift today." {
		t.Fatalf("latest chat ends with %q, want the briefing", last.Content)
	}
	if !last.IsProactive() || last.SourceLabel != conversations.SourceDailyBriefing {
		t.Errorf("provenance = %q/%q, want proactive/Daily briefing", last.Origin, last.SourceLabel)
	}

	if len(inbox.notes) != 1 {
		t.Fatalf("bell notes = %d, want 1", len(inbox.notes))
	}
	note := inbox.notes[0]
	if note.kind != nudge.KindBriefingReady {
		t.Errorf("kind = %q, want briefing_ready", note.kind)
	}
	if note.href != "/app/chat/"+older.ID.String() {
		t.Errorf("href = %q, want the chat it was posted into", note.href)
	}
}

// With no chat yet, the briefing starts one rather than going nowhere.
func TestBriefingStartsAThreadWhenThereIsNone(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "briefing-first@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	convos := conversations.NewService(conversations.NewRepository(pool))
	inbox := &stubInbox{}
	svc := reports.NewService(reports.Options{
		Repository: reports.NewRepository(pool),
		Users:      userSvc,
		Queue:      &stubQueue{},
		Client:     &fake.Client{Responses: []fake.Response{{Text: "Good morning."}}},
		Inbox:      inbox,
		Chats:      convos,
		Now:        func() time.Time { return time.Date(2026, 8, 13, 7, 0, 0, 0, time.UTC) },
	})

	day := reports.DayContaining(time.Date(2026, 8, 13, 7, 0, 0, 0, time.UTC), time.UTC)
	pending, _, err := svc.EnsureBriefing(ctx, user.ID, day)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Generate(ctx, pending.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	list, err := convos.List(ctx, user.ID, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("threads = %d (%v), want 1 new one", len(list), err)
	}
	if inbox.notes[0].href != "/app/chat/"+list[0].ID.String() {
		t.Errorf("href = %q", inbox.notes[0].href)
	}
}
