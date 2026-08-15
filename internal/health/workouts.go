package health

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Workout is one finished session arriving from a device.
//
// Distinct from Reading because it is not a measurement: it is an event with a
// duration, a kind, and a calorie cost, and it belongs in activity_sessions
// beside a manually logged workout rather than in a column of numbers.
type Workout struct {
	// ActivityCode names the kind of session, validated against the same
	// Go-owned MET table a manual entry uses. A provider's own vocabulary is
	// translated before it reaches here, exactly as Strava's sport types are.
	ActivityCode string

	// ExternalID is the provider's own id, and it is what makes a replayed
	// payload a no-op. Required, because without one every re-sync would log
	// the same run again.
	ExternalID string

	StartedAt time.Time
	EndedAt   time.Time

	// Calories is the device's own figure. Zero means unknown, and the MET
	// estimate is used instead — a watch that read someone's heart rate knows
	// more than a table does, but only when it actually reported.
	Calories float64
}

// ActivityImporter is the slice of activity.Service this package needs.
type ActivityImporter interface {
	Import(ctx context.Context, in activity.ImportInput) (activity.Session, bool, error)
}

// BiometricsLookup supplies the body weight a calorie estimate needs, matching
// the arrangement internal/fitness/strava uses.
type BiometricsLookup interface {
	Current(ctx context.Context, userID uuid.UUID) (biometrics.Biometric, error)
}

// WithWorkouts enables the workout half of ingest.
//
// Optional rather than a constructor argument because the two halves are
// independent: a deployment can accept readings without wiring the activity
// slice, and a payload carrying workouts to a service without it is told so
// rather than silently dropped.
func (s *Service) WithWorkouts(activities ActivityImporter, bio BiometricsLookup) *Service {
	s.activities = activities
	s.biometrics = bio
	return s
}

// IngestWorkouts records finished sessions pushed by a provider.
//
// Rejects the batch on the first bad entry, for the same reason a batch of
// readings is rejected whole: a half-applied sync leaves a gap neither side can
// see. Unlike readings this cannot be done in one transaction, because
// activity.Import owns its own dedupe and its own writes — so validation runs
// over the whole batch first, which is where every avoidable failure lives.
func (s *Service) IngestWorkouts(ctx context.Context, userID uuid.UUID, source string, workouts []Workout) (Result, error) {
	if s.activities == nil {
		return Result{}, apperr.Wrap(apperr.ErrValidation, "this deployment does not accept workouts")
	}
	if err := validateWorkouts(source, workouts); err != nil {
		return Result{}, err
	}

	weightKg, err := s.currentWeight(ctx, userID)
	if err != nil {
		return Result{}, err
	}

	written := 0
	for _, w := range workouts {
		if _, _, err := s.activities.Import(ctx, activity.ImportInput{
			UserID:       userID,
			ActivityCode: w.ActivityCode,
			Source:       source,
			ExternalID:   w.ExternalID,
			StartedAt:    w.StartedAt,
			EndedAt:      w.EndedAt,
			WeightKg:     weightKg,
			Calories:     w.Calories,
		}); err != nil {
			return Result{}, err
		}
		// Counted whether or not it was new: a bridge replaying yesterday has
		// delivered those workouts, and reporting 0 would read as a failure.
		written++
	}

	return Result{Written: written}, nil
}

// currentWeight reads the weight a calorie estimate needs.
//
// A person who has never recorded one still gets their workouts stored; the
// estimate falls back to the device's own calorie figure, and to zero when it
// has none. Refusing the whole sync over a missing profile field would be a
// worse trade than an occasional missing estimate.
func (s *Service) currentWeight(ctx context.Context, userID uuid.UUID) (float64, error) {
	if s.biometrics == nil {
		return 0, nil
	}
	current, err := s.biometrics.Current(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return 0, nil
		}
		return 0, apperr.Wrap(err, "read body weight for workout import")
	}
	return current.WeightKg, nil
}

func validateWorkouts(source string, workouts []Workout) error {
	if source == "" {
		return apperr.Wrap(apperr.ErrValidation, "a workout needs a source")
	}
	if len(workouts) > maxReadings {
		return apperr.Wrap(apperr.ErrValidation, "too many workouts: %d, max %d", len(workouts), maxReadings)
	}

	for i, w := range workouts {
		if w.ActivityCode == "" {
			return apperr.Wrap(apperr.ErrValidation, "workout %d has no activity code", i)
		}
		if w.ExternalID == "" {
			return apperr.Wrap(apperr.ErrValidation, "workout %d (%s) has no external id", i, w.ActivityCode)
		}
		if w.StartedAt.IsZero() {
			return apperr.Wrap(apperr.ErrValidation, "workout %d (%s) has no start time", i, w.ActivityCode)
		}
		if !w.EndedAt.After(w.StartedAt) {
			return apperr.Wrap(apperr.ErrValidation,
				"workout %d (%s) ends at or before it starts", i, w.ActivityCode)
		}
	}
	return nil
}
