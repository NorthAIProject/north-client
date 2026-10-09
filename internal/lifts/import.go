package lifts

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/lifts/hevy"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// SourceHevy marks sessions imported from a Hevy export.
const SourceHevy = "hevy"

// importActivity is what an imported lifting session is recorded as.
const importActivity = "strength_training"

// Activities records imported sessions; activity.Service is one.
type Activities interface {
	Import(ctx context.Context, in activity.ImportInput) (activity.Session, bool, error)
}

// BodyWeights gives the weight a session's calories are estimated from.
type BodyWeights interface {
	Current(ctx context.Context, userID uuid.UUID) (biometrics.Biometric, error)
}

// ExerciseMatchers builds a matcher over the exercise catalog.
type ExerciseMatchers interface {
	Matcher(ctx context.Context) (*exercises.Matcher, error)
}

// WithImports lets the service import other apps' history. weights may be
// nil; sessions then carry no calorie estimate.
func (s *Service) WithImports(activities Activities, weights BodyWeights, matchers ExerciseMatchers) *Service {
	s.activities = activities
	s.weights = weights
	s.matchers = matchers
	return s
}

// ImportResult is what an import did.
type ImportResult struct {
	// Workouts had their sets imported, into a new session or one another
	// source already recorded; Duplicates were already here, from an earlier
	// import of the same file.
	Workouts   int
	Duplicates int
	Sets       int
	// Skipped are rows that were not importable sets: cardio and timed rows,
	// or a weight or rep count outside what the app accepts.
	Skipped int
	// Unmatched are exercise names with no catalog entry, kept as typed. They
	// count toward volume and records, but not toward muscles.
	Unmatched []string
}

// ImportHevy reads a Hevy workout export into sessions and sets.
//
// Each workout becomes a finished strength session with source "hevy", and
// its sets are logged against it with their kind and effort. Importing the
// same file twice adds nothing. A workout that Apple Health already recorded
// (Hevy writes there too) keeps that session and gains the sets, rather than
// appearing twice.
func (s *Service) ImportHevy(ctx context.Context, user users.User, r io.Reader) (ImportResult, error) {
	if s.activities == nil || s.matchers == nil {
		return ImportResult{}, apperr.Wrap(apperr.ErrValidation, "imports are not available here")
	}
	parsed, err := hevy.Parse(r, user.Location())
	if err != nil {
		if errors.Is(err, hevy.ErrNotHevy) {
			return ImportResult{}, apperr.FieldErrors{}.Add("file", "That file is not a Hevy workout export. In Hevy: Profile → Settings → Export & Import Data → Export Workouts.")
		}
		return ImportResult{}, apperr.FieldErrors{}.Add("file", "That file could not be read: "+err.Error())
	}
	matcher, err := s.matchers.Matcher(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	weightKg, err := s.bodyWeight(ctx, user)
	if err != nil {
		return ImportResult{}, err
	}

	result := ImportResult{Skipped: parsed.Skipped}
	unmatched := map[string]bool{}
	for _, w := range parsed.Workouts {
		if len(w.Sets) == 0 {
			continue
		}
		session, fresh, err := s.importSession(ctx, user, w, weightKg)
		if err != nil {
			return result, err
		}
		if !fresh {
			result.Duplicates++
			continue
		}
		result.Workouts++
		for i, set := range w.Sets {
			slug, ok := matcher.Match(set.Exercise)
			if !ok {
				unmatched[set.Exercise] = true
			}
			if _, err := s.Log(ctx, user, LogInput{
				ExerciseName:      set.Exercise,
				ExerciseSlug:      slug,
				SetNumber:         set.Number,
				WeightKg:          set.WeightKg,
				Reps:              set.Reps,
				PerformedAt:       setTime(w, i),
				ActivitySessionID: &session,
				Kind:              set.Kind,
				RIR:               set.RIR,
			}); err != nil {
				return result, apperr.Wrap(err, "import set %d of %q", i+1, w.Title)
			}
			result.Sets++
		}
	}
	for name := range unmatched {
		result.Unmatched = append(result.Unmatched, name)
	}
	sort.Strings(result.Unmatched)
	return result, nil
}

// importSession finds or writes the session a workout's sets belong to.
// fresh is false when the workout's sets are already here.
func (s *Service) importSession(ctx context.Context, user users.User, w hevy.Workout, weightKg float64) (uuid.UUID, bool, error) {
	session, created, err := s.activities.Import(ctx, activity.ImportInput{
		UserID:       user.ID,
		ActivityCode: importActivity,
		Source:       SourceHevy,
		ExternalID:   hevyExternalID(user.ID, w),
		StartedAt:    w.Start,
		EndedAt:      w.End,
		WeightKg:     weightKg,
	})
	switch {
	case err != nil:
		return uuid.Nil, false, err
	case created:
		return session.ID, true, nil
	case session.ID == uuid.Nil || session.Source == SourceHevy:
		// This very workout was imported before.
		return uuid.Nil, false, nil
	}
	// Another source recorded the same workout. Its sets come from here
	// unless an earlier import already put them there.
	existing, err := s.repo.ListForSession(ctx, user.ID, session.ID)
	if err != nil {
		return uuid.Nil, false, err
	}
	return session.ID, len(existing) == 0, nil
}

// hevyExternalID names a workout for deduplication. Hevy's export has no
// workout id, so it is the start time and title — and the person, because
// the (source, external id) key is shared by everyone.
func hevyExternalID(userID uuid.UUID, w hevy.Workout) string {
	return userID.String() + "/" + w.Start.UTC().Format(time.RFC3339) + "/" + strings.ToLower(strings.TrimSpace(w.Title))
}

// setTime spaces a workout's sets a minute apart from its start, so they
// keep the file's order, never running past its end. The export carries no
// time per set.
func setTime(w hevy.Workout, i int) *time.Time {
	at := w.Start.Add(time.Duration(i) * time.Minute)
	if w.End.After(w.Start) && at.After(w.End) {
		at = w.End
	}
	return &at
}

func (s *Service) bodyWeight(ctx context.Context, user users.User) (float64, error) {
	if s.weights == nil {
		return 0, nil
	}
	current, err := s.weights.Current(ctx, user.ID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return 0, nil
		}
		return 0, apperr.Wrap(err, "read body weight for import")
	}
	return current.WeightKg, nil
}
