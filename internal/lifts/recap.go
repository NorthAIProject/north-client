package lifts

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Sessions reads the timed workout a recap describes.
type Sessions interface {
	Get(ctx context.Context, id, userID uuid.UUID) (activity.Session, error)
}

// Prescriptions is what the person's current plan asks for on a weekday: its
// focus and how many sets in total. ok is false when the plan has no such day.
type Prescriptions interface {
	Prescription(ctx context.Context, user users.User, weekday string) (focus string, sets int, ok bool, err error)
}

// WithRecaps lets the service describe finished workouts. prescriptions may
// be nil; the recap then says "18 sets" rather than "18 of 20 sets".
func (s *Service) WithRecaps(sessions Sessions, prescriptions Prescriptions) *Service {
	s.sessions = sessions
	s.prescriptions = prescriptions
	return s
}

// Recap is one finished workout in a sentence and per exercise, each against
// the last time it was done. It is computed, not generated: the finish
// screen, the plan page, Insights and the coach all read the same numbers.
func (s *Service) Recap(ctx context.Context, user users.User, sessionID uuid.UUID) (lift.Recap, error) {
	if s.sessions == nil {
		return lift.Recap{}, apperr.Wrap(apperr.ErrNotFound, "recaps are not wired")
	}
	session, err := s.sessions.Get(ctx, sessionID, user.ID)
	if err != nil {
		return lift.Recap{}, err
	}
	if session.Status != activity.StatusCompleted {
		return lift.Recap{}, apperr.Wrap(apperr.ErrConflict, "the session is not finished")
	}
	return s.recapFor(ctx, user, session)
}

// LatestRecap is the newest finished workout with sets inside rg. false when
// no set in the window belongs to a finished session.
func (s *Service) LatestRecap(ctx context.Context, user users.User, rg timerange.Range) (lift.Recap, bool, error) {
	if s.sessions == nil {
		return lift.Recap{}, false, nil
	}
	sets, err := s.repo.ListBetween(ctx, user.ID, rg.Since, rg.Until)
	if err != nil {
		return lift.Recap{}, false, err
	}
	tried := map[uuid.UUID]bool{}
	for _, set := range sets { // newest first
		if set.ActivitySessionID == nil || tried[*set.ActivitySessionID] {
			continue
		}
		id := *set.ActivitySessionID
		tried[id] = true
		session, err := s.sessions.Get(ctx, id, user.ID)
		switch {
		case apperr.Is(err, apperr.ErrNotFound):
			continue
		case err != nil:
			return lift.Recap{}, false, err
		case session.Status != activity.StatusCompleted:
			// Still running: the finish screen shows it, not the history.
			continue
		}
		recap, err := s.recapFor(ctx, user, session)
		if err != nil {
			return lift.Recap{}, false, err
		}
		return recap, true, nil
	}
	return lift.Recap{}, false, nil
}

func (s *Service) recapFor(ctx context.Context, user users.User, session activity.Session) (lift.Recap, error) {
	current, err := s.repo.ListForSession(ctx, user.ID, session.ID)
	if err != nil {
		return lift.Recap{}, err
	}
	var earlier []Set
	if keys := uniqueKeys(current); len(keys) > 0 {
		earlier, err = s.repo.ListForExercisesBefore(ctx, user.ID, keys, session.StartedAt.Add(-history), session.StartedAt)
		if err != nil {
			return lift.Recap{}, err
		}
	}

	weekday := planWeekdayOf(session, user.Location())
	var focus string
	var prescribed int
	if weekday != "" && s.prescriptions != nil {
		var ok bool
		focus, prescribed, ok, err = s.prescriptions.Prescription(ctx, user, weekday)
		if err != nil {
			return lift.Recap{}, err
		}
		if !ok {
			focus, prescribed = "", 0
		}
	}

	var calories float64
	if session.CaloriesBurned != nil {
		calories = *session.CaloriesBurned
	}
	end := s.now()
	if session.EndedAt != nil {
		end = *session.EndedAt
	}

	recap := lift.BuildRecap(session.Elapsed(end), calories, prescribed, current, earlier)
	recap.SessionID = session.ID
	recap.StartedAt = session.StartedAt
	recap.PlanWeekday = weekday
	recap.Focus = focus
	return recap, nil
}

// planWeekdayOf is the plan day a session finished: the one it was started
// for, or for an untagged strength session the day it was done.
func planWeekdayOf(session activity.Session, loc *time.Location) string {
	if weekday, ok := activity.CanonicalWeekday(session.PlanWeekday); ok {
		return weekday
	}
	if activity.IsStrength(session.ActivityCode) {
		return session.StartedAt.In(loc).Weekday().String()
	}
	return ""
}

func uniqueKeys(sets []Set) []string {
	seen := map[string]bool{}
	var keys []string
	for _, set := range sets {
		if k := set.Key(); !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}
