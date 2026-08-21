package conversations

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/jobs"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// DefaultKeepRecent is how many turns the coach sends verbatim. Everything
// older is represented by the rolling summary instead.
//
// Kept here rather than in coach because both the summariser and the context
// builder must agree on it: summarising a different number of turns than the
// tail carries would either lose history or pay for it twice.
const DefaultKeepRecent = 20

// summaryMaxTokens caps the compaction. The prompt asks for under 300 words;
// this is the backstop. Deliberately far smaller than the history it replaces —
// a summary that grew as fast as the thread would defeat the point.
const summaryMaxTokens = 700

// summaryBatch bounds how many turns one pass folds in, so a thread left
// unsummarised for a very long time is compacted over several passes rather
// than in one enormous prompt.
const summaryBatch = 200

// Generator is the non-streaming AI call a compaction needs.
//
// The same shape reports uses, and for the same reason: this runs in the
// worker, where there is no per-request provider chain to inherit.
type Generator interface {
	Generate(ctx context.Context, req ai.Request) (*ai.Response, error)
}

// Summarizer compacts the older turns of long threads.
//
// Its own type rather than a method on Service because it needs a model and
// Service deliberately does not: everything else in this package stores and
// retrieves, and keeping the AI call at arm's length is what lets the storage
// be tested without one.
type Summarizer struct {
	convos     *Service
	client     Generator
	model      string
	keepRecent int
	log        *slog.Logger
}

type SummarizerOptions struct {
	Conversations *Service
	Client        Generator

	// Model is the cheap one. Compaction is working memory, not a product.
	Model string

	// KeepRecent must match the context builder's tail. Zero uses the default.
	KeepRecent int

	Log *slog.Logger
}

func NewSummarizer(opts SummarizerOptions) *Summarizer {
	s := &Summarizer{
		convos:     opts.Conversations,
		client:     opts.Client,
		model:      opts.Model,
		keepRecent: opts.KeepRecent,
		log:        opts.Log,
	}
	if s.keepRecent <= 0 {
		s.keepRecent = DefaultKeepRecent
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	return s
}

// HandleSummarizeJob is registered on the worker.
func (s *Summarizer) HandleSummarizeJob(ctx context.Context, payload json.RawMessage) error {
	var p jobs.SummarizeConversationPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return apperr.Wrap(err, "decode summarize payload")
	}
	if p.ConversationID == uuid.Nil {
		return apperr.New("summarize payload missing conversation id")
	}
	return s.Summarize(ctx, p.ConversationID)
}

// Summarize folds a thread's older turns into its rolling summary.
//
// "Older" means everything except the tail the context builder still sends
// verbatim: summarising a turn the model can already read costs twice for the
// same information.
func (s *Summarizer) Summarize(ctx context.Context, conversationID uuid.UUID) error {
	if s.convos == nil || s.client == nil {
		return nil
	}

	owner, err := s.convos.Owner(ctx, conversationID)
	if err != nil {
		return err
	}
	convo, err := s.convos.Get(ctx, conversationID, owner)
	if err != nil {
		return err
	}

	// The watermark: the newest turn that is NOT in the tail. Everything at or
	// before it is what the model is about to stop being able to see.
	tail, err := s.convos.Recent(ctx, conversationID, s.keepRecent)
	if err != nil {
		return err
	}
	if len(tail) < s.keepRecent {
		return nil // still short enough that nothing has fallen out
	}
	through := tail[0].CreatedAt.Add(-time.Nanosecond)

	var after time.Time
	if convo.ContextSummaryThrough != nil {
		after = *convo.ContextSummaryThrough
		if !through.After(after) {
			return nil // already covers everything outside the tail
		}
	}

	pending, err := s.convos.ToSummarize(ctx, conversationID, after, through, summaryBatch)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	prompt, err := prompts.Render(prompts.ConversationSummary, map[string]any{
		"Existing":   convo.ContextSummary,
		"Transcript": Transcript(pending),
	})
	if err != nil {
		return apperr.Wrap(err, "build summary prompt")
	}

	resp, err := s.client.Generate(ctx, ai.Request{
		Model:     s.model,
		Messages:  []ai.Message{ai.UserText(prompt)},
		MaxTokens: summaryMaxTokens,
	})
	if err != nil {
		return apperr.Wrap(err, "generate conversation summary")
	}

	// The watermark only advances together with text that describes those
	// turns; SetContextSummary refuses an empty one. An empty reply therefore
	// leaves the turns pending for the next pass rather than writing them off.
	return s.convos.SetContextSummary(ctx, conversationID, resp.Text, pending[len(pending)-1].CreatedAt)
}

// Transcript renders turns for the summarising prompt.
func Transcript(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		speaker := "Coach"
		if m.Role == ai.RoleUser {
			speaker = "Them"
		}
		fmt.Fprintf(&b, "%s: %s\n\n", speaker, strings.TrimSpace(m.Content))
	}
	return strings.TrimSpace(b.String())
}

type chainGenerator struct {
	runner *ai.Runner
}

// ClientFromChain walks the provider chain until one of them answers.
//
// Compaction runs in the worker, where there is no request and so no
// bring-your-own provider to inherit — hence the empty tier, which selects the
// default chain. Same trade-off reports makes.
//
// It walks the chain rather than taking the registry default because a
// compaction that gives up when the head provider is rate limited leaves the
// conversation ungrown, and the job is retried against the same busy provider.
func ClientFromChain(runner *ai.Runner) Generator {
	return chainGenerator{runner: runner}
}

func (g chainGenerator) Generate(ctx context.Context, req ai.Request) (*ai.Response, error) {
	var resp *ai.Response

	_, err := g.runner.Run(ctx, ai.RunOptions{}, func(c ai.Client) error {
		r, err := c.Generate(ctx, req)
		resp = r
		return err
	})
	if err != nil {
		return nil, err
	}

	return resp, nil
}
