package spend_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	"github.com/NorthAIProject/north-client/internal/spend"
)

// captureRecorder records in memory, so the metering path can be asserted
// without a database.
type captureRecorder struct{ got []spend.Generation }

func (c *captureRecorder) Record(_ context.Context, g spend.Generation) {
	c.got = append(c.got, g)
}

// stubClient answers with fixed usage and a model the "provider" chose, which
// is the case that matters: AI_MODEL ships empty, so the configured model is
// usually nothing and the reported one is all there is.
type stubClient struct {
	name   string
	model  string
	usage  ai.Usage
	chunks int
}

func (s stubClient) Name() string { return s.name }

func (s stubClient) Generate(_ context.Context, _ ai.Request) (*ai.Response, error) {
	return &ai.Response{Text: "done", Model: s.model, Usage: s.usage}, nil
}

func (s stubClient) Chat(_ context.Context, _ ai.Request) (<-chan ai.StreamChunk, error) {
	out := make(chan ai.StreamChunk, s.chunks+1)
	for range s.chunks {
		out <- ai.StreamChunk{Text: "tok"}
	}
	usage := s.usage
	out <- ai.StreamChunk{Usage: &usage, Model: s.model}
	close(out)
	return out, nil
}

func (s stubClient) UploadFile(_ context.Context, _ ai.UploadRequest) (*ai.File, error) {
	return &ai.File{}, nil
}

func metered(t *testing.T, byok bool) (ai.Client, *captureRecorder) {
	t.Helper()
	rec := &captureRecorder{}
	client := ai.Metered(stubClient{
		name:   "openrouter",
		model:  "z-ai/glm-5.2:free",
		usage:  ai.Usage{InputTokens: 1000, OutputTokens: 400},
		chunks: 3,
	}, spend.NewMeter(rec), byok)
	return client, rec
}

func TestGenerateIsRecordedWithTheProvidersOwnModel(t *testing.T) {
	client, rec := metered(t, false)
	id := uuid.New()
	ctx := aiattr.WithUser(context.Background(), id, spend.SurfaceWeeklyReview)

	if _, err := client.Generate(ctx, ai.Request{}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(rec.got) != 1 {
		t.Fatalf("recorded %d generations, want 1", len(rec.got))
	}
	g := rec.got[0]
	if g.Model != "z-ai/glm-5.2:free" {
		t.Errorf("Model = %q; the provider's reported model was dropped", g.Model)
	}
	if g.Surface != spend.SurfaceWeeklyReview {
		t.Errorf("Surface = %q, want %q", g.Surface, spend.SurfaceWeeklyReview)
	}
	if g.UserID == nil || *g.UserID != id {
		t.Errorf("UserID = %v, want %v", g.UserID, id)
	}
	if g.InputTokens != 1000 || g.OutputTokens != 400 {
		t.Errorf("tokens = %d/%d, want 1000/400", g.InputTokens, g.OutputTokens)
	}
	// The stub answers on the free floor, which has a rate of zero. That is a
	// price, not a missing one, and the ledger has to be able to tell.
	if !g.Priced {
		t.Error("Priced = false for a model with a rate of zero; it will be reported as a pricing gap")
	}
	if g.CostMicros != 0 {
		t.Errorf("CostMicros = %d, want 0", g.CostMicros)
	}
}

// The stream carries usage in a final frame. Recording has to wait for it, and
// the caller must still see every chunk.
func TestChatIsRecordedOnceTheStreamEnds(t *testing.T) {
	client, rec := metered(t, false)
	ctx := aiattr.WithUser(context.Background(), uuid.New(), spend.SurfaceCoach)

	stream, err := client.Chat(ctx, ai.Request{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	seen := 0
	for range stream {
		seen++
	}
	if seen != 4 {
		t.Errorf("caller saw %d chunks, want 4; the decorator swallowed some", seen)
	}
	if len(rec.got) != 1 {
		t.Fatalf("recorded %d generations, want 1", len(rec.got))
	}
	if rec.got[0].InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", rec.got[0].InputTokens)
	}
}

// A user's own key is their bill. The tokens are still recorded — they are real
// — but pricing them with our rates would be a number about nothing.
func TestBYOKIsRecordedWithoutACost(t *testing.T) {
	client, rec := metered(t, true)
	ctx := aiattr.WithUser(context.Background(), uuid.New(), spend.SurfaceCoach)

	if _, err := client.Generate(ctx, ai.Request{}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	g := rec.got[0]
	if !g.BYOK {
		t.Error("BYOK = false on a client built from a user credential")
	}
	if g.CostMicros != 0 {
		t.Errorf("CostMicros = %d, want 0 for BYOK", g.CostMicros)
	}
	if g.InputTokens != 1000 {
		t.Errorf("InputTokens = %d; BYOK tokens should still be recorded", g.InputTokens)
	}
}

// An unlabelled call is a wiring gap. It is recorded as nobody's, with the
// surface left for the repository to mark unknown, rather than being guessed
// into someone's bill.
func TestAnUnlabelledCallIsRecordedWithoutAUser(t *testing.T) {
	client, rec := metered(t, false)

	if _, err := client.Generate(context.Background(), ai.Request{}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if rec.got[0].UserID != nil {
		t.Errorf("UserID = %v, want nil for a call nobody labelled", rec.got[0].UserID)
	}
}

// Metering must not change what a caller sees when there is no meter to write
// to — that is the wiring state every test outside this package runs in.
func TestAnAbsentMeterLeavesTheClientAlone(t *testing.T) {
	inner := stubClient{name: "openrouter"}
	if got := ai.Metered(inner, nil, false); got != ai.Client(inner) {
		t.Error("Metered wrapped the client despite having no meter")
	}
}
