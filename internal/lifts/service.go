package lifts

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Bounds mirror the column CHECKs, so a typo is a field error rather than a
// database one.
const (
	maxWeightKg = 600
	maxReps     = 100
	maxSetNo    = 50
	maxNameLen  = 120
	maxKeys     = 30
)

// history is how far back records and "last time" look. A lift not done in a
// year is starting over anyway.
const history = 365 * 24 * time.Hour

// MuscleLookup names an exercise's muscles from its catalog slug.
type MuscleLookup interface {
	Resolve(ctx context.Context, slugs []string) (map[string]exercises.Exercise, error)
}

type Service struct {
	repo    *Repository
	muscles MuscleLookup
	now     func() time.Time
}

// NewService takes the exercise catalog for per-muscle sets; nil leaves
// that breakdown empty.
func NewService(repo *Repository, muscles MuscleLookup) *Service {
	return &Service{repo: repo, muscles: muscles, now: time.Now}
}

// LogInput is one set. PerformedAt defaults to now; ActivitySessionID ties
// it to the timed workout it belongs to, when there is one.
type LogInput struct {
	ExerciseName      string
	ExerciseSlug      string
	SetNumber         int
	WeightKg          float64
	Reps              int
	PerformedAt       *time.Time
	ActivitySessionID *uuid.UUID
}

// Log records a set against the local day it was done on.
func (s *Service) Log(ctx context.Context, user users.User, in LogInput) (Set, error) {
	name := strings.TrimSpace(in.ExerciseName)
	errs := apperr.FieldErrors{}
	if name == "" || len(name) > maxNameLen {
		errs = errs.Add("exerciseName", "Name the exercise.")
	}
	if in.SetNumber < 1 || in.SetNumber > maxSetNo {
		errs = errs.Add("setNumber", "Enter a set number between 1 and 50.")
	}
	if in.WeightKg < 0 || in.WeightKg > maxWeightKg {
		errs = errs.Add("weightKg", "Enter a weight between 0 and 600 kg.")
	}
	if in.Reps < 1 || in.Reps > maxReps {
		errs = errs.Add("reps", "Enter between 1 and 100 reps.")
	}
	if len(errs) > 0 {
		return Set{}, errs
	}
	at := s.now()
	if in.PerformedAt != nil {
		at = *in.PerformedAt
	}
	return s.repo.Create(ctx, user.ID, Set{
		ActivitySessionID: in.ActivitySessionID,
		LogDate:           timerange.StartOfDay(at.In(user.Location())),
		ExerciseSlug:      strings.TrimSpace(in.ExerciseSlug),
		ExerciseName:      name,
		SetNumber:         in.SetNumber,
		WeightKg:          lift.Round(in.WeightKg),
		Reps:              in.Reps,
		PerformedAt:       at,
	})
}

// Undo removes a set logged by mistake.
func (s *Service) Undo(ctx context.Context, user users.User, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, user.ID)
}

// Last is, per exercise key, the sets of the last workout that had it — what
// a new workout prefills. Keys with no history are absent.
func (s *Service) Last(ctx context.Context, user users.User, keys []string) (map[string][]Set, error) {
	clean := make([]string, 0, len(keys))
	for _, k := range keys {
		if k = lift.KeyFor("", k); k != "" && len(clean) < maxKeys {
			clean = append(clean, k)
		}
	}
	out := map[string][]Set{}
	if len(clean) == 0 {
		return out, nil
	}
	sets, err := s.repo.ListForExercises(ctx, user.ID, clean, s.now().Add(-history))
	if err != nil {
		return nil, err
	}
	per := map[string][]Set{}
	for _, set := range sets {
		per[set.Key()] = append(per[set.Key()], set)
	}
	for key, group := range per {
		out[key] = lift.LastWorkout(group)
	}
	return out, nil
}

// MuscleSets is how many sets worked a muscle, as a primary mover.
type MuscleSets struct {
	Muscle string
	Sets   int
}

// Stats is lifting over a window.
type Stats struct {
	Range timerange.Range
	// Sets inside the window, newest first.
	Sets      []Set
	Exercises []lift.Exercise
	// Records set inside the window, newest first, judged against a year of
	// history so the first week of a range is not all "records".
	Records []lift.Record
	Weekly  []lift.DayValue
	Muscles []MuscleSets
	// PriorVolumeKg is the same-length window before, for a delta.
	PriorVolumeKg float64
}

// VolumeKg is the window's total.
func (st Stats) VolumeKg() float64 {
	total := 0.0
	for _, s := range st.Sets {
		total += s.Volume()
	}
	return lift.Round(total)
}

func (s *Service) Stats(ctx context.Context, user users.User, rg timerange.Range) (Stats, error) {
	var all, prior []Set
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		all, err = s.repo.ListBetween(gctx, user.ID, rg.Since.Add(-history), rg.Until)
		return
	})
	g.Go(func() (err error) {
		prev := rg.Previous()
		prior, err = s.repo.ListBetween(gctx, user.ID, prev.Since, prev.Until)
		return
	})
	if err := g.Wait(); err != nil {
		return Stats{}, err
	}

	st := Stats{Range: rg}
	for _, set := range all {
		if rg.Contains(set.PerformedAt) {
			st.Sets = append(st.Sets, set)
		}
	}
	for _, r := range lift.Records(all) {
		if rg.Contains(r.Set.PerformedAt) {
			st.Records = append(st.Records, r)
		}
	}
	sort.SliceStable(st.Records, func(i, j int) bool { return st.Records[i].Set.PerformedAt.After(st.Records[j].Set.PerformedAt) })
	st.Exercises = lift.ByExercise(st.Sets)
	st.Weekly = lift.WeeklyVolume(st.Sets, rg.Since, rg.Until)
	for _, set := range prior {
		st.PriorVolumeKg += set.Volume()
	}
	st.PriorVolumeKg = lift.Round(st.PriorVolumeKg)

	muscles, err := s.muscleSets(ctx, st.Sets)
	if err != nil {
		return Stats{}, err
	}
	st.Muscles = muscles
	return st, nil
}

func (s *Service) muscleSets(ctx context.Context, sets []Set) ([]MuscleSets, error) {
	if s.muscles == nil {
		return nil, nil
	}
	var slugs []string
	seen := map[string]bool{}
	for _, set := range sets {
		if set.ExerciseSlug != "" && !seen[set.ExerciseSlug] {
			seen[set.ExerciseSlug] = true
			slugs = append(slugs, set.ExerciseSlug)
		}
	}
	if len(slugs) == 0 {
		return nil, nil
	}
	catalog, err := s.muscles.Resolve(ctx, slugs)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, set := range sets {
		for _, m := range catalog[set.ExerciseSlug].Primary {
			counts[m]++
		}
	}
	out := make([]MuscleSets, 0, len(counts))
	for m, n := range counts {
		out = append(out, MuscleSets{Muscle: m, Sets: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sets != out[j].Sets {
			return out[i].Sets > out[j].Sets
		}
		return out[i].Muscle < out[j].Muscle
	})
	return out, nil
}

// Recent is the last fortnight, for the coach.
func (s *Service) Recent(ctx context.Context, user users.User) ([]Set, []lift.Record, error) {
	now := s.now()
	all, err := s.repo.ListBetween(ctx, user.ID, now.Add(-history), now.Add(time.Minute))
	if err != nil {
		return nil, nil, err
	}
	since := now.AddDate(0, 0, -14)
	var recent []Set
	for _, set := range all {
		if !set.PerformedAt.Before(since) {
			recent = append(recent, set)
		}
	}
	var records []lift.Record
	for _, r := range lift.Records(all) {
		if !r.Set.PerformedAt.Before(since) {
			records = append(records, r)
		}
	}
	return recent, records, nil
}

// NextSetNumber is the number the next set of an exercise gets today: one
// past the sets already logged for it since local midnight. Used when a set
// arrives without a number, as one said in conversation does.
func (s *Service) NextSetNumber(ctx context.Context, user users.User, slug, name string) (int, error) {
	since := timerange.StartOfDay(s.now().In(user.Location()))
	sets, err := s.repo.ListForExercises(ctx, user.ID, []string{lift.KeyFor(slug, name)}, since)
	if err != nil {
		return 0, err
	}
	return min(len(sets)+1, maxSetNo), nil
}

// Between lists the sets inside a window, newest first.
func (s *Service) Between(ctx context.Context, user users.User, rg timerange.Range) ([]Set, error) {
	return s.repo.ListBetween(ctx, user.ID, rg.Since, rg.Until)
}
