package memories

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/memories/extract"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
)

// maxOfferedFacts bounds how many believed facts an extraction is shown.
//
// Every one of them is prompt tokens on a job that runs for each conversation
// that goes quiet, so this is a real cost rather than a free improvement. Thirty
// facts at up to 240 characters is a few thousand characters — enough that a
// contradiction is usually visible, small enough that extraction does not become
// the most expensive call in the product.
//
// Ordered by category then recency by the query, so when the cap does bite it
// drops the oldest fact in the longest-tailed category rather than a random one.
const maxOfferedFacts = 30

// Extractor runs structured extraction against a transcript.
//
// believed is what the store already holds, numbered so a returned fact can say
// which of them it replaces. Passed in rather than looked up here so the
// extractor stays a pure function of its inputs and can be tested without a
// database.
//
// Production uses the AI registry; tests inject a stub.
type Extractor interface {
	Extract(ctx context.Context, transcript string, believed []CurrentFact) ([]extract.Candidate, error)
}

// AIExtractor calls the provider chain with the memory extraction schema.
//
// Memory is the product, and an extraction that is skipped is a thing the coach
// will not know months from now — so this walks the chain rather than accepting
// whatever the default provider says today.
type AIExtractor struct {
	Runner *ai.Runner
	Model  string
}

func (e *AIExtractor) Extract(ctx context.Context, transcript string, believed []CurrentFact) ([]extract.Candidate, error) {
	if e == nil || e.Runner == nil {
		return nil, apperr.New("memory extraction is not configured")
	}

	prompt, err := prompts.Render(prompts.MemoryExtraction, map[string]string{
		"Transcript": transcript,
		"Believed":   formatBelieved(believed),
	})
	if err != nil {
		return nil, err
	}

	temp := float32(0.1)

	var resp *ai.Response
	_, err = e.Runner.Run(ctx, ai.RunOptions{}, func(client ai.Client) error {
		// Resolved per provider: an empty model means "whatever this client is
		// configured with", and the client that answers may not be the one the
		// chain started on.
		model := e.Model
		if model == "" {
			model = client.Name()
		}

		r, genErr := client.Generate(ctx, ai.Request{
			Model:          model,
			Messages:       []ai.Message{ai.UserText(prompt)},
			ResponseSchema: extract.Schema(),
			Temperature:    &temp,
		})
		resp = r
		return genErr
	})
	if err != nil {
		return nil, apperr.Wrap(err, "extract memories")
	}

	var result extract.Result
	if err := json.Unmarshal([]byte(resp.Text), &result); err != nil {
		return nil, apperr.Wrap(err, "decode memory extraction")
	}
	return extract.Sanitise(result, len(believed)), nil
}

// formatBelieved numbers the facts the store already holds, so a returned fact
// can point at one of them.
//
// The numbers are 1-based because 0 is the schema's "replaces nothing", and a
// list starting at 0 would make the two meanings collide on the most likely
// value a model emits when it has no opinion.
func formatBelieved(believed []CurrentFact) string {
	if len(believed) == 0 {
		return "(nothing recorded yet)"
	}

	var b strings.Builder
	for i, f := range believed {
		pinned := ""
		if f.Pinned {
			pinned = ", pinned"
		}
		fmt.Fprintf(&b, "%d. [%s%s] %s\n", i+1, f.Category, pinned, f.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ExtractionService is the worker-side facade: conversations + memories + AI.
type ExtractionService struct {
	Memories      *Service
	Conversations *conversations.Service
	Extractor     Extractor
	Log           *slog.Logger
}

// HandleExtractJob is registered on the worker.
func (s *ExtractionService) HandleExtractJob(ctx context.Context, payload json.RawMessage) error {
	var p jobs.ExtractMemoriesPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return apperr.Wrap(err, "decode extract payload")
	}
	if p.UserID == uuid.Nil || p.ConversationID == uuid.Nil {
		return apperr.New("extract payload missing ids")
	}

	// Ownership: conversation must belong to the user.
	if _, err := s.Conversations.Get(ctx, p.ConversationID, p.UserID); err != nil {
		return err
	}

	msgs, err := s.Conversations.Recent(ctx, p.ConversationID, 20)
	if err != nil {
		return err
	}
	if len(msgs) < 2 {
		// Still counts as read. A one-line thread will never be worth
		// extracting, and leaving it unmarked would put it back in front of
		// the sweep on every pass.
		return s.markRead(ctx, p.ConversationID)
	}

	// What the store already believes, so the model can say which of it is now
	// out of date instead of filing a contradicting duplicate.
	believed, err := s.Memories.CurrentForSupersession(ctx, p.UserID, maxOfferedFacts)
	if err != nil {
		return err
	}

	transcript := formatTranscript(msgs)
	// Attribution goes on here rather than inside Extract: the job payload is
	// where the account is known, and the extractor deliberately takes only a
	// transcript and the believed facts so it can be tested without one.
	candidates, err := s.Extractor.Extract(aiattr.WithUser(ctx, p.UserID, spend.SurfaceMemory), transcript, believed)
	if err != nil {
		// Deliberately not marked: the thread was not read, the provider
		// failed. Marking here would lose it silently.
		return err
	}

	n, err := s.Memories.InsertExtractions(ctx, p.UserID, p.ConversationID, resolveSupersessions(candidates, believed))
	if err != nil {
		return err
	}

	// Marked whether or not anything was found. Recording "nothing here" is the
	// whole reason an uneventful conversation is not re-read, and re-run, for
	// as long as it exists.
	if err := s.markRead(ctx, p.ConversationID); err != nil {
		return err
	}
	if s.Log != nil && n > 0 {
		s.Log.Info("memory extraction stored candidates",
			slog.Int("count", n),
			slog.String("conversation_id", p.ConversationID.String()),
			slog.String("user_id", p.UserID.String()),
		)
	}
	return nil
}

// resolveSupersessions turns each candidate's 1-based index back into the id of
// the fact it replaces.
//
// Sanitise has already dropped out-of-range indexes to zero, so anything still
// set here points at a fact that was really offered. The bounds check stays
// anyway: this function is one line away from being called with a different
// list than the one the model saw, and the failure mode of getting that wrong
// is retiring the wrong fact.
func resolveSupersessions(candidates []extract.Candidate, believed []CurrentFact) []Proposal {
	out := make([]Proposal, 0, len(candidates))
	for _, c := range candidates {
		pr := Proposal{Candidate: c}
		if c.Supersedes >= 1 && c.Supersedes <= len(believed) {
			id := believed[c.Supersedes-1].ID
			pr.SupersedesID = &id
		}
		out = append(out, pr)
	}
	return out
}

func (s *ExtractionService) markRead(ctx context.Context, conversationID uuid.UUID) error {
	return s.Conversations.MarkExtracted(ctx, conversationID)
}

func formatTranscript(msgs []conversations.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		role := "User"
		if m.Role == ai.RoleModel {
			role = "Coach"
		}
		fmt.Fprintf(&b, "%s: %s\n\n", role, strings.TrimSpace(m.Content))
	}
	return strings.TrimSpace(b.String())
}
