package caffeine

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/caffeine/caffeine"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// maxEntryMG mirrors the column's CHECK, so a typo is a field error.
const maxEntryMG = 1000

type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewService(repo *Repository) *Service { return &Service{repo: repo, now: time.Now} }

// LogInput is one drink. A Preset fills MG and Label when they are empty; At
// defaults to now, and may be set for a cup remembered after the fact.
type LogInput struct {
	Preset string
	MG     int
	Label  string
	At     *time.Time
}

// Log records a drink against the local day it was drunk on.
func (s *Service) Log(ctx context.Context, user users.User, in LogInput) (Entry, error) {
	if in.Preset != "" {
		mg, ok := caffeine.PresetMG(in.Preset)
		if !ok {
			return Entry{}, apperr.FieldErrors{}.Add("preset", "Pick one of the listed drinks.")
		}
		if in.MG == 0 {
			in.MG = mg
		}
		if in.Label == "" {
			in.Label = in.Preset
		}
	}
	if in.MG <= 0 || in.MG > maxEntryMG {
		return Entry{}, apperr.FieldErrors{}.Add("mg", "Enter an amount between 1 and 1000 mg.")
	}
	at := s.now()
	if in.At != nil {
		at = *in.At
	}
	date := timerange.StartOfDay(at.In(user.Location()))
	return s.repo.Create(ctx, user.ID, date, in.MG, strings.TrimSpace(in.Label), at)
}

// Undo removes a drink logged by mistake.
func (s *Service) Undo(ctx context.Context, user users.User, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, user.ID)
}

// Between lists the drinks inside a window, newest first.
func (s *Service) Between(ctx context.Context, user users.User, rg timerange.Range) ([]Entry, error) {
	return s.repo.ListBetween(ctx, user.ID, rg.Since, rg.Until)
}

// Today lists today's drinks in the person's zone.
func (s *Service) Today(ctx context.Context, user users.User) ([]Entry, error) {
	return s.Between(ctx, user, timerange.Parse(timerange.KeyToday, user.Location()))
}
