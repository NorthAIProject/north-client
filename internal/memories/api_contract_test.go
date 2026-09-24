package memories

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestMemoryShapes(t *testing.T) {
	t.Parallel()

	convo := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	created := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	apitest.AssertGolden(t, "memories.golden.json", MemoryList{
		Pending: []MemoryView{{
			ID: uuid.MustParse("a7a7a7a7-a7a7-a7a7-a7a7-a7a7a7a7a7a7"), Category: "injury",
			Content: "Left knee aches on long downhill runs.", Status: "pending", Source: "extraction",
			SourceConversationID: &convo, CreatedAt: created,
		}},
		Approved: []MemoryView{{
			ID: uuid.MustParse("b8b8b8b8-b8b8-b8b8-b8b8-b8b8b8b8b8b8"), Category: "preference",
			Content: "Trains before work, around 07:00.", Status: "approved", Pinned: true, Source: "user", CreatedAt: created,
		}},
		Categories: Categories,
	})
}
