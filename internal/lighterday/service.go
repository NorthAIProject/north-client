// Package lighterday offers an easier session on a morning the body has not
// recovered: when today's recovery (insights.RecoveryData.Low) is under the
// person's usual, they may train about 60% of today's sets instead of the
// plan. It lasts one day.
//
// Usually it is an offer the person takes or declines. When they let the
// coach adjust their training (notifications.Prefs.CoachAdjustsTraining), the
// coach takes it for them on a low morning and says so, and they can undo it:
// a choice with source "coach", reverted by choosing to keep the plan.
package lighterday

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/insights"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Choices.
const (
	ChoiceLighter = "lighter"
	ChoiceKeep    = "keep"
)

// Who made the choice.
const (
	SourcePerson = "person"
	SourceCoach  = "coach"
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
	// Source is who made the choice: the person, or the coach on a low
	// morning. Empty with no choice.
	Source string
}

// Offered reports whether the offer should show: a low morning, a session
// still to do, and no answer yet.
func (t Today) Offered() bool {
	return t.Recovery.Low() && t.Due && !t.Done && t.Choice == ""
}

// Lighter reports whether today trains lighter.
func (t Today) Lighter() bool { return t.Choice == ChoiceLighter }

type Service struct {
	repo     Repository
	recovery Recovery
	sessions SessionReader
	awards   Awards
	now      func() time.Time
}

// Awards grants the badge for listening to a low morning. awards.Service
// satisfies it.
type Awards interface {
	Grant(ctx context.Context, userID uuid.UUID, at time.Time, badgeID string)
}

func (s *Service) WithAwards(a Awards) *Service { s.awards = a; return s }

// Repository is the persistence Service depends on: the one answer a person
// gives each day. NewRepository is what the process wires, and a test can
// hand Service anything that keeps those answers.
type Repository interface {
	SetChoice(ctx context.Context, userID uuid.UUID, day time.Time, choice, source string) error
	Choice(ctx context.Context, userID uuid.UUID, day time.Time) (choice, source string, err error)
}

func NewService(repo Repository, r Recovery, s SessionReader) *Service {
	return &Service{repo: repo, recovery: r, sessions: s, now: time.Now}
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
	choice, source, err := s.choice(ctx, user, today)
	if err != nil {
		return Today{}, err
	}
	out.Choice, out.Source = choice, source
	return out, nil
}

// Choose records the answer for today. Taking it lighter needs a session
// still to do today; keeping the plan is always allowed, since it changes
// nothing.
func (s *Service) Choose(ctx context.Context, user users.User, choice string) (Today, error) {
	return s.ChooseAs(ctx, user, choice, SourcePerson)
}

// ChooseAs is Choose on someone's behalf: SourceCoach when the coach lightens
// a low morning itself. Undoing it is Choose(ChoiceKeep), by the person.
func (s *Service) ChooseAs(ctx context.Context, user users.User, choice, source string) (Today, error) {
	if source != SourcePerson && source != SourceCoach {
		source = SourcePerson
	}
	if choice != ChoiceLighter && choice != ChoiceKeep {
		return Today{}, apperr.FieldErrors{}.Add("choice", i18n.T(ctx, "lighterday.error.choice"))
	}
	t, err := s.Today(ctx, user)
	if err != nil {
		return Today{}, err
	}
	if choice == ChoiceLighter && (!t.Due || t.Done) {
		return Today{}, apperr.FieldErrors{}.Add("choice", i18n.T(ctx, "lighterday.error.nosession"))
	}
	today := timerange.StartOfDay(s.now().In(user.Location()))
	if err = s.repo.SetChoice(ctx, user.ID, today, choice, source); err != nil {
		return Today{}, err
	}
	t.Choice, t.Source = choice, source
	// Easing off on a low morning, by one's own choice, is the badge. The
	// coach doing it for them is not listening.
	if s.awards != nil && choice == ChoiceLighter && source == SourcePerson && t.Recovery.Low() {
		s.awards.Grant(ctx, user.ID, s.now(), "listened_to_body")
	}
	return t, nil
}

// LighterOn reports whether the person chose a lighter session on the local
// day containing at. It satisfies workouts.LighterSource.
func (s *Service) LighterOn(ctx context.Context, user users.User, at time.Time) (bool, error) {
	choice, _, err := s.choice(ctx, user, timerange.StartOfDay(at.In(user.Location())))
	return choice == ChoiceLighter, err
}

func (s *Service) choice(ctx context.Context, user users.User, day time.Time) (choice, source string, err error) {
	return s.repo.Choice(ctx, user.ID, day)
}
