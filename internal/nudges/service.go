package nudges

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	listDefault = 20

	// Quiet for two full local days: a check-in on day D is first eligible
	// on D+3.
	missedCheckInDays = 2

	// Inclusive window of local days from today. Due today through today+7.
	goalDeadlineWindow = 7

	// Brand-new accounts are silent for today and yesterday.
	onboardGraceDays = 2
)

type accounts interface {
	ByID(ctx context.Context, id uuid.UUID) (users.User, error)
	ListOnboarded(ctx context.Context, after uuid.UUID, limit int) ([]users.User, error)
}

type checkinDays interface {
	LatestLocalDate(ctx context.Context, userID uuid.UUID) (date time.Time, ok bool, err error)
}

type activeGoals interface {
	ListActive(ctx context.Context, userID uuid.UUID) ([]goals.Goal, error)
}

type Service struct {
	repo     *Repository
	accounts accounts
	checkins checkinDays
	goals    activeGoals
	now      func() time.Time
}

func NewService(repo *Repository, accounts accounts, checkins checkinDays, goals activeGoals) *Service {
	return &Service{
		repo:     repo,
		accounts: accounts,
		checkins: checkins,
		goals:    goals,
		now:      time.Now,
	}
}

// WithClock fixes now so tests can cross a local midnight without waiting.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// CreateIfAbsent stores the draft when the dedupe key is new.
// created is false when the same (user, kind, key) already exists.
func (s *Service) CreateIfAbsent(ctx context.Context, userID uuid.UUID, d Draft) (Nudge, bool, error) {
	return s.repo.Insert(ctx, userID, d)
}

func (s *Service) ListOpen(ctx context.Context, userID uuid.UUID, limit int) ([]Nudge, error) {
	if limit <= 0 || limit > 100 {
		limit = listDefault
	}
	return s.repo.ListOpen(ctx, userID, limit)
}

func (s *Service) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.repo.CountUnread(ctx, userID)
}

func (s *Service) MarkRead(ctx context.Context, id, userID uuid.UUID) (Nudge, error) {
	return s.repo.MarkRead(ctx, id, userID)
}

func (s *Service) Dismiss(ctx context.Context, id, userID uuid.UUID) (Nudge, error) {
	return s.repo.Dismiss(ctx, id, userID)
}

func (s *Service) ListOnboarded(ctx context.Context, after uuid.UUID, limit int) ([]users.User, error) {
	if s.accounts == nil {
		return nil, nil
	}
	return s.accounts.ListOnboarded(ctx, after, limit)
}

// Evaluate applies the v1 rules and inserts any new nudges.
func (s *Service) Evaluate(ctx context.Context, user users.User) (int, error) {
	if user.NeedsOnboarding() {
		return 0, nil
	}

	today := localDate(user, s.now())
	if daysBetween(onboardedLocal(user, today), today) < onboardGraceDays {
		return 0, nil
	}

	created := 0
	n, err := s.evalMissedCheckIn(ctx, user, today)
	if err != nil {
		return created, err
	}
	created += n

	n, err = s.evalGoalDeadlines(ctx, user, today)
	return created + n, err
}

func (s *Service) evalMissedCheckIn(ctx context.Context, user users.User, today time.Time) (int, error) {
	if s.checkins == nil {
		return 0, nil
	}

	last, ok, err := s.checkins.LatestLocalDate(ctx, user.ID)
	if err != nil {
		return 0, err
	}

	var body string
	if !ok {
		body = "You have not checked in since joining."
	} else {
		quiet := daysBetween(last, today)
		if quiet <= missedCheckInDays {
			return 0, nil
		}
		body = fmt.Sprintf("It has been %d days since your last check-in.", quiet)
	}

	_, inserted, err := s.CreateIfAbsent(ctx, user.ID, Draft{
		Kind:      KindMissedCheckIn,
		DedupeKey: today.Format("2006-01-02"),
		Title:     "Check in with yourself",
		Body:      body,
		Href:      "/app/check-ins",
	})
	if err != nil {
		return 0, err
	}
	if inserted {
		return 1, nil
	}
	return 0, nil
}

func (s *Service) evalGoalDeadlines(ctx context.Context, user users.User, today time.Time) (int, error) {
	if s.goals == nil {
		return 0, nil
	}

	list, err := s.goals.ListActive(ctx, user.ID)
	if err != nil {
		return 0, err
	}

	created := 0
	for _, g := range list {
		if g.TargetDate.IsZero() {
			continue
		}
		due := calendarDay(g.TargetDate)
		until := daysBetween(today, due)
		if until < 0 || until > goalDeadlineWindow {
			continue
		}

		body := "Due today."
		if until > 0 {
			body = fmt.Sprintf("Due in %d days.", until)
		}

		_, inserted, err := s.CreateIfAbsent(ctx, user.ID, Draft{
			Kind:      KindGoalDeadline,
			DedupeKey: g.ID.String() + ":" + due.Format("2006-01-02"),
			Title:     fmt.Sprintf("“%s” is due %s", g.Title, due.Format("Monday")),
			Body:      body,
			Href:      "/app/goals/" + g.ID.String(),
		})
		if err != nil {
			return created, err
		}
		if inserted {
			created++
		}
	}
	return created, nil
}

func localDate(user users.User, at time.Time) time.Time {
	loc := user.Location()
	t := at.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func calendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func onboardedLocal(user users.User, today time.Time) time.Time {
	if user.OnboardedAt == nil {
		return today
	}
	return localDate(user, *user.OnboardedAt)
}

// daysBetween counts calendar days from a to b, ignoring clock time.
func daysBetween(a, b time.Time) int {
	aa := calendarDay(a)
	bb := calendarDay(b)
	return int(bb.Sub(aa).Hours() / 24)
}
