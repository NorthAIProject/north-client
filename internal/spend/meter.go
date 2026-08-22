package spend

import (
	"context"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/pricing"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
)

// Meter joins the three halves of a cost record: what the provider reported,
// who the context says it was for, and what pricing says it is worth.
//
// It lives here rather than in internal/ai because it is the only piece that
// needs all three, and internal/ai must not learn about users or prices to make
// a request.
type Meter struct {
	recorder Recorder
}

func NewMeter(r Recorder) *Meter { return &Meter{recorder: r} }

// Record implements ai.Meter.
func (m *Meter) Record(ctx context.Context, provider, model string, usage ai.Usage, byok bool) {
	if m == nil || m.recorder == nil {
		return
	}

	attr := aiattr.From(ctx)

	var userID *uuid.UUID
	if attr.UserID != uuid.Nil {
		id := attr.UserID
		userID = &id
	}

	// A user's own key is their bill. Pricing it with our rates would be a
	// number about nothing, so BYOK calls carry tokens and no cost.
	var cost int64
	if !byok {
		cost, _ = pricing.Cost(provider, model, usage.InputTokens, usage.OutputTokens)
	}

	m.recorder.Record(ctx, Generation{
		UserID:       userID,
		Surface:      attr.Surface,
		Provider:     provider,
		Model:        model,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CostMicros:   cost,
		BYOK:         byok,
	})
}

// Assert the shape internal/ai depends on, so a signature change breaks here
// rather than at wiring time.
var _ ai.Meter = (*Meter)(nil)
