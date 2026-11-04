package conversations_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// created_at comes from now(), which can repeat, so paging on it alone could skip
// or repeat a row at the seam. Every row here shares one timestamp, the worst case.
func TestPagesAgreeWhereTimestampsTie(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	u, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email:        "ties@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Ties",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := conversations.NewRepository(pool)
	c, err := repo.Create(ctx, u.ID, "", conversations.KindChat)
	if err != nil {
		t.Fatal(err)
	}

	const total = 7
	for range total {
		if _, err = repo.Append(ctx, conversations.NewMessage{ConversationID: c.ID, Role: ai.RoleUser, Content: "turn"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE messages SET created_at = '2026-11-04T10:00:00Z' WHERE conversation_id = $1`, c.ID); err != nil {
		t.Fatal(err)
	}

	seen := map[uuid.UUID]bool{}
	page, err := repo.Recent(ctx, c.ID, 3)
	for err == nil && len(page) > 0 {
		for _, m := range page {
			if seen[m.ID] {
				t.Errorf("message %s came back twice", m.ID)
			}
			seen[m.ID] = true
		}
		page, err = repo.RecentBefore(ctx, c.ID, page[0].ID, 3)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != total {
		t.Errorf("paged through %d messages, want %d", len(seen), total)
	}
}
