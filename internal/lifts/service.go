package lifts

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/bodymap"
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

	// sessions and prescriptions are for recaps; see WithRecaps.
	sessions      Sessions
	prescriptions Prescriptions

	// activities, weights and matchers are for imports; see WithImports.
	activities Activities
	weights    BodyWeights
	matchers   ExerciseMatchers
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
	// Kind is one of lift.Kinds; empty means a work set.
	Kind string
	// RIR is reps in reserve, 0 to lift.MaxRIR; nil when not given.
	RIR *int
	// ClientID is the id the client gave the set before sending it, so a
	// retry of the same upload returns the set already logged rather than
	// logging it twice. Nil logs a new set every time.
	ClientID *uuid.UUID
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
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = lift.KindWork
	}
	if !lift.ValidKind(kind) {
		errs = errs.Add("kind", "Choose work, warm-up or drop set.")
	}
	if in.RIR != nil && (*in.RIR < 0 || *in.RIR > lift.MaxRIR) {
		errs = errs.Add("rir", "Enter reps in reserve between 0 and 10.")
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
		WeightKg:          util.RoundHalfUpToScale(in.WeightKg, 1),
		Reps:              in.Reps,
		PerformedAt:       at,
		Kind:              kind,
		RIR:               in.RIR,
	}, in.ClientID)
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
	return util.RoundHalfUpToScale(total, 1)
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
	st.PriorVolumeKg = util.RoundHalfUpToScale(st.PriorVolumeKg, 1)

	muscles, err := s.muscleSets(ctx, st.Sets)
	if err != nil {
		return Stats{}, err
	}
	st.Muscles = muscles
	return st, nil
}

func (s *Service) muscleSets(ctx context.Context, sets []Set) ([]MuscleSets, error) {
	catalog, err := s.catalog(ctx, sets)
	if err != nil || len(catalog) == 0 {
		return nil, err
	}
	counts := map[string]int{}
	for _, set := range lift.Working(sets) {
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

// catalog is the catalog entry of every exercise in sets that has one. Empty
// without a catalog wired or with only typed-in exercises.
func (s *Service) catalog(ctx context.Context, sets []Set) (map[string]exercises.Exercise, error) {
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
	return s.muscles.Resolve(ctx, slugs)
}

// Readiness is how fatigued or detrained each muscle is now, from a year of
// sets — far enough back to notice a muscle left alone for months.
func (s *Service) Readiness(ctx context.Context, user users.User) (lift.Load, error) {
	sets, weights, now, err := s.yearOfSets(ctx, user)
	if err != nil {
		return lift.Load{}, err
	}
	return lift.LoadOf(sets, weights, now), nil
}

// BodyMapMaxDays bounds how far back the body figure's heat may look.
const BodyMapMaxDays = 28

// BodyMap is the body figure: which regions the last days of training heated,
// and when each was last trained. A year of sets is read so that a muscle
// left alone for months still knows when it was last worked.
func (s *Service) BodyMap(ctx context.Context, user users.User, days int) (bodymap.Map, error) {
	if days < 1 || days > BodyMapMaxDays {
		return bodymap.Map{}, apperr.FieldErrors{}.Add("days", fmt.Sprintf("Use a number of days from 1 to %d.", BodyMapMaxDays))
	}
	sets, weights, now, err := s.yearOfSets(ctx, user)
	if err != nil {
		return bodymap.Map{}, err
	}
	return bodymap.FromHeat(lift.HeatOf(sets, weights, now, user.Location(), days)), nil
}

// yearOfSets is the person's sets from the last year, the catalog muscle
// weights of the exercises in them, and the moment they were read at.
func (s *Service) yearOfSets(ctx context.Context, user users.User) ([]Set, lift.Weights, time.Time, error) {
	now := s.now().In(user.Location())
	sets, err := s.repo.ListBetween(ctx, user.ID, now.Add(-history), now.Add(time.Minute))
	if err != nil {
		return nil, nil, now, err
	}
	catalog, err := s.catalog(ctx, sets)
	if err != nil {
		return nil, nil, now, err
	}
	weights := make(lift.Weights, len(catalog))
	for slug, e := range catalog {
		weights[slug] = lift.MuscleWeights(e.Primary, e.Secondary)
	}
	return sets, weights, now, nil
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

// MatchExercise is the catalog slug a typed exercise name means, or "" when
// none fits or no catalog is wired. A set logged by name alone then still
// reaches the muscles its exercise works.
func (s *Service) MatchExercise(ctx context.Context, name string) (string, error) {
	if s.matchers == nil {
		return "", nil
	}
	m, err := s.matchers.Matcher(ctx)
	if err != nil {
		return "", err
	}
	slug, _ := m.Match(name)
	return slug, nil
}

// Today is the sets logged since local midnight, oldest first, and the names
// of exercises done in the last fortnight, most recent first — what a form
// for the next set offers.
func (s *Service) Today(ctx context.Context, user users.User) ([]Set, []string, error) {
	now := s.now().In(user.Location())
	since := now.AddDate(0, 0, -14)
	sets, err := s.repo.ListBetween(ctx, user.ID, since, now.Add(time.Minute))
	if err != nil {
		return nil, nil, err
	}
	midnight := timerange.StartOfDay(now)
	var today []Set
	var names []string
	seen := map[string]bool{}
	for _, set := range sets { // newest first
		if !set.PerformedAt.Before(midnight) {
			today = append(today, set)
		}
		if !seen[set.Key()] {
			seen[set.Key()] = true
			names = append(names, set.ExerciseName)
		}
	}
	slices.Reverse(today)
	return today, names, nil
}
