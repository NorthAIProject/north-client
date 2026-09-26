package milestones

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewService(repo *Repository) *Service { return &Service{repo: repo, now: time.Now} }

// Create starts tracking something. LastDone nil means today.
func (s *Service) Create(ctx context.Context, user users.User, name string, lastDone *time.Time, intervalMonths *int) (Tracker, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 40 {
		return Tracker{}, apperr.FieldErrors{}.Add("name", "Name it in up to 40 characters.")
	}
	if intervalMonths != nil && (*intervalMonths < 1 || *intervalMonths > 60) {
		return Tracker{}, apperr.FieldErrors{}.Add("intervalMonths", "Choose an interval between 1 and 60 months.")
	}
	today := timerange.StartOfDay(s.now().In(user.Location()))
	last := today
	if lastDone != nil {
		last = timerange.StartOfDay(lastDone.In(user.Location()))
		if last.After(today) {
			return Tracker{}, apperr.FieldErrors{}.Add("lastDoneOn", "That date has not happened yet.")
		}
	}
	return s.repo.Create(ctx, user.ID, name, last, intervalMonths)
}

// Done resets a tracker to today.
func (s *Service) Done(ctx context.Context, user users.User, id uuid.UUID) (Tracker, error) {
	return s.repo.MarkDone(ctx, id, user.ID, timerange.StartOfDay(s.now().In(user.Location())))
}

func (s *Service) Delete(ctx context.Context, user users.User, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, user.ID)
}

func (s *Service) List(ctx context.Context, user users.User) ([]Tracker, error) {
	return s.repo.List(ctx, user.ID)
}
