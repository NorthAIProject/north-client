package conversations_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestStartDefaultsToChat(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "convo-chat@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Test", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := conversations.NewService(conversations.NewRepository(pool)).Start(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != conversations.KindChat {
		t.Fatalf("kind = %q", c.Kind)
	}
	if c.Ended() {
		t.Fatal("a new chat is not ended")
	}
}

func TestStartKindReflectionAndSummaryEndsIt(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "convo-reflect@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Test", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := conversations.NewService(conversations.NewRepository(pool))
	c, err := svc.StartKind(ctx, user.ID, conversations.KindReflection)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsReflection() {
		t.Fatalf("kind = %q", c.Kind)
	}
	if c.Ended() {
		t.Fatal("a new reflection is not ended")
	}
	if sumErr := svc.SetSummary(ctx, c.ID, "You are tired and the deadline is real."); sumErr != nil {
		t.Fatal(sumErr)
	}
	got, err := svc.Get(ctx, c.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ended() {
		t.Fatal("summary should end the session")
	}
	if got.Summary != "You are tired and the deadline is real." {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func TestStartKindRejectsUnknown(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "convo-bad@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Test", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = conversations.NewService(conversations.NewRepository(pool)).StartKind(ctx, user.ID, "dream")
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v", err)
	}
}
