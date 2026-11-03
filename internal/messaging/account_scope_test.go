package messaging_test

import (
	"context"
	"sync"
	"testing"
	"time"

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

	// And within the new bot a forward update is accepted and a repeat is not.
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 400470829, newBot); err != nil {
		t.Fatalf("the new bot's second update was rejected: %v", err)
	}
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 400470829, newBot); err == nil {
		t.Error("a genuine redelivery on the new bot was accepted")
	}
}

// The guard must not become a way to replay a message: same bot, same id, twice
// is still a redelivery, whatever order ids arrive in.
func TestARedeliveryIsStillRefusedWithinOneBot(t *testing.T) {
	repo, chat, bot, ctx := linkedChat(t, "same-bot@north.test")

	if _, err := repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 100, bot); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 100, bot); err == nil {
		t.Error("update 100 was accepted twice on the same bot")
	}
}

// Updates are answered concurrently, and a photo is downloaded before it is
// claimed, so a text sent after it can claim first. The photo is not a
// redelivery: it was never answered. Under the old high-water mark it was
// discarded silently, and an album lost every picture that lost the race.
func TestAnUpdateClaimedOutOfOrderIsStillAnswered(t *testing.T) {
	repo, chat, bot, ctx := linkedChat(t, "out-of-order@north.test")

	if _, err := repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 102, bot); err != nil {
		t.Fatal(err)
	}
	link, err := repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 101, bot)
	if err != nil {
		t.Fatalf("an older, never-answered update was refused as a redelivery: %v", err)
	}
	if link.LastUpdateID != 102 {
		t.Errorf("LastUpdateID = %d, want the highest id answered (102)", link.LastUpdateID)
	}
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 101, bot); err == nil {
		t.Error("the out-of-order update was accepted a second time")
	}
}

// Two deliveries of one update racing each other: exactly one may win.
func TestConcurrentDeliveriesOfOneUpdateClaimOnce(t *testing.T) {
	repo, chat, bot, ctx := linkedChat(t, "racing@north.test")

	const tries = 8
	wins := make(chan bool, tries)
	var wg sync.WaitGroup
	for range tries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 500, bot)
			wins <- err == nil
		}()
	}
	wg.Wait()
	close(wins)

	n := 0
	for won := range wins {
		if won {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d deliveries claimed update 500, want exactly 1", n)
	}
}

// Claimed updates are only kept for as long as a platform might resend them.
func TestOldClaimsArePurged(t *testing.T) {
	repo, chat, bot, ctx := linkedChat(t, "purge@north.test")

	if _, err := repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 7, bot); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.PurgeSeenUpdates(ctx, time.Hour); err != nil || n != 0 {
		t.Fatalf("purge of a fresh claim removed %d (err %v), want 0", n, err)
	}
	if n, err := repo.PurgeSeenUpdates(ctx, 0); err != nil || n != 1 {
		t.Fatalf("purge removed %d (err %v), want 1", n, err)
	}
}

func linkedChat(t *testing.T, email string) (*messaging.PostgresRepository, string, string, context.Context) {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()

	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
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
	return repo, chat, bot, ctx
}

// A platform with no account notion, and a legacy row written before the
// column, are claimed under the empty account like any other.
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

	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 50, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 50, ""); err == nil {
		t.Error("a repeated update was accepted with no account set")
	}
	if _, err = repo.ClaimUpdate(ctx, messaging.PlatformTelegram, chat, 51, ""); err != nil {
		t.Errorf("a newer update was rejected with no account set: %v", err)
	}
}
