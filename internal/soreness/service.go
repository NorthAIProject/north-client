package soreness

import (
	"context"
	"strings"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/soreness/sore"
	"github.com/NorthAIProject/north-client/internal/users"
)

type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewService(repo *Repository) *Service { return &Service{repo: repo, now: time.Now} }

func (s *Service) today(user users.User) time.Time {
	return timerange.StartOfDay(s.now().In(user.Location()))
}

// Set records or changes today's soreness in one region.
func (s *Service) Set(ctx context.Context, user users.User, region string, severity int, note string) (Entry, error) {
	if !sore.ValidRegion(region) {
		return Entry{}, apperr.FieldErrors{}.Add("region", "Pick a region of the body.")
	}
	if severity < sore.Stiff || severity > sore.Painful {
		return Entry{}, apperr.FieldErrors{}.Add("severity", "Severity is 1 (stiff) to 3 (painful).")
	}
	return s.repo.Upsert(ctx, user.ID, s.today(user), region, severity, strings.TrimSpace(note))
}

// Clear removes today's soreness in one region — it stopped hurting.
func (s *Service) Clear(ctx context.Context, user users.User, region string) error {
	if !sore.ValidRegion(region) {
		return apperr.FieldErrors{}.Add("region", "Pick a region of the body.")
	}
	return s.repo.Delete(ctx, user.ID, s.today(user), region)
}

// OnDate lists one day's sore regions.
func (s *Service) OnDate(ctx context.Context, user users.User, date time.Time) ([]Entry, error) {
	d := timerange.StartOfDay(date.In(user.Location()))
	return s.repo.ListBetween(ctx, user.ID, d, d.AddDate(0, 0, 1))
}
