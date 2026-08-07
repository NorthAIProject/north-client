package hydration

import (
	"context"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// maxEntryML mirrors the column's CHECK constraint. Duplicated on purpose:
// the database guarantees it, and this turns a constraint violation into a
// field error a person can act on.
const maxEntryML = 5000

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// LocalDate is the calendar day for this user at the given instant.
func LocalDate(user users.User, at time.Time) time.Time {
	loc := user.Location()
	t := at.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// Log records a drink against the user's current local day.
func (s *Service) Log(ctx context.Context, user users.User, amountML int) (Entry, error) {
	if amountML <= 0 {
		return Entry{}, apperr.FieldErrors{}.Add("amount_ml", "Enter how much you drank.").OrNil()
	}
	if amountML > maxEntryML {
		return Entry{}, apperr.FieldErrors{}.Add("amount_ml", "That looks like more than one drink — log it in smaller amounts.").OrNil()
	}

	return s.repo.Create(ctx, user.ID, LocalDate(user, time.Now()), amountML)
}

// Undo removes an entry. Logging a drink is one tap, so mis-tapping it is
// one tap too, and there is no version of this worth confirming.
func (s *Service) Undo(ctx context.Context, user users.User, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, user.ID)
}

// Today is the user's intake so far today.
func (s *Service) Today(ctx context.Context, user users.User) (Day, error) {
	date := LocalDate(user, time.Now())

	total, entries, err := s.repo.TotalForDate(ctx, user.ID, date)
	if err != nil {
		return Day{}, err
	}

	return Day{
		Date:     date,
		TotalML:  total,
		Entries:  entries,
		TargetML: DefaultDailyTargetML,
	}, nil
}

// TodayEntries lists today's drinks, most recent first, for the undo affordance.
func (s *Service) TodayEntries(ctx context.Context, user users.User) ([]Entry, error) {
	return s.repo.ListForDate(ctx, user.ID, LocalDate(user, time.Now()))
}

// RecentDays returns the trailing `days` calendar days that have entries,
// most recent first.
func (s *Service) RecentDays(ctx context.Context, user users.User, days int) ([]Day, error) {
	if days <= 0 || days > 90 {
		days = 7
	}
	since := LocalDate(user, time.Now()).AddDate(0, 0, -(days - 1))
	return s.repo.TotalsSince(ctx, user.ID, since)
}
