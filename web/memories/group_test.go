package memories_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/memories/memory"
	memorypages "github.com/NorthAIProject/north-client/web/memories"
)

func TestGroupApprovedByCategoryOrdersPinnedFirst(t *testing.T) {
	t.Parallel()
	cat := memory.CategoryHabit
	list := []memory.Memory{
		{ID: uuid.New(), Category: cat, Content: "b", Pinned: false},
		{ID: uuid.New(), Category: cat, Content: "a", Pinned: true},
	}

	groups := memorypages.GroupApprovedByCategory(list)
	if len(groups) != 1 {
		t.Fatalf("groups = %d", len(groups))
	}
	if !groups[0].Items[0].Pinned {
		t.Fatal("pinned fact should be first in its group")
	}
}
