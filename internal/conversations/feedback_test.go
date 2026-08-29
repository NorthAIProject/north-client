package conversations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func feedbackFixture(t *testing.T) (*conversations.Service, *pgxpool.Pool, users.User, users.User) {
	t.Helper()

	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))

	create := func(email string) users.User {
		u, err := userSvc.Register(context.Background(), users.Registration{
			Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
			DisplayName: "Test", Timezone: "UTC",
		})
		if err != nil {
			t.Fatalf("create %s: %v", email, err)
		}
		return u
	}

	return conversations.NewService(conversations.NewRepository(pool)), pool,
		create("rate-owner@north.test"), create("rate-stranger@north.test")
}

// exchange starts a thread and returns the user's turn and the coach's reply.
func exchange(t *testing.T, svc *conversations.Service, userID uuid.UUID) (conversations.Message, conversations.Message) {
	t.Helper()

	ctx := context.Background()
	thread, err := svc.Start(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}

	mine, err := svc.AppendUserMessage(ctx, thread.ID, "How should I train this week?", nil)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := svc.AppendModelMessage(ctx, thread.ID,
		"Three sessions, and keep Wednesday light.", nil, "fake", "fake", nil)
	if err != nil {
		t.Fatal(err)
	}
	return mine, reply
}

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

func TestRatingACoachReplyPersists(t *testing.T) {
	svc, _, owner, _ := feedbackFixture(t)
	ctx := context.Background()
	_, reply := exchange(t, svc, owner.ID)

	if reply.Rated() {
		t.Fatal("a fresh reply is already rated")
	}

	rated, err := svc.SetMessageHelpful(ctx, reply.ID, owner.ID, yes())
	if err != nil {
		t.Fatal(err)
	}
	if !rated.RatedHelpful() {
		t.Errorf("Helpful = %v, want true", rated.Helpful)
	}

	// And it survives a reload, which is the only thing that proves it was
	// written rather than returned.
	msgs, err := svc.Recent(ctx, reply.ConversationID, 10)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range msgs {
		if m.ID == reply.ID {
			found = true
			if !m.RatedHelpful() {
				t.Errorf("reloaded Helpful = %v, want true", m.Helpful)
			}
		}
	}
	if !found {
		t.Fatal("the rated reply is not in the thread")
	}
}

// Three states, and the third one has to be reachable. Somebody who taps the
// wrong answer and cannot undo it leaves a wrong label in the one column the
// product will later check its own judgement against.
func TestAnAnswerCanBeChangedAndCleared(t *testing.T) {
	svc, _, owner, _ := feedbackFixture(t)
	ctx := context.Background()
	_, reply := exchange(t, svc, owner.ID)

	if _, err := svc.SetMessageHelpful(ctx, reply.ID, owner.ID, yes()); err != nil {
		t.Fatal(err)
	}

	flipped, err := svc.SetMessageHelpful(ctx, reply.ID, owner.ID, no())
	if err != nil {
		t.Fatal(err)
	}
	if !flipped.RatedUnhelpful() {
		t.Errorf("after flipping, Helpful = %v, want false", flipped.Helpful)
	}

	cleared, err := svc.SetMessageHelpful(ctx, reply.ID, owner.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Rated() {
		t.Errorf("after clearing, Helpful = %v, want nil", cleared.Helpful)
	}
}

// Rating your own message is meaningless, and allowing it would put noise in the
// only labelled column the product has.
func TestYourOwnTurnCannotBeRated(t *testing.T) {
	svc, _, owner, _ := feedbackFixture(t)
	ctx := context.Background()
	mine, _ := exchange(t, svc, owner.ID)

	_, err := svc.SetMessageHelpful(ctx, mine.ID, owner.ID, yes())
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("rating a user turn returned %v, want not-found", err)
	}
}

// The ownership check lives in the UPDATE statement, so this is the test that
// proves it is actually there — a message id carries no user id of its own.
func TestAStrangerCannotRateSomebodyElsesReply(t *testing.T) {
	svc, _, owner, stranger := feedbackFixture(t)
	ctx := context.Background()
	_, reply := exchange(t, svc, owner.ID)

	_, err := svc.SetMessageHelpful(ctx, reply.ID, stranger.ID, yes())
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a stranger's rating returned %v, want not-found", err)
	}

	// And nothing was written.
	msgs, err := svc.Recent(ctx, reply.ConversationID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.ID == reply.ID && m.Rated() {
			t.Error("the stranger's rating was stored anyway")
		}
	}
}

func TestRatingAMessageThatDoesNotExist(t *testing.T) {
	svc, _, owner, _ := feedbackFixture(t)

	_, err := svc.SetMessageHelpful(context.Background(), uuid.New(), owner.ID, yes())
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("got %v, want not-found", err)
	}
}
