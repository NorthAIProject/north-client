package conversations_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	"github.com/NorthAIProject/north-client/internal/spend"
)

// attributionGenerator keeps the label each model call arrived with.
type attributionGenerator struct {
	got []aiattr.Attribution
}

func (g *attributionGenerator) Generate(ctx context.Context, _ ai.Request) (*ai.Response, error) {
	g.got = append(g.got, aiattr.From(ctx))
	return &ai.Response{Text: "They are training for a marathon."}, nil
}

// A worker job has no request to inherit a label from, so an unwrapped
// compaction was billed to nobody under "unknown".
func TestASummarizeJobIsAttributedToTheThreadsOwner(t *testing.T) {
	gen := &attributionGenerator{}
	convos, sum, u := summaryFixture(t, gen, 2)
	ctx := context.Background()

	c, err := convos.Start(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	fill(t, convos, c.ID, 6)

	payload, err := json.Marshal(jobs.SummarizeConversationPayload{UserID: u.ID, ConversationID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = sum.HandleSummarizeJob(ctx, payload); err != nil {
		t.Fatalf("job: %v", err)
	}

	if len(gen.got) != 1 {
		t.Fatalf("model calls = %d, want 1", len(gen.got))
	}
	if gen.got[0].UserID != u.ID || gen.got[0].Surface != spend.SurfaceSummary {
		t.Errorf("attribution = %+v, want %s on %q", gen.got[0], u.ID, spend.SurfaceSummary)
	}
}
