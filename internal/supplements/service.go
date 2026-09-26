package supplements

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/supplements/supplement"
	"github.com/NorthAIProject/north-client/internal/users"
)

type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewService(repo *Repository) *Service { return &Service{repo: repo, now: time.Now} }

// LogInput is one supplement taken. A Preset fills the name and nutrients;
// otherwise Name is required and Nutrients are whatever the person says.
type LogInput struct {
	Preset    string
	Name      string
	Count     int
	Nutrients []string
}

func (s *Service) Log(ctx context.Context, user users.User, in LogInput) (Entry, error) {
	if in.Preset != "" {
		p, ok := supplement.PresetFor(in.Preset)
		if !ok {
			return Entry{}, apperr.FieldErrors{}.Add("preset", "Pick one of the listed supplements.")
		}
		if in.Name == "" {
			in.Name = p.Name
		}
		if in.Nutrients == nil {
			in.Nutrients = p.Nutrients
		}
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		return Entry{}, apperr.FieldErrors{}.Add("name", "Name the supplement.")
	}
	if in.Count == 0 {
		in.Count = 1
	}
	if in.Count < 1 || in.Count > 20 {
		return Entry{}, apperr.FieldErrors{}.Add("count", "Enter how many, from 1 to 20.")
	}
	for _, n := range in.Nutrients {
		if !supplement.ValidNutrient(n) {
			return Entry{}, apperr.FieldErrors{}.Add("nutrients", "Unknown nutrient "+n+".")
		}
	}
	now := s.now()
	return s.repo.Create(ctx, user.ID, timerange.StartOfDay(now.In(user.Location())), Entry{
		Name: in.Name, Count: in.Count, Nutrients: in.Nutrients, LoggedAt: now,
	})
}

func (s *Service) Undo(ctx context.Context, user users.User, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, user.ID)
}

func (s *Service) Between(ctx context.Context, user users.User, rg timerange.Range) ([]Entry, error) {
	return s.repo.ListBetween(ctx, user.ID, rg.Since, rg.Until)
}

func (s *Service) Today(ctx context.Context, user users.User) ([]Entry, error) {
	return s.Between(ctx, user, timerange.Parse(timerange.KeyToday, user.Location()))
}
