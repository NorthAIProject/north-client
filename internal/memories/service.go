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
		errs = errs.Add("content", "Write the fact you want Khepri to remember.")
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

// Approve believes a pending fact, and retires the one it replaces.
//
// The order matters: the new fact is approved first, so a failure to retire the
// old one leaves both current rather than neither. Two facts the coach can see
// is a duplicate somebody notices; zero is a fact silently lost.
//
// A refused retirement is not an error. The target may have been deleted,
// already retired by an earlier approval, or pinned — and pinned is refused
// deliberately, because a fact the person said always matters is not something a
// model's suggestion gets to overrule. In every one of those cases the new fact
// still stands and the human still sees both.
func (s *Service) Approve(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
	approved, err := s.repo.SetStatus(ctx, id, userID, StatusApproved)
	if err != nil {
		return Memory{}, err
	}

	if approved.SupersedesID != nil {
		if _, _, err := s.repo.Supersede(ctx, *approved.SupersedesID, userID); err != nil {
			return approved, err
		}
	}
	return approved, nil
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

func (s *Service) ListPendingForConversation(ctx context.Context, userID, conversationID uuid.UUID) ([]Memory, error) {
	return s.repo.ListPendingForConversation(ctx, userID, conversationID)
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
// Proposal is a sanitised extraction candidate with its supersession target
// resolved from an index to an id.
//
// The two halves are kept apart on purpose. Candidate is what a model returned
// and is validated as untrusted input; SupersedesID is a real row this code
// resolved. Merging them into one struct would put a model-supplied integer and
// a database identity in the same field set and invite one to be mistaken for
// the other.
type Proposal struct {
	extract.Candidate

	// SupersedesID is the fact approving this one would retire, or nil.
	SupersedesID *uuid.UUID
}

// CurrentForSupersession returns the believed facts an extraction may propose
// replacing.
func (s *Service) CurrentForSupersession(ctx context.Context, userID uuid.UUID, limit int) ([]CurrentFact, error) {
	return s.repo.ListCurrentForSupersession(ctx, userID, limit)
}

// InsertExtractions files proposed facts as pending.
//
// A proposed supersession is recorded on the new row and not acted on: retiring
// the old fact happens when a human approves the new one. Doing it here would
// mean a rejected extraction had already deleted something true, and the whole
// reason extraction lands in a review queue is that it is not trusted yet.
func (s *Service) InsertExtractions(ctx context.Context, userID, conversationID uuid.UUID, proposals []Proposal) (int, error) {
	if len(proposals) == 0 {
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
	for _, pr := range proposals {
		key := strings.ToLower(strings.TrimSpace(pr.Content))
		if key == "" || existing[key] {
			continue
		}
		conf := pr.Confidence
		if _, err := s.repo.Create(ctx, userID, NewMemory{
			Category:             pr.Category,
			Content:              strings.TrimSpace(pr.Content),
			Status:               StatusPending,
			Source:               SourceExtraction,
			SourceConversationID: convPtr,
			Confidence:           &conf,
			SupersedesID:         pr.SupersedesID,
		}); err != nil {
			return inserted, err
		}
		existing[key] = true
		inserted++
	}
	return inserted, nil
}
