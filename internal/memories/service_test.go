package memories_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/memories/extract"
	"github.com/NorthAIProject/north-client/internal/memories/memory"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
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

func TestCreateApprovedAndContext(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "mem@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	m, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryHabit,
		Content:  "Prefers morning training before work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != memories.StatusApproved {
		t.Fatalf("status = %s", m.Status)
	}

	list, err := svc.ForContext(ctx, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("context list = %d", len(list))
	}
}

func TestPendingNotInContextUntilApproved(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "pending@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	n, err := svc.InsertExtractions(ctx, user.ID, uuid.Nil, []extract.Candidate{
		{Category: memory.CategoryEquipment, Content: "Only owns a pair of dumbbells", Confidence: 0.9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("expected pending insert")
	}

	list, err := svc.ForContext(ctx, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("pending must not reach coach context, got %d", len(list))
	}

	pending, err := svc.ListPending(ctx, user.ID)
	if err != nil || len(pending) == 0 {
		t.Fatalf("pending list: %v len=%d", err, len(pending))
	}
	approved, err := svc.Approve(ctx, pending[0].ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != memories.StatusApproved {
		t.Fatal(approved.Status)
	}
	list, err = svc.ForContext(ctx, user.ID, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("after approve: %v len=%d", err, len(list))
	}
}

func TestOwnershipIsolation(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	owner := seedUser(t, pool, "owner-mem@north.test")
	stranger := seedUser(t, pool, "stranger-mem@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	m, err := svc.Create(ctx, owner.ID, memories.Input{
		Category: memories.CategoryGeneral,
		Content:  "Something only the owner should see",
	})
	if err != nil {
		t.Fatal(err)
	}

	assertNotFound := func(label string, err error) {
		t.Helper()
		if !apperr.Is(err, apperr.ErrNotFound) {
			t.Fatalf("%s: %v", label, err)
		}
	}

	if _, err = svc.Get(ctx, m.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger get: %v", err)
	}
	if _, err = svc.Update(ctx, m.ID, stranger.ID, memories.Input{
		Category: memories.CategoryGeneral,
		Content:  "Hijacked content here for test",
	}); err == nil {
		t.Fatal("stranger update should fail")
	} else {
		assertNotFound("stranger update", err)
	}
	if err = svc.Delete(ctx, m.ID, stranger.ID); err == nil {
		t.Fatal("stranger delete should fail")
	} else {
		assertNotFound("stranger delete", err)
	}
	if _, err = svc.SetPinned(ctx, m.ID, stranger.ID, true); err == nil {
		t.Fatal("stranger pin should fail")
	} else {
		assertNotFound("stranger pin", err)
	}
	if _, err = svc.SetExcluded(ctx, m.ID, stranger.ID, true); err == nil {
		t.Fatal("stranger exclude should fail")
	} else {
		assertNotFound("stranger exclude", err)
	}
	pending, err := svc.InsertExtractions(ctx, owner.ID, uuid.Nil, []extract.Candidate{
		{Category: memory.CategoryHabit, Content: "Another private habit for isolation", Confidence: 0.9},
	})
	if err != nil || pending != 1 {
		t.Fatalf("seed pending: n=%d err=%v", pending, err)
	}
	pendingList, err := svc.ListPending(ctx, owner.ID)
	if err != nil || len(pendingList) == 0 {
		t.Fatal(err)
	}
	if _, err = svc.Approve(ctx, pendingList[0].ID, stranger.ID); err == nil {
		t.Fatal("stranger approve should fail")
	} else {
		assertNotFound("stranger approve", err)
	}

	if err = svc.Delete(ctx, m.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Get(ctx, m.ID, owner.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("after delete get: %v", err)
	}
	if err = svc.Delete(ctx, m.ID, owner.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("double delete: %v", err)
	}
}

func TestExcludedFactsStayOutOfCoachContext(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "excluded@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	m, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryPreference,
		Content:  "Prefers short coaching replies over long essays",
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := svc.ForContext(ctx, user.ID, "coaching style")
	if err != nil || len(list) != 1 {
		t.Fatalf("before exclude: %v len=%d", err, len(list))
	}

	excluded, err := svc.SetExcluded(ctx, m.ID, user.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !excluded.Excluded || excluded.Pinned {
		t.Fatalf("excluded=%t pinned=%t", excluded.Excluded, excluded.Pinned)
	}

	list, err = svc.ForContext(ctx, user.ID, "coaching style")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("excluded fact reached context, got %d", len(list))
	}

	approved, err := svc.ListApproved(ctx, user.ID)
	if err != nil || len(approved) != 1 || !approved[0].Excluded {
		t.Fatalf("UI list should still show excluded fact: %v len=%d", err, len(approved))
	}
}

func TestPinAndExcludeAreMutuallyExclusive(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "mutex@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	m, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryHabit,
		Content:  "Trains before breakfast on most weekdays",
	})
	if err != nil {
		t.Fatal(err)
	}

	pinned, err := svc.SetPinned(ctx, m.ID, user.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !pinned.Pinned || pinned.Excluded {
		t.Fatalf("after pin: pinned=%t excluded=%t", pinned.Pinned, pinned.Excluded)
	}

	excluded, err := svc.SetExcluded(ctx, m.ID, user.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !excluded.Excluded || excluded.Pinned {
		t.Fatalf("after exclude: pinned=%t excluded=%t", excluded.Pinned, excluded.Excluded)
	}

	pinnedAgain, err := svc.SetPinned(ctx, m.ID, user.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !pinnedAgain.Pinned || pinnedAgain.Excluded {
		t.Fatalf("after re-pin: pinned=%t excluded=%t", pinnedAgain.Pinned, pinnedAgain.Excluded)
	}
}

func TestDedupExtractions(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "dedup@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	repo := memories.NewRepository(pool)
	if _, err := repo.Create(ctx, user.ID, memories.NewMemory{
		Category: memory.CategoryHabit,
		Content:  "Runs before breakfast",
		Status:   memories.StatusApproved,
		Source:   memories.SourceUser,
	}); err != nil {
		t.Fatal(err)
	}

	n, err := svc.InsertExtractions(ctx, user.ID, uuid.Nil, []extract.Candidate{
		{Category: memory.CategoryHabit, Content: "Runs before breakfast", Confidence: 0.95},
		{Category: memory.CategoryHabit, Content: "Sleeps by 22:00 on weeknights", Confidence: 0.9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 new extraction after dedup, got %d", n)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	_, err := memories.Validate(memories.Input{Content: "short"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListPendingForConversation(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "reflect-mem@north.test")
	svc := memories.NewService(memories.NewRepository(pool))
	convos := conversations.NewService(conversations.NewRepository(pool))

	thisThread, err := convos.StartKind(ctx, user.ID, conversations.KindReflection)
	if err != nil {
		t.Fatal(err)
	}
	otherThread, err := convos.Start(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.InsertExtractions(ctx, user.ID, thisThread.ID, []extract.Candidate{
		{Category: memory.CategoryHabit, Content: "Sleeps badly before deadlines", Confidence: 0.9},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.InsertExtractions(ctx, user.ID, otherThread.ID, []extract.Candidate{
		{Category: memory.CategoryPreference, Content: "Prefers evening training sessions", Confidence: 0.8},
	}); err != nil {
		t.Fatal(err)
	}

	list, err := svc.ListPendingForConversation(ctx, user.ID, thisThread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("pending for this thread = %d", len(list))
	}
	if list[0].Content != "Sleeps badly before deadlines" {
		t.Fatalf("content = %q", list[0].Content)
	}

	if _, err = svc.Approve(ctx, list[0].ID, user.ID); err != nil {
		t.Fatal(err)
	}

	list, err = svc.ListPendingForConversation(ctx, user.ID, thisThread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("approved fact still pending for this thread: %d", len(list))
	}
}
