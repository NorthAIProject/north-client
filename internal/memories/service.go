package memories

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/memories/extract"
	"github.com/NorthAIProject/north-client/internal/search"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

const (
	listDefault   = 100
	contextLimit  = 20
	maxContentLen = 240
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// Input is a memory as submitted by the user.
type Input struct {
	Category string
	Content  string
}

// Validate checks a manual memory before storage.
func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	in.Category = strings.TrimSpace(in.Category)
	if in.Category == "" {
		in.Category = CategoryGeneral
	} else if !slices.Contains(Categories, in.Category) {
		errs = errs.Add("category", "Choose one of the listed categories.")
	}

	in.Content = strings.TrimSpace(in.Content)
	switch {
	case in.Content == "":
		errs = errs.Add("content", "Write the fact you want North to remember.")
	case len(in.Content) < 8:
		errs = errs.Add("content", "Make it specific — a few more words help.")
	case len(in.Content) > maxContentLen:
		errs = errs.Add("content", "Keep this under 240 characters.")
	}

	return in, errs.OrNil()
}

// Create stores a user-authored fact as approved.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (Memory, error) {
	clean, err := Validate(in)
	if err != nil {
		return Memory{}, err
	}
	return s.repo.Create(ctx, userID, NewMemory{
		Category: clean.Category,
		Content:  clean.Content,
		Status:   StatusApproved,
		Source:   SourceUser,
	})
}

func (s *Service) Get(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
	return s.repo.Get(ctx, id, userID)
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, limit int) ([]Memory, error) {
	if limit <= 0 || limit > 200 {
		limit = listDefault
	}
	return s.repo.List(ctx, userID, limit)
}

func (s *Service) ListPending(ctx context.Context, userID uuid.UUID) ([]Memory, error) {
	return s.repo.ListByStatus(ctx, userID, StatusPending, listDefault)
}

func (s *Service) ListApproved(ctx context.Context, userID uuid.UUID) ([]Memory, error) {
	return s.repo.ListByStatus(ctx, userID, StatusApproved, listDefault)
}

func (s *Service) Update(ctx context.Context, id, userID uuid.UUID, in Input) (Memory, error) {
	clean, err := Validate(in)
	if err != nil {
		return Memory{}, err
	}
	return s.repo.Update(ctx, id, userID, clean.Category, clean.Content)
}

func (s *Service) Approve(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
	return s.repo.SetStatus(ctx, id, userID, StatusApproved)
}

func (s *Service) Reject(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
	return s.repo.SetStatus(ctx, id, userID, StatusRejected)
}

func (s *Service) SetPinned(ctx context.Context, id, userID uuid.UUID, pinned bool) (Memory, error) {
	return s.repo.SetPinned(ctx, id, userID, pinned)
}

func (s *Service) SetExcluded(ctx context.Context, id, userID uuid.UUID, excluded bool) (Memory, error) {
	return s.repo.SetExcluded(ctx, id, userID, excluded)
}

func (s *Service) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.SoftDelete(ctx, id, userID)
}

func (s *Service) CountPending(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.repo.CountPending(ctx, userID)
}

// ForContext returns the approved memories the coach should see this turn.
//
// With a query, facts are ranked against it and pinned facts are kept
// regardless. Without one — a background job, or a turn with no user text —
// there is nothing to rank against, so it falls back to the newest facts. That
// fallback is the old behaviour, and it is the correct answer to "show me
// something" rather than a degraded answer to "show me what matters".
func (s *Service) ForContext(ctx context.Context, userID uuid.UUID, query string) ([]Retrieved, error) {
	normalised, err := search.Normalise(query)
	if err != nil {
		if !errors.Is(err, search.ErrEmptyTerm) {
			return nil, err
		}
		return s.recentForContext(ctx, userID)
	}
	return s.repo.SearchForContext(ctx, userID, normalised, contextLimit)
}

func (s *Service) recentForContext(ctx context.Context, userID uuid.UUID) ([]Retrieved, error) {
	list, err := s.repo.ForContext(ctx, userID, contextLimit)
	if err != nil {
		return nil, err
	}
	out := make([]Retrieved, 0, len(list))
	for _, m := range list {
		out = append(out, Retrieved{Memory: m})
	}
	return out, nil
}

// InsertExtractions stores sanitised candidates as pending, skipping duplicates.
func (s *Service) InsertExtractions(ctx context.Context, userID, conversationID uuid.UUID, candidates []extract.Candidate) (int, error) {
	if len(candidates) == 0 {
		return 0, nil
	}

	existing, err := s.repo.ExistingContents(ctx, userID)
	if err != nil {
		return 0, err
	}

	var convPtr *uuid.UUID
	if conversationID != uuid.Nil {
		convPtr = &conversationID
	}

	inserted := 0
	for _, c := range candidates {
		key := strings.ToLower(strings.TrimSpace(c.Content))
		if key == "" || existing[key] {
			continue
		}
		conf := c.Confidence
		if _, err := s.repo.Create(ctx, userID, NewMemory{
			Category:             c.Category,
			Content:              strings.TrimSpace(c.Content),
			Status:               StatusPending,
			Source:               SourceExtraction,
			SourceConversationID: convPtr,
			Confidence:           &conf,
		}); err != nil {
			return inserted, err
		}
		existing[key] = true
		inserted++
	}
	return inserted, nil
}
