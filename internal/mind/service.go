package mind

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/checkins"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// contextEntries bounds how many journal entries reach the coach, same
// reasoning as goals' contextGoals: a handful of recent reflections, not a
// growing archive.
const contextEntries = 5

// MoodLookup is the mind package's view of check-ins: just enough to read
// recent mood/energy history, so this package need not depend on
// checkins.Service's concrete type. Satisfied directly by *checkins.Service.
type MoodLookup interface {
	List(ctx context.Context, userID uuid.UUID, limit int) ([]checkins.CheckIn, error)
}

type Service struct {
	repo  *Repository
	moods MoodLookup
}

func NewService(repo *Repository, moods MoodLookup) *Service {
	return &Service{repo: repo, moods: moods}
}

// Input is a journal entry as submitted.
type Input struct {
	Content string
	Mood    *int
}

func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	in.Content = strings.TrimSpace(in.Content)
	switch {
	case in.Content == "":
		errs = errs.Add("content", "Write something.")
	case len(in.Content) > 4000:
		errs = errs.Add("content", "Keep this under 4000 characters.")
	}

	if in.Mood != nil && (*in.Mood < 1 || *in.Mood > 5) {
		errs = errs.Add("mood", "Mood must be between 1 and 5.")
	}

	return in, errs.OrNil()
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (JournalEntry, error) {
	clean, err := Validate(in)
	if err != nil {
		return JournalEntry{}, err
	}
	return s.repo.Create(ctx, userID, clean.Content, clean.Mood)
}

// ListBetween returns the entries written inside a window, newest first.
func (s *Service) ListBetween(ctx context.Context, userID uuid.UUID, rg timerange.Range) ([]JournalEntry, error) {
	return s.repo.ListBetween(ctx, userID, rg.Since, rg.Until)
}

func (s *Service) Recent(ctx context.Context, userID uuid.UUID, limit int) ([]JournalEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.Recent(ctx, userID, limit)
}

// MoodTrend summarizes recent check-in mood/energy — a view over data
// check-ins already collect, not a new data model.
type MoodTrend struct {
	AverageMood   float64
	AverageEnergy float64
	Count         int
}

func (t MoodTrend) Summary() string {
	return fmt.Sprintf("Average mood %.1f/5, average energy %.1f/5 over the last %d check-ins", t.AverageMood, t.AverageEnergy, t.Count)
}

// RecentMoodTrend averages the trailing `limit` check-ins' mood and energy.
func (s *Service) RecentMoodTrend(ctx context.Context, userID uuid.UUID, limit int) (MoodTrend, error) {
	if limit <= 0 || limit > 100 {
		limit = 14
	}

	list, err := s.moods.List(ctx, userID, limit)
	if err != nil {
		return MoodTrend{}, err
	}
	if len(list) == 0 {
		return MoodTrend{}, nil
	}

	var mood, energy int
	for _, c := range list {
		mood += c.Mood
		energy += c.Energy
	}

	return MoodTrend{
		AverageMood:   float64(mood) / float64(len(list)),
		AverageEnergy: float64(energy) / float64(len(list)),
		Count:         len(list),
	}, nil
}
