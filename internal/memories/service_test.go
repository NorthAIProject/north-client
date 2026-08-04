package memories_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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

	list, err := svc.ForContext(ctx, user.ID)
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

	list, err := svc.ForContext(ctx, user.ID)
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
	list, err = svc.ForContext(ctx, user.ID)
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
	if _, err := svc.Get(ctx, m.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger get: %v", err)
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
