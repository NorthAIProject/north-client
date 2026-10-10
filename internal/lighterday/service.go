// Package lighterday offers an easier session on a morning the body has not
// recovered: when today's recovery (insights.RecoveryData.Low) is under the
// person's usual, they may train about 60% of today's sets instead of the
// plan. It is an offer they take or decline, never a
// change made for them, and it lasts one day.
package lighterday

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/insights"
	lighterdaydb "github.com/NorthAIProject/north-client/internal/lighterday/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Choices.
const (
	ChoiceLighter = "lighter"
	ChoiceKeep    = "keep"
)

// Recovery reads today's recovery, the same one the Progress screen, the
// briefing and the coach read. insights.RecoverySource satisfies it.
type Recovery interface {
	Recovery(ctx context.Context, user users.User, now time.Time) (insights.RecoveryData, error)
}

// SessionReader says whether a plan session falls today and whether it is
// done. workouts.Service satisfies it.
type SessionReader interface {
	DueToday(ctx context.Context, user users.User, today time.Time) (string, string, bool, error)
	CompletedToday(ctx context.Context, user users.User, today time.Time) (bool, string, error)
}

// Today is what the morning looks like for the offer.
type Today struct {
	Recovery insights.RecoveryData
	// Session is today's plan session, if there is one.
	Session string
	Due     bool
	Done    bool
	// Choice is what the person answered today: lighter, keep, or empty.
	Choice string
}

// Offered reports whether the offer should show: a low morning, a session
// still to do, and no answer yet.
func (t Today) Offered() bool {
	return t.Recovery.Low() && t.Due && !t.Done && t.Choice == ""
}

// Lighter reports whether today trains lighter.
func (t Today) Lighter() bool { return t.Choice == ChoiceLighter }

type Service struct {
	q        *lighterdaydb.Queries
	recovery Recovery
	sessions SessionReader
	now      func() time.Time
}

func NewService(pool *pgxpool.Pool, r Recovery, s SessionReader) *Service {
	return &Service{q: lighterdaydb.New(pool), recovery: r, sessions: s, now: time.Now}
}

// WithClock fixes now, for tests.
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// Today reads readiness, today's session and any answer already given.
func (s *Service) Today(ctx context.Context, user users.User) (Today, error) {
	now := s.now()
	today := timerange.StartOfDay(now.In(user.Location()))
	var out Today

	if s.recovery != nil {
		r, err := s.recovery.Recovery(ctx, user, now)
		if err != nil {
			return Today{}, err
		}
		out.Recovery = r
	}
	if s.sessions != nil {
		title, _, due, err := s.sessions.DueToday(ctx, user, today)
		if err != nil {
			return Today{}, err
		}
		out.Session, out.Due = title, due
		if due {
			if out.Done, _, err = s.sessions.CompletedToday(ctx, user, today); err != nil {
				return Today{}, err
			}
		}
	}
	choice, err := s.choice(ctx, user, today)
	if err != nil {
		return Today{}, err
	}
	out.Choice = choice
	return out, nil
}

// Choose records the answer for today. Taking it lighter needs a session
// still to do today; keeping the plan is always allowed, since it changes
// nothing.
func (s *Service) Choose(ctx context.Context, user users.User, choice string) (Today, error) {
	if choice != ChoiceLighter && choice != ChoiceKeep {
		return Today{}, apperr.FieldErrors{}.Add("choice", "Choose lighter or keep.")
	}
	t, err := s.Today(ctx, user)
	if err != nil {
		return Today{}, err
	}
	if choice == ChoiceLighter && (!t.Due || t.Done) {
		return Today{}, apperr.FieldErrors{}.Add("choice", "There is no session left to do today.")
	}
	today := timerange.StartOfDay(s.now().In(user.Location()))
	if err = s.q.SetChoice(ctx, lighterdaydb.SetChoiceParams{UserID: user.ID, Day: date(today), Choice: choice}); err != nil {
		return Today{}, apperr.Wrap(err, "save lighter day")
	}
	t.Choice = choice
	return t, nil
}

// LighterOn reports whether the person chose a lighter session on the local
// day containing at. It satisfies workouts.LighterSource.
func (s *Service) LighterOn(ctx context.Context, user users.User, at time.Time) (bool, error) {
	choice, err := s.choice(ctx, user, timerange.StartOfDay(at.In(user.Location())))
	return choice == ChoiceLighter, err
}

func (s *Service) choice(ctx context.Context, user users.User, day time.Time) (string, error) {
	c, err := s.q.GetChoice(ctx, lighterdaydb.GetChoiceParams{UserID: user.ID, Day: date(day)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", apperr.Wrap(err, "load lighter day")
	}
	return c, nil
}

func date(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
