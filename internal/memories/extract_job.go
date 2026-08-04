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
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Extractor runs structured extraction against a transcript.
//
// Production uses the AI registry; tests inject a stub.
type Extractor interface {
	Extract(ctx context.Context, transcript string) ([]extract.Candidate, error)
}

// AIExtractor calls a registered AI client with the memory extraction schema.
type AIExtractor struct {
	Registry *ai.Registry
	Model    string
}

func (e *AIExtractor) Extract(ctx context.Context, transcript string) ([]extract.Candidate, error) {
	if e == nil || e.Registry == nil {
		return nil, apperr.New("memory extraction is not configured")
	}
	client, err := e.Registry.Default()
	if err != nil {
		return nil, err
	}

	prompt, err := prompts.Render(prompts.MemoryExtraction, map[string]string{
		"Transcript": transcript,
	})
	if err != nil {
		return nil, err
	}

	model := e.Model
	if model == "" {
		model = client.Name()
	}

	temp := float32(0.1)
	resp, err := client.Generate(ctx, ai.Request{
		Model:          model,
		Messages:       []ai.Message{ai.UserText(prompt)},
		ResponseSchema: extract.Schema(),
		Temperature:    &temp,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "extract memories")
	}

	var result extract.Result
	if err := json.Unmarshal([]byte(resp.Text), &result); err != nil {
		return nil, apperr.Wrap(err, "decode memory extraction")
	}
	return extract.Sanitise(result), nil
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
		return nil
	}

	transcript := formatTranscript(msgs)
	candidates, err := s.Extractor.Extract(ctx, transcript)
	if err != nil {
		return err
	}

	n, err := s.Memories.InsertExtractions(ctx, p.UserID, p.ConversationID, candidates)
	if err != nil {
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
