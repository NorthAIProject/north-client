package goals

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// contextGoals bounds how many active goals reach the coach. Someone with
// thirty active goals has a prioritisation problem the coach should notice
// rather than a context that should grow to fit.
const contextGoals = 8

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// Input is a goal as submitted.
type Input struct {
	Title      string
	Motivation string
	Success    string
	Category   string
	TargetDate time.Time
}

// Validate checks a goal before it is stored.
func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	in.Title = strings.TrimSpace(in.Title)
	switch {
	case in.Title == "":
		errs = errs.Add("title", "Give the goal a name.")
	case len(in.Title) > 200:
		errs = errs.Add("title", "Keep the name under 200 characters.")
	}

	in.Motivation = strings.TrimSpace(in.Motivation)
	if len(in.Motivation) > 1000 {
		errs = errs.Add("motivation", "Keep this under 1000 characters.")
	}

	in.Success = strings.TrimSpace(in.Success)
	if len(in.Success) > 1000 {
		errs = errs.Add("success", "Keep this under 1000 characters.")
	}

	in.Category = strings.TrimSpace(in.Category)
	if in.Category == "" {
		in.Category = CategoryOther
	} else if !slices.Contains(Categories, in.Category) {
		errs = errs.Add("category", "Choose one of the listed categories.")
	}

	// A deadline in the past is almost always a typo in the year, and storing
	// it would make the goal permanently overdue the moment it is created.
	if !in.TargetDate.IsZero() && in.TargetDate.Before(time.Now().AddDate(0, 0, -1)) {
		errs = errs.Add("target_date", "That date has already passed.")
	}

	return in, errs.OrNil()
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (Goal, error) {
	clean, err := Validate(in)
	if err != nil {
		return Goal{}, err
	}

	return s.repo.Create(ctx, userID, NewGoal(clean))
}

func (s *Service) Update(ctx context.Context, id, userID uuid.UUID, in Input) (Goal, error) {
	clean, err := Validate(in)
	if err != nil {
		return Goal{}, err
	}

	return s.repo.Update(ctx, id, userID, NewGoal(clean))
}

func (s *Service) Get(ctx context.Context, id, userID uuid.UUID) (Goal, error) {
	return s.repo.Get(ctx, id, userID)
}

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Goal, error) {
	return s.repo.List(ctx, userID, 100)
}

func (s *Service) ListActive(ctx context.Context, userID uuid.UUID) ([]Goal, error) {
	return s.repo.ListActive(ctx, userID, contextGoals)
}

// SetStatus moves a goal between active, achieved, paused, and abandoned.
func (s *Service) SetStatus(ctx context.Context, id, userID uuid.UUID, status string) (Goal, error) {
	if !slices.Contains(Statuses, status) {
		return Goal{}, apperr.Wrap(apperr.ErrValidation, "unknown goal status %q", status)
	}
	return s.repo.SetStatus(ctx, id, userID, status)
}

func (s *Service) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.Delete(ctx, id, userID)
}

// AddUpdate records a progress note.
func (s *Service) AddUpdate(ctx context.Context, goalID, userID uuid.UUID, note string, progress *int) (Update, error) {
	note = strings.TrimSpace(note)

	var errs apperr.FieldErrors
	switch {
	case note == "":
		errs = errs.Add("note", "Write something about how it is going.")
	case len(note) > 2000:
		errs = errs.Add("note", "Keep this under 2000 characters.")
	}
	if progress != nil && (*progress < 0 || *progress > 100) {
		errs = errs.Add("progress", "Progress must be between 0 and 100.")
	}
	if err := errs.OrNil(); err != nil {
		return Update{}, err
	}

	// Ownership is checked before writing, because goal_updates has its own
	// user_id and would otherwise accept a note against someone else's goal.
	if _, err := s.repo.Get(ctx, goalID, userID); err != nil {
		return Update{}, err
	}

	return s.repo.AddUpdate(ctx, goalID, userID, note, progress)
}

func (s *Service) Updates(ctx context.Context, goalID uuid.UUID, limit int) ([]Update, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.Updates(ctx, goalID, limit)
}
