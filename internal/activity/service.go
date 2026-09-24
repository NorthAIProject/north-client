package activity

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// BiometricsLookup is the activity package's view of biometrics: just enough
// to read the current weight, so this package need not depend on
// biometrics.Service's concrete type.
type BiometricsLookup interface {
	Current(ctx context.Context, userID uuid.UUID) (biometrics.Biometric, error)
}

type Service struct {
	repo       *Repository
	biometrics BiometricsLookup
}

func NewService(repo *Repository, lookup BiometricsLookup) *Service {
	return &Service{repo: repo, biometrics: lookup}
}

// Start begins a new session. It requires a recorded biometric (to snapshot
// the weight used for the burn calculation) and refuses to open a second
// session while one is already active or paused.
func (s *Service) Start(ctx context.Context, userID uuid.UUID, activityCode string) (Session, error) {
	if _, ok := LookupMET(activityCode); !ok {
		return Session{}, apperr.Wrap(apperr.ErrValidation, "unknown activity %q", activityCode)
	}

	if _, ok, err := s.repo.Active(ctx, userID); err != nil {
		return Session{}, err
	} else if ok {
		return Session{}, apperr.Wrap(apperr.ErrConflict, "a session is already open")
	}

	bio, err := s.biometrics.Current(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return Session{}, apperr.Wrap(apperr.ErrValidation, "record your biometrics before tracking activity")
		}
		return Session{}, err
	}

	return s.repo.Create(ctx, userID, activityCode, bio.WeightKg)
}

func (s *Service) Pause(ctx context.Context, id, userID uuid.UUID) (Session, error) {
	session, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return Session{}, err
	}
	if session.Status != StatusActive {
		return Session{}, apperr.Wrap(apperr.ErrConflict, "session is not active")
	}
	return s.repo.SetPaused(ctx, id, userID)
}

func (s *Service) Resume(ctx context.Context, id, userID uuid.UUID) (Session, error) {
	session, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return Session{}, err
	}
	if session.Status != StatusPaused {
		return Session{}, apperr.Wrap(apperr.ErrConflict, "session is not paused")
	}

	pausedSeconds := int(time.Since(*session.PausedAt).Seconds())
	return s.repo.SetResumed(ctx, id, userID, session.TotalPausedSeconds+pausedSeconds)
}

// Stop ends a session and computes its calorie burn:
// MET * weight_kg_snapshot * elapsed_hours.
func (s *Service) Stop(ctx context.Context, id, userID uuid.UUID) (Session, error) {
	session, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return Session{}, err
	}
	if !session.IsOpen() {
		return Session{}, apperr.Wrap(apperr.ErrConflict, "session is already finished")
	}

	met, _ := LookupMET(session.ActivityCode) // validated at Start; always found

	now := time.Now()
	elapsedHours := session.Elapsed(now).Hours()
	calories := met.Value * session.WeightKgSnapshot * elapsedHours

	return s.repo.Complete(ctx, id, userID, now, calories)
}

func (s *Service) Cancel(ctx context.Context, id, userID uuid.UUID) error {
	session, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return err
	}
	if !session.IsOpen() {
		return apperr.Wrap(apperr.ErrConflict, "session is already finished")
	}
	return s.repo.Cancel(ctx, id, userID)
}

// Active returns the user's open session, if any.
func (s *Service) Active(ctx context.Context, userID uuid.UUID) (Session, bool, error) {
	return s.repo.Active(ctx, userID)
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, limit int) ([]Session, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.List(ctx, userID, limit)
}

// TotalCaloriesSince sums the calories burned by completed sessions since a
// point in time, e.g. the user's local midnight for "today's burn."
func (s *Service) TotalCaloriesSince(ctx context.Context, userID uuid.UUID, since time.Time) (float64, error) {
	return s.repo.SumCaloriesSince(ctx, userID, since)
}

// CaloriesBetween sums completed sessions inside a window. Bounded at both
// ends so a window and the one before it can be compared without the session
// on the boundary landing in both.
func (s *Service) CaloriesBetween(ctx context.Context, userID uuid.UUID, rg timerange.Range) (float64, error) {
	return s.repo.SumCaloriesBetween(ctx, userID, rg.Since, rg.Until)
}

// ListBetween returns the sessions finished inside a window, newest first.
func (s *Service) ListBetween(ctx context.Context, userID uuid.UUID, rg timerange.Range) ([]Session, error) {
	return s.repo.ListBetween(ctx, userID, rg.Since, rg.Until)
}

// LogInput is a finished session the person is entering after the fact:
// "ran 5 km in 28 minutes this morning."
type LogInput struct {
	ActivityCode string

	// StartedAt zero means the session has just finished, so it is placed
	// to end now.
	StartedAt time.Time
	Duration  time.Duration

	// DistanceM is optional. Zero means not recorded, not zero metres.
	DistanceM float64
}

// Bounds on a logged session. Generous, because a person entering their own
// day is not the threat; they exist so a typo of 300 minutes as 3000 does not
// become a training week.
const (
	maxLogDuration = 24 * time.Hour
	maxLogDistance = 500_000.0 // metres; nobody logs more than an ultra by hand

	// futureSlack tolerates a phone clock a few minutes ahead of the server.
	futureSlack = 5 * time.Minute
)

// Log records a session that already happened, costed exactly as Stop costs
// one: MET * weight * hours. It does not care whether a timer session is open,
// because logging yesterday's run has nothing to do with the one in progress.
func (s *Service) Log(ctx context.Context, userID uuid.UUID, in LogInput) (Session, error) {
	met, ok := LookupMET(in.ActivityCode)
	if !ok {
		return Session{}, apperr.Wrap(apperr.ErrValidation, "unknown activity %q", in.ActivityCode)
	}
	if in.Duration < time.Minute || in.Duration > maxLogDuration {
		return Session{}, apperr.Wrap(apperr.ErrValidation, "a session lasts between a minute and a day")
	}
	if in.DistanceM < 0 || in.DistanceM > maxLogDistance {
		return Session{}, apperr.Wrap(apperr.ErrValidation, "distance must be between 0 and %.0f km", maxLogDistance/1000)
	}

	now := time.Now()
	if in.StartedAt.IsZero() {
		in.StartedAt = now.Add(-in.Duration)
	}
	if in.StartedAt.Add(in.Duration).After(now.Add(futureSlack)) {
		return Session{}, apperr.Wrap(apperr.ErrValidation, "a session cannot end in the future")
	}

	bio, err := s.biometrics.Current(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return Session{}, apperr.Wrap(apperr.ErrValidation, "record your biometrics before logging activity")
		}
		return Session{}, err
	}

	calories := met.Value * bio.WeightKg * in.Duration.Hours()
	return s.repo.Log(ctx, userID, in, bio.WeightKg, calories)
}

// ImportInput is one finished session arriving from a provider sync.
//
// It carries its own weight rather than looking one up: the caller is the
// one holding the context about when the session happened.
type ImportInput struct {
	UserID       uuid.UUID
	ActivityCode string
	Source       string
	ExternalID   string
	StartedAt    time.Time
	EndedAt      time.Time
	WeightKg     float64

	// Calories is the provider's own figure when it has one. Zero means
	// "unknown", and Import falls back to the MET estimate.
	Calories float64
}

// Import records a finished session from a provider, reporting false when it
// had already been imported.
//
// The provider's own calorie figure wins when present: a device that watched
// someone's heart rate knows more than a MET table does. The estimate is the
// fallback, computed exactly as Stop computes it, so a synced session and a
// logged one are costed the same way.
func (s *Service) Import(ctx context.Context, in ImportInput) (Session, bool, error) {
	met, ok := LookupMET(in.ActivityCode)
	if !ok {
		return Session{}, false, apperr.Wrap(apperr.ErrValidation, "unknown activity %q", in.ActivityCode)
	}
	if in.ExternalID == "" {
		return Session{}, false, apperr.Wrap(apperr.ErrValidation, "an imported session needs an external id")
	}

	duplicate, found, err := s.sameWorkoutFromAnotherSource(ctx, in)
	if err != nil {
		return Session{}, false, err
	}
	if found {
		return duplicate, false, nil
	}

	if in.Calories <= 0 {
		hours := in.EndedAt.Sub(in.StartedAt).Hours()
		if hours < 0 {
			hours = 0
		}
		in.Calories = met.Value * in.WeightKg * hours
	}

	return s.repo.Import(ctx, in)
}

// sameWorkoutFromAnotherSource finds a completed session that is this import
// seen by a different provider: a watch run that also syncs to Strava, or a
// session timed in the app and written to Apple Health, which then syncs back.
//
// UNIQUE (source, external_id) cannot catch these, because each provider has
// its own id for the same hour. They are recognised by time instead: sharing
// at least half of the shorter session. Back-to-back workouts, which touch or
// overlap by a few minutes, stay two workouts.
func (s *Service) sameWorkoutFromAnotherSource(ctx context.Context, in ImportInput) (Session, bool, error) {
	// A session overlapping this one must end after it starts, and one that
	// ends more than a day after this one finishes is not a workout.
	candidates, err := s.repo.ListBetween(ctx, in.UserID, in.StartedAt, in.EndedAt.Add(24*time.Hour))
	if err != nil {
		return Session{}, false, err
	}

	for _, c := range candidates {
		if c.Source == in.Source || c.EndedAt == nil {
			continue
		}
		overlap := minTime(*c.EndedAt, in.EndedAt).Sub(maxTime(c.StartedAt, in.StartedAt))
		shorter := min(c.EndedAt.Sub(c.StartedAt), in.EndedAt.Sub(in.StartedAt))
		if overlap > 0 && 2*overlap >= shorter {
			return c, true, nil
		}
	}
	return Session{}, false, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
