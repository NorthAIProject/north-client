package screentime

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/screentime/screen"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewService(repo *Repository) *Service { return &Service{repo: repo, now: time.Now} }

// Set records a day's total. Date nil is today; a Shortcut run after midnight
// can name yesterday.
func (s *Service) Set(ctx context.Context, user users.User, date *time.Time, minutes int, source string) (Day, error) {
	if minutes < 0 || minutes > 1440 {
		return Day{}, apperr.FieldErrors{}.Add("minutes", "Enter between 0 and 1440 minutes.")
	}
	if source == "" {
		source = "manual"
	}
	if !screen.ValidSource(source) {
		return Day{}, apperr.FieldErrors{}.Add("source", "Source must be manual or shortcut.")
	}
	d := timerange.StartOfDay(s.now().In(user.Location()))
	if date != nil {
		d = timerange.StartOfDay(date.In(user.Location()))
	}
	return s.repo.Upsert(ctx, user.ID, d, minutes, source)
}

// ForDate is a day's total, if one was recorded.
func (s *Service) ForDate(ctx context.Context, user users.User, date time.Time) (Day, bool, error) {
	return s.repo.ForDate(ctx, user.ID, timerange.StartOfDay(date.In(user.Location())))
}
