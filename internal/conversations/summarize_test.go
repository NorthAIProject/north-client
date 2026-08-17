package conversations_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// stubGenerator records what it was asked and answers with a fixed line.
type stubGenerator struct {
	reply   string
	err     error
	prompts []string
}

func (g *stubGenerator) Generate(_ context.Context, req ai.Request) (*ai.Response, error) {
	for _, m := range req.Messages {
		g.prompts = append(g.prompts, m.Text())
	}
	if g.err != nil {
		return nil, g.err
	}
	return &ai.Response{Text: g.reply}, nil
}

func summaryFixture(t *testing.T, gen conversations.Generator, keep int) (*conversations.Service, *conversations.Summarizer, users.User) {
	t.Helper()
	pool := testdb.New(t)

	convos := conversations.NewService(conversations.NewRepository(pool))
	userSvc := users.NewService(users.NewRepository(pool))

	u, err := userSvc.Register(context.Background(), users.Registration{
		Email:        fmt.Sprintf("summary-%s@north.test", uuid.NewString()[:8]),
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}

	s := conversations.NewSummarizer(conversations.SummarizerOptions{
		Conversations: convos,
		Client:        gen,
		KeepRecent:    keep,
	})
	return convos, s, u
}

// fill writes n alternating turns.
func fill(t *testing.T, convos *conversations.Service, id uuid.UUID, n int) {
	t.Helper()
	ctx := context.Background()
	for i := range n {
		var err error
		if i%2 == 0 {
			_, err = convos.AppendUserMessage(ctx, id, fmt.Sprintf("user turn %d", i), nil)
		} else {
			_, err = convos.AppendModelMessage(ctx, id, fmt.Sprintf("coach turn %d", i), nil, "m", "p", nil)
		}
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
}

// A thread shorter than the tail has lost nothing, so there is nothing to say.
func TestSummarizeSkipsShortThreads(t *testing.T) {
	gen := &stubGenerator{reply: "should not be called"}
	convos, sum, u := summaryFixture(t, gen, 10)
	ctx := context.Background()

	c, err := convos.Start(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	fill(t, convos, c.ID, 4)

	if err = sum.Summarize(ctx, c.ID); err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if len(gen.prompts) != 0 {
		t.Fatalf("called the model for a short thread")
	}

	got, err := convos.Get(ctx, c.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.HasContextSummary() {
		t.Fatal("wrote a summary for a thread that has lost nothing")
	}
}

// The turns still in the tail must not be summarised: the model reads them
// verbatim, and paying to compact them too is the bug this guards.
func TestSummarizeCoversOnlyWhatFallsOutOfTheTail(t *testing.T) {
	gen := &stubGenerator{reply: "they set a squat goal of 100kg"}
	convos, sum, u := summaryFixture(t, gen, 10)
	ctx := context.Background()

	c, err := convos.Start(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	fill(t, convos, c.ID, 30)

	if err = sum.Summarize(ctx, c.ID); err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if len(gen.prompts) != 1 {
		t.Fatalf("model calls = %d, want 1", len(gen.prompts))
	}

	prompt := gen.prompts[0]
	// 30 turns, tail of 10 -> turns 0..19 are folded in, 20..29 are not.
	if !strings.Contains(prompt, "user turn 0") {
		t.Fatal("the oldest turn was not folded in")
	}
	if !strings.Contains(prompt, "turn 19") {
		t.Fatal("the last turn outside the tail was not folded in")
	}
	if strings.Contains(prompt, "turn 20") {
		t.Fatal("a turn still in the tail was summarised as well")
	}

	got, err := convos.Get(ctx, c.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContextSummary != "they set a squat goal of 100kg" {
		t.Fatalf("summary = %q", got.ContextSummary)
	}
	if got.ContextSummaryThrough == nil {
		t.Fatal("watermark was not set")
	}
	// The reflection summary is a different column and must be untouched.
	if got.Summary != "" {
		t.Fatalf("wrote into the reflection summary: %q", got.Summary)
	}
}

// A second pass folds only the new turns in, and is given what it wrote before.
func TestSummarizeIsIncremental(t *testing.T) {
	gen := &stubGenerator{reply: "first pass"}
	convos, sum, u := summaryFixture(t, gen, 10)
	ctx := context.Background()

	c, err := convos.Start(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	fill(t, convos, c.ID, 30)
	if err = sum.Summarize(ctx, c.ID); err != nil {
		t.Fatal(err)
	}

	fill(t, convos, c.ID, 10) // now 40
	gen.reply = "second pass"
	if err = sum.Summarize(ctx, c.ID); err != nil {
		t.Fatal(err)
	}

	if len(gen.prompts) != 2 {
		t.Fatalf("model calls = %d, want 2", len(gen.prompts))
	}
	second := gen.prompts[1]
	if !strings.Contains(second, "first pass") {
		t.Fatal("the second pass was not shown what the first wrote")
	}
	if strings.Contains(second, "user turn 0") {
		t.Fatal("the second pass re-folded turns the first already covered")
	}

	got, err := convos.Get(ctx, c.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContextSummary != "second pass" {
		t.Fatalf("summary = %q", got.ContextSummary)
	}
}

// Running again with no new turns must not call the model.
func TestSummarizeIsIdempotent(t *testing.T) {
	gen := &stubGenerator{reply: "once"}
	convos, sum, u := summaryFixture(t, gen, 10)
	ctx := context.Background()

	c, err := convos.Start(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	fill(t, convos, c.ID, 30)

	for range 3 {
		if err = sum.Summarize(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	if len(gen.prompts) != 1 {
		t.Fatalf("model calls = %d across three passes, want 1", len(gen.prompts))
	}
}

// An empty reply must not advance the watermark: doing so would drop those
// turns from context with nothing describing them.
func TestSummarizeRefusesToBankAnEmptyReply(t *testing.T) {
	gen := &stubGenerator{reply: "   "}
	convos, sum, u := summaryFixture(t, gen, 10)
	ctx := context.Background()

	c, err := convos.Start(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	fill(t, convos, c.ID, 30)

	if err = sum.Summarize(ctx, c.ID); err == nil {
		t.Fatal("an empty summary was accepted")
	}

	got, err := convos.Get(ctx, c.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.HasContextSummary() || got.ContextSummaryThrough != nil {
		t.Fatal("an empty reply moved the watermark")
	}
}

// The whole point of the ticket: the person still sees every message.
func TestHistoryIsUnaffectedBySummarization(t *testing.T) {
	gen := &stubGenerator{reply: "compacted"}
	convos, sum, u := summaryFixture(t, gen, 10)
	ctx := context.Background()

	c, err := convos.Start(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	fill(t, convos, c.ID, 30)

	before, err := convos.History(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = sum.Summarize(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	after, err := convos.History(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(before) != 30 || len(after) != 30 {
		t.Fatalf("history = %d before, %d after; want 30 and 30", len(before), len(after))
	}
	if after[0].Content != "user turn 0" {
		t.Fatalf("the oldest message is no longer first in history: %q", after[0].Content)
	}
}

// The sweep finds long threads and ignores short ones.
func TestAwaitingSummaryFindsOnlyOutgrownThreads(t *testing.T) {
	gen := &stubGenerator{reply: "x"}
	convos, _, u := summaryFixture(t, gen, 10)
	ctx := context.Background()

	short, err := convos.Start(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	fill(t, convos, short.ID, 4)

	long, err := convos.Start(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	fill(t, convos, long.ID, 30)

	pending, err := convos.AwaitingSummary(ctx, 10, 50)
	if err != nil {
		t.Fatal(err)
	}

	var ids []uuid.UUID
	for _, p := range pending {
		ids = append(ids, p.ID)
	}
	if !containsID(ids, long.ID) {
		t.Fatal("the long thread was not offered to the sweep")
	}
	if containsID(ids, short.ID) {
		t.Fatal("a short thread was offered to the sweep")
	}
}

func containsID(ids []uuid.UUID, want uuid.UUID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
