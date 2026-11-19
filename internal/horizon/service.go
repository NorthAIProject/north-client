// Package horizon shows where a person's training is heading: their lifts,
// weight and waist as ranges over the next twelve weeks, the body figure on
// each day ahead, and what a few changes of plan would do to all of it.
//
// It owns no measurements. Everything is read from the slices that do (lifts,
// workouts, biometrics, health, progress checks, recovery) and handed to the
// forecast leaf, which is arithmetic with no model call. What this package
// stores is its own track record: see snapshots.go.
package horizon

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/bodymap"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/horizon/forecast"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// The slices Horizon reads, each as the one method it needs.
type (
	// Lifts is a year of sets and the muscles their exercises load.
	Lifts interface {
		History(ctx context.Context, user users.User) ([]lift.Set, lift.Weights, time.Time, error)
		// MatchExercises is the catalog slug each typed name means; a name
		// the catalog has nothing like is left out.
		MatchExercises(ctx context.Context, names []string) (map[string]string, error)
		// MuscleWeights is what a set of each exercise loads.
		MuscleWeights(ctx context.Context, slugs []string) (lift.Weights, error)
	}
	// Plans is the plan the person follows; ErrNotFound when there is none.
	Plans interface {
		ActivePlan(ctx context.Context, userID uuid.UUID) (workouts.StoredPlan, error)
	}
	// Measurements are weights typed into the app, newest first.
	Measurements interface {
		History(ctx context.Context, userID uuid.UUID, limit int) ([]biometrics.Biometric, error)
	}
	// Readings are weights synced from a phone or a scale.
	Readings interface {
		Between(ctx context.Context, userID uuid.UUID, metric string, since, until time.Time) ([]health.Stored, error)
	}
	// Checks are progress checks, which carry the waist.
	Checks interface {
		History(ctx context.Context, user users.User) ([]progresscheck.Check, error)
	}
	// Recovery counts how many of the last seven mornings read low.
	Recovery interface {
		LowMornings(ctx context.Context, user users.User, now time.Time) (int, error)
	}
	// Targets is the weight the person is aiming at.
	Targets interface {
		Get(ctx context.Context, userID uuid.UUID) (preferences.Preferences, error)
	}
)

// Options are the slices Horizon reads. Lifts is required; a nil anything
// else means that part of the picture is simply absent.
type Options struct {
	Lifts        Lifts
	Plans        Plans
	Measurements Measurements
	Readings     Readings
	Checks       Checks
	Recovery     Recovery
	Targets      Targets
	Snapshots    *Repository
}

type Service struct {
	o Options
}

func NewService(o Options) *Service { return &Service{o: o} }

// bodyWindow is how far back weights and waists are read: enough for the
// backtest to have a record to score.
const bodyWindow = 26 * 7 * forecast.Day

// Horizon is the whole forecast for one person at one moment.
type Horizon struct {
	At       time.Time
	Scenario forecast.Scenario

	// Lifts are the most-trained lifts, ready or not, at most MaxLifts.
	Lifts  []forecast.LiftForecast
	Weight forecast.Series
	Waist  forecast.Series
	// WeightETA is when the target weight is expected; TargetWeightKg is
	// nil when none is set.
	TargetWeightKg *float64
	WeightETA      forecast.ETA

	// Frames are the body on each day ahead.
	Frames []BodyFrame

	Deload forecast.Deload
	// HasPlan is false when no plan is followed, so the frames show
	// training simply stopping.
	HasPlan bool
}

// BodyFrame is the figure on one day ahead.
type BodyFrame struct {
	OffsetDays int
	At         time.Time
	Shape      bodymap.Shape
	Regions    []RegionLoad
}

// RegionLoad is one region of the figure on one day.
type RegionLoad struct {
	Region string
	// Fatigue is 0 (fresh) to 1 (spent).
	Fatigue float64
	// Retained is the body map's detraining shade (lift.Strength): 1 when
	// trained within two weeks, down to 0.5. It draws the figure; it is not a
	// share of any lift's strength.
	Retained float64
}

// Ready reports whether anything at all could be forecast.
func (h Horizon) Ready() bool {
	if h.Weight.Status == forecast.StatusReady || h.Waist.Status == forecast.StatusReady {
		return true
	}
	for _, l := range h.Lifts {
		if l.Status == forecast.StatusReady {
			return true
		}
	}
	return false
}

// Needs is everything still missing, in the order worth showing: lifts
// closest to being ready first, then weight, then waist.
func (h Horizon) Needs() []forecast.Need {
	var out []forecast.Need
	for _, l := range h.Lifts {
		if l.Status != forecast.StatusReady {
			out = append(out, l.Needs...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Want-out[i].Have < out[j].Want-out[j].Have })
	if len(out) > 3 {
		out = out[:3]
	}
	if len(h.Lifts) == 0 {
		out = append(out, forecast.Need{Code: forecast.NeedLiftSessions, Have: 0, Want: forecast.LiftMinSessions})
	}
	if h.Weight.Status != forecast.StatusReady {
		out = append(out, h.Weight.Needs...)
	}
	if h.Waist.Status != forecast.StatusReady {
		out = append(out, h.Waist.Needs...)
	}
	return out
}

// Forecast builds the horizon for a person under a what-if (the zero
// Scenario for the plain forecast).
func (s *Service) Forecast(ctx context.Context, user users.User, sc forecast.Scenario) (Horizon, error) {
	sc = sc.Clamp()
	sets, weights, now, err := s.o.Lifts.History(ctx, user)
	if err != nil {
		return Horizon{}, err
	}
	out := Horizon{At: now, Scenario: sc}

	histories := forecast.LiftHistories(sets)
	if len(histories) > forecast.MaxLifts {
		histories = histories[:forecast.MaxLifts]
	}
	progress := make([]string, 0, len(histories))
	for _, h := range histories {
		f := forecast.ForecastLift(h, now, sc.Pause())
		out.Lifts = append(out.Lifts, f)
		progress = append(progress, f.Plateau)
	}

	body, err := s.body(ctx, user, now)
	if err != nil {
		return Horizon{}, err
	}
	out.Weight = forecast.ForecastWeight(body.weighIns, now)
	out.Waist = forecast.ForecastWaist(body.waists, now)
	if body.targetKg != nil {
		out.TargetWeightKg = body.targetKg
		out.WeightETA = forecast.ETAFor(out.Weight, *body.targetKg)
	}

	sessions, hasPlan, err := s.plan(ctx, user)
	if err != nil {
		return Horizon{}, err
	}
	out.HasPlan = hasPlan
	if weights, err = s.withPlanWeights(ctx, weights, sessions); err != nil {
		return Horizon{}, err
	}
	for _, f := range forecast.Frames(sets, sessions, weights, now, sc) {
		out.Frames = append(out.Frames, BodyFrame{
			OffsetDays: f.OffsetDays,
			At:         f.At,
			Shape:      body.shapeAt(out.Weight, out.Waist, f.OffsetDays),
			Regions:    regions(f.Load),
		})
	}

	low := 0
	if s.o.Recovery != nil {
		if low, err = s.o.Recovery.LowMornings(ctx, user, now); err != nil {
			return Horizon{}, err
		}
	}
	out.Deload = forecast.SuggestDeload(forecast.DeloadInput{
		Progress:          progress,
		WeeksSinceLighter: forecast.WeeksSinceLighter(sets, now, user.Location()),
		LowRecoveryDays:   low,
		RepsFalling:       forecast.RepsFalling(sets, now),
	})
	return out, nil
}

// Lift is one lift's forecast with the history it was drawn from.
type Lift struct {
	forecast.LiftForecast
	// History is the best estimated max of each day it was trained.
	History []forecast.Point
	// Past are the four-week forecasts the method would have made, each
	// with what then happened.
	Past []forecast.Miss
}

// Lift is the detail of one lift, by its key. ErrNotFound for a lift the
// person has never logged a loaded set of.
func (s *Service) Lift(ctx context.Context, user users.User, key string) (Lift, error) {
	sets, _, now, err := s.o.Lifts.History(ctx, user)
	if err != nil {
		return Lift{}, err
	}
	for _, h := range forecast.LiftHistories(sets) {
		if h.Key != key {
			continue
		}
		return Lift{
			LiftForecast: forecast.ForecastLift(h, now, forecast.Pause{}),
			History:      h.Points,
			Past:         forecast.LiftMisses(h, now),
		}, nil
	}
	return Lift{}, apperr.ErrNotFound
}

// MaxNextSetKeys bounds one next-sets request: a session's worth of lifts.
const MaxNextSetKeys = 20

// NextSets suggests the next working set for each of keys that has enough
// history; a key without is simply absent from the result.
func (s *Service) NextSets(ctx context.Context, user users.User, keys []string) (map[string]forecast.NextSet, error) {
	sets, _, now, err := s.o.Lifts.History(ctx, user)
	if err != nil {
		return nil, err
	}
	// A lift is asked for by its key, or by a name: a client holding only
	// what the person typed has no catalog slug. A name is resolved the way
	// logging a set resolves it, so "squat" finds the sets logged as
	// "Barbell Squat"; one the catalog does not know matches the sets logged
	// under exactly that name. asked maps a lift's own key to how it was
	// asked for, which is what the answer is filed under.
	asked := make(map[string]string, len(keys))
	for _, k := range keys {
		asked[k] = k
	}
	logged := make(map[string]bool, len(sets))
	for _, set := range sets {
		logged[set.Key()] = true
	}
	var names []string
	for _, k := range keys {
		if !logged[k] { // a lift's own key means that lift and no other
			names = append(names, k)
		}
	}
	slugs, err := s.o.Lifts.MatchExercises(ctx, names)
	if err != nil {
		return nil, err
	}
	for _, k := range names {
		if _, taken := asked[slugs[k]]; slugs[k] != "" && !taken {
			asked[slugs[k]] = k
		}
	}
	byKey := map[string][]lift.Set{}
	// The plan's rep ranges are under the lift's own key.
	planKey := map[string]string{}
	for _, set := range sets {
		own := set.Key()
		k, ok := asked[own]
		if !ok {
			k, ok = asked[lift.KeyFor("", set.ExerciseName)]
		}
		if ok {
			byKey[k] = append(byKey[k], set)
			planKey[k] = own
		}
	}
	ranges, err := s.repRanges(ctx, user)
	if err != nil {
		return nil, err
	}
	rangeFor := func(k string) forecast.RepRange {
		if own, ok := planKey[k]; ok {
			return ranges[own]
		}
		return ranges[k]
	}
	out := make(map[string]forecast.NextSet, len(byKey))
	for k, own := range byKey {
		if next, ok := forecast.SuggestNext(forecast.NextSetInput{Sets: own, Range: rangeFor(k), Now: now}); ok {
			out[k] = next
		}
	}
	return out, nil
}

// activePlan is the followed plan, and false when there is none.
func (s *Service) activePlan(ctx context.Context, user users.User) (plan.Plan, bool, error) {
	if s.o.Plans == nil {
		return plan.Plan{}, false, nil
	}
	stored, err := s.o.Plans.ActivePlan(ctx, user.ID)
	if apperr.Is(err, apperr.ErrNotFound) {
		return plan.Plan{}, false, nil
	}
	if err != nil {
		return plan.Plan{}, false, err
	}
	return stored.Plan, true, nil
}

// plan is the followed plan as the sessions of a week. It reads the plan's
// usual week: a week the person rearranged is this week's business, and the
// forecast is about the twelve after it.
func (s *Service) plan(ctx context.Context, user users.User) ([]forecast.PlannedSession, bool, error) {
	p, ok, err := s.activePlan(ctx, user)
	if err != nil || !ok {
		return nil, false, err
	}
	slugOf, err := s.planSlugs(ctx, p)
	if err != nil {
		return nil, false, err
	}
	var out []forecast.PlannedSession
	for _, d := range p.Days {
		weekday, known := weekdayOf(d.Weekday)
		if !known {
			continue
		}
		session := forecast.PlannedSession{Weekday: weekday}
		for _, e := range d.Exercises {
			session.Exercises = append(session.Exercises, forecast.PlannedExercise{Slug: slugOf(e), Sets: e.Sets})
		}
		out = append(out, session)
	}
	return out, true, nil
}

// planSlugs gives the catalog exercise each of a plan's exercises means. A
// plan written by the coach names it; an imported one has only what the
// person's file called it, which is resolved the way logging a set resolves
// a typed name, all of them at one reading of the catalog. "" when the
// catalog has nothing like it.
func (s *Service) planSlugs(ctx context.Context, p plan.Plan) (func(plan.Exercise) string, error) {
	var names []string
	for _, d := range p.Days {
		for _, e := range d.Exercises {
			if e.CatalogSlug == "" && e.Name != "" {
				names = append(names, e.Name)
			}
		}
	}
	matched, err := s.o.Lifts.MatchExercises(ctx, names)
	if err != nil {
		return nil, err
	}
	return func(e plan.Exercise) string {
		if e.CatalogSlug != "" {
			return e.CatalogSlug
		}
		return matched[e.Name]
	}, nil
}

// withPlanWeights adds what the plan's exercises load to the weights of the
// exercises already logged. Without it a planned lift the person has not
// done yet would load nothing, and the figure would show them detraining
// through a plan they follow.
func (s *Service) withPlanWeights(ctx context.Context, weights lift.Weights, sessions []forecast.PlannedSession) (lift.Weights, error) {
	var missing []string
	for _, session := range sessions {
		for _, e := range session.Exercises {
			if _, ok := weights[e.Slug]; e.Slug != "" && !ok {
				missing = append(missing, e.Slug)
			}
		}
	}
	if len(missing) == 0 {
		return weights, nil
	}
	more, err := s.o.Lifts.MuscleWeights(ctx, missing)
	if err != nil {
		return nil, err
	}
	all := make(lift.Weights, len(weights)+len(more))
	for slug, w := range weights {
		all[slug] = w
	}
	for slug, w := range more {
		all[slug] = w
	}
	return all, nil
}

// repRanges is the plan's rep range for each lift it names, by lift key.
func (s *Service) repRanges(ctx context.Context, user users.User) (map[string]forecast.RepRange, error) {
	p, ok, err := s.activePlan(ctx, user)
	if err != nil || !ok {
		return nil, err
	}
	slugOf, err := s.planSlugs(ctx, p)
	if err != nil {
		return nil, err
	}
	out := map[string]forecast.RepRange{}
	for _, d := range p.Days {
		for _, e := range d.Exercises {
			rng, ok := forecast.ParseReps(e.Reps)
			if !ok {
				continue
			}
			out[lift.KeyFor(slugOf(e), e.Name)] = rng
		}
	}
	return out, nil
}

func weekdayOf(label string) (time.Weekday, bool) {
	canonical, ok := plan.CanonicalWeekday(label)
	if !ok {
		return 0, false
	}
	for d := time.Sunday; d <= time.Saturday; d++ {
		if d.String() == canonical {
			return d, true
		}
	}
	return 0, false
}

// regions folds a day's muscles onto the figure's regions. A region that
// shows several muscles takes the most fatigued and the most detrained of
// them: the figure should not hide a tired muscle behind a fresh neighbour.
func regions(load lift.Load) []RegionLoad {
	byRegion := map[string]*RegionLoad{}
	for _, m := range load.Muscles {
		region, ok := bodymap.RegionOf(m.Muscle)
		if !ok {
			continue
		}
		r := byRegion[region]
		if r == nil {
			r = &RegionLoad{Region: region, Retained: 1}
			byRegion[region] = r
		}
		if m.Fatigue > r.Fatigue {
			r.Fatigue = m.Fatigue
		}
		if m.Strength < r.Retained {
			r.Retained = m.Strength
		}
	}
	out := make([]RegionLoad, 0, len(byRegion))
	for _, r := range byRegion {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Region < out[j].Region })
	return out
}
