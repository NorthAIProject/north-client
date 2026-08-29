package messaging_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// A Telegram update id counts up per bot, not globally. Point a deployment at a
// different bot and the new bot's sequence starts from its own low number, so
// every update compares as older than the watermark the previous bot left.
//
// Before account_id existed this classified every message from the new bot as a
// redelivery and dropped it — silently, at INFO, permanently, because a
// watermark can only go up. It cost a live test to find: two real messages were
// discarded while the bot looked healthy in every other respect.
func TestAChangeOfBotStartsANewDeliverySequence(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "bot-change@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Test", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}

	repo := messaging.NewRepository(pool)
	const chat = "476978568"

	if _, err = repo.Insert(ctx, user.ID, messaging.PlatformTelegram, chat); err != nil {
		t.Fatal(err)
	}

	// The old bot gets a long way into its own sequence.
	const oldBot, newBot = "111111111", "8111737206"
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 735928348, oldBot); err != nil {
		t.Fatalf("first claim on the old bot: %v", err)
	}

	// The new bot's ids are much lower. They are not older — they are a
	// different sequence, and must be accepted.
	link, err := repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 400470828, newBot)
	if err != nil {
		t.Fatalf("the new bot's first update was rejected as a redelivery: %v", err)
	}
	if link.AccountID != newBot {
		t.Errorf("AccountID = %q, want the new bot", link.AccountID)
	}
	if link.LastUpdateID != 400470828 {
		t.Errorf("LastUpdateID = %d, want the new sequence's id", link.LastUpdateID)
	}

	// And within the new bot the watermark works exactly as before: forward is
	// accepted, a repeat is not.
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 400470829, newBot); err != nil {
		t.Fatalf("the new bot's second update was rejected: %v", err)
	}
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 400470829, newBot); err == nil {
		t.Error("a genuine redelivery on the new bot was accepted")
	}
}

// The guard must not become a way to replay a message: same bot, same id, twice
// is still a redelivery. This is the property the watermark existed for, and the
// account check must not weaken it.
func TestARedeliveryIsStillRefusedWithinOneBot(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "same-bot@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Test", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}

	repo := messaging.NewRepository(pool)
	const chat, bot = "476978568", "8111737206"

	if _, err = repo.Insert(ctx, user.ID, messaging.PlatformTelegram, chat); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 100, bot); err != nil {
		t.Fatal(err)
	}

	for _, id := range []int64{100, 99, 1} {
		if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, id, bot); err == nil {
			t.Errorf("update %d was accepted on the same bot; it is older than the watermark", id)
		}
	}
}

// A platform with no account notion keeps the behaviour it had before the column
// existed, and a legacy row written before the column keeps working: the first
// claim records the account and resets, then it is monotonic from there.
func TestLegacyRowsAndAccountlessPlatformsStillWork(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "legacy@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Test", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}

	repo := messaging.NewRepository(pool)
	const chat = "999"

	if _, err = repo.Insert(ctx, user.ID, messaging.PlatformTelegram, chat); err != nil {
		t.Fatal(err)
	}

	// Empty account throughout: no reset ever fires, so this is the old
	// monotonic rule exactly.
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 50, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 49, ""); err == nil {
		t.Error("an older update was accepted with no account set")
	}
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 51, ""); err != nil {
		t.Errorf("a newer update was rejected with no account set: %v", err)
	}
}
