package soreness

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/soreness/sore"
	"github.com/NorthAIProject/north-client/internal/users"
)

type Service struct {
	repo Repository
	now  func() time.Time
}

// Repository is the persistence Service needs. Service names it rather than
// the Postgres type so the database stays a detail of the composition root:
// NewRepository is what the process wires, and a test can hand Service
// anything that keeps soreness entries.
type Repository interface {
	Upsert(ctx context.Context, userID uuid.UUID, day time.Time, region string, severity int, note string) (Entry, error)
	Delete(ctx context.Context, userID uuid.UUID, day time.Time, region string) error
	ListBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Entry, error)
}

func NewService(repo Repository) *Service { return &Service{repo: repo, now: time.Now} }

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

// HistoryMaxDays is the longest stretch History looks back over.
const HistoryMaxDays = 90

// History lists the sore regions of the last days days, today included,
// newest first.
func (s *Service) History(ctx context.Context, user users.User, days int) ([]Entry, error) {
	if days < 1 || days > HistoryMaxDays {
		return nil, apperr.FieldErrors{}.Add("days", "Days is 1 to 90.")
	}
	today := s.today(user)
	return s.repo.ListBetween(ctx, user.ID, today.AddDate(0, 0, 1-days), today.AddDate(0, 0, 1))
}

// Today is the user's current day, the last one History covers.
func (s *Service) Today(user users.User) time.Time { return s.today(user) }

// OnDate lists one day's sore regions.
func (s *Service) OnDate(ctx context.Context, user users.User, date time.Time) ([]Entry, error) {
	d := timerange.StartOfDay(date.In(user.Location()))
	return s.repo.ListBetween(ctx, user.ID, d, d.AddDate(0, 0, 1))
}
