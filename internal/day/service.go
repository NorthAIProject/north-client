// Package day composes "My Day": one local date, read across every slice that
// records something about it — food, water, sleep, movement, the body — and
// laid out as a timeline beside a grid of cards.
//
// It owns one table, day_rules, the standing intentions a day is read against
// ("kitchen closes 20:00"). Everything else it reads through the owning
// slice's service, exactly as the dashboard does.
package day

import (
	"context"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics/biometric"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/day/day"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Health metric names this page reads. The device-side names are the contract
// with the iOS sync (north-ios HealthPayload) and with bridge apps.
const (
	metricActiveCalories  = "active_calories"
	metricExerciseMinutes = "exercise_minutes"
	metricStandHours      = "stand_hours"
	metricDaylight        = "time_in_daylight"
	metricHRV             = "hrv_sdnn"
	metricRestingHR       = "resting_heart_rate"
	metricDietaryWater    = "dietary_water"
	metricDietaryEnergy   = "dietary_energy"
	metricDietaryProtein  = "dietary_protein"
	metricDietaryCarbs    = "dietary_carbs"
	metricDietaryFat      = "dietary_fat"
	metricBodyMass        = "body_mass"

	// baselineDays is how far back "usual" reaches for HRV and resting heart
	// rate. Two weeks smooths a bad night without hiding a real trend.
	baselineDays = 14
)

// The slices this page reads, each narrowed to what it needs. Small local
// interfaces rather than the concrete services, so the tests can hand in a
// fake without a database behind every one of them.
type (
	Hydration interface {
		DaysBetween(ctx context.Context, user users.User, rg timerange.Range) ([]hydration.Day, error)
	}
	Sleep interface {
		ListBetween(ctx context.Context, user users.User, rg timerange.Range) ([]sleep.Log, error)
	}
	Food interface {
		Day(ctx context.Context, userID uuid.UUID, date time.Time) ([]meals.FoodLogEntry, error)
	}
	MacroGoals interface {
		Current(ctx context.Context, userID uuid.UUID) (calculator.MacroPlan, error)
	}
	Biometrics interface {
		Current(ctx context.Context, userID uuid.UUID) (biometric.Biometric, error)
	}
	Health interface {
		Between(ctx context.Context, userID uuid.UUID, metric string, since, until time.Time) ([]health.Stored, error)
	}
	Activity interface {
		ListBetween(ctx context.Context, userID uuid.UUID, rg timerange.Range) ([]activity.Session, error)
	}
	Streaks interface {
		Streak(ctx context.Context, user users.User) (int, error)
	}
	Timeline interface {
		Timeline(ctx context.Context, user users.User, rg timerange.Range, limit int) ([]dashboard.Entry, error)
	}
)

// Options keeps the constructor readable. Every field but Rules is optional:
// a nil slice is an empty card, never a failed page.
type Options struct {
	Rules      *Repository
	Hydration  Hydration
	Sleep      Sleep
	Food       Food
	MacroGoals MacroGoals
	Biometrics Biometrics
	Health     Health
	Activity   Activity
	Streaks    Streaks
	Timeline   Timeline

	// Now is the clock. Nil means time.Now; tests pin it.
	Now func() time.Time
}

type Service struct {
	rules      *Repository
	hydration  Hydration
	sleep      Sleep
	food       Food
	macroGoals MacroGoals
	biometrics Biometrics
	health     Health
	activity   Activity
	streaks    Streaks
	timeline   Timeline
	now        func() time.Time
}

func NewService(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		rules:      opts.Rules,
		hydration:  opts.Hydration,
		sleep:      opts.Sleep,
		food:       opts.Food,
		macroGoals: opts.MacroGoals,
		biometrics: opts.Biometrics,
		health:     opts.Health,
		activity:   opts.Activity,
		streaks:    opts.Streaks,
		timeline:   opts.Timeline,
		now:        now,
	}
}

// Snapshot is one local date, everything on it.
type Snapshot struct {
	Date    time.Time // midnight, in the reader's zone
	Now     time.Time // in the reader's zone
	IsToday bool

	Food     day.Food
	Water    day.Water
	Activity day.ActivityRings
	Sleep    *day.Sleep
	Workouts day.Workouts
	Body     day.Body
	Streak   int

	// Vitals. Nil means not measured, which the tile shows as a dash.
	EnergyPercent   *int
	DaylightMinutes *int

	Timeline []dashboard.Entry
	Rules    []day.Rule
	Markers  []day.Marker
}

// ParseDate reads a ?date= value in the reader's zone. Empty or malformed is
// today: a hand-edited URL must not take the page down.
func ParseDate(q string, loc *time.Location, now time.Time) time.Time {
	if t, err := time.ParseInLocation("2006-01-02", q, loc); err == nil {
		return t
	}
	return timerange.StartOfDay(now.In(loc))
}

// Load gathers one date.
//
// Slices are queried concurrently, with the dashboard's error policy: a
// missing row is an empty card, a real error fails the page.
func (s *Service) Load(ctx context.Context, user users.User, date time.Time) (Snapshot, error) {
	loc := user.Location()
	now := s.now().In(loc)
	date = timerange.StartOfDay(date.In(loc))
	rg := timerange.Between(date, date.AddDate(0, 0, 1))

	snap := Snapshot{
		Date:    date,
		Now:     now,
		IsToday: rg.Contains(now),
	}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error { return s.loadFood(gctx, user, date, &snap) })
	g.Go(func() error { return s.loadWater(gctx, user, rg, &snap) })
	g.Go(func() error { return s.loadSleep(gctx, user, rg, &snap) })
	g.Go(func() error { return s.loadMovement(gctx, user, rg, &snap) })
	g.Go(func() error { return s.loadBody(gctx, user, &snap) })
	g.Go(func() error { return s.loadEnergy(gctx, user, rg, &snap) })

	if s.streaks != nil {
		g.Go(func() (err error) { snap.Streak, err = s.streaks.Streak(gctx, user); return })
	}
	if s.health != nil {
		g.Go(func() error {
			mins, ok, err := s.sum(gctx, user.ID, metricDaylight, rg.Since, rg.Until)
			if ok {
				v := int(mins)
				snap.DaylightMinutes = &v
			}
			return err
		})
	}
	if s.timeline != nil {
		g.Go(func() (err error) {
			// No cap: the day view is the whole day, and one day is small.
			snap.Timeline, err = s.timeline.Timeline(gctx, user, rg, 0)
			return
		})
	}
	g.Go(func() error {
		rules, err := s.Rules(gctx, user.ID)
		if err != nil {
			return err
		}
		snap.Rules = rules
		snap.Markers = markersFor(rules, date, now)
		return nil
	})

	if err := g.Wait(); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// Rules lists a person's day rules.
func (s *Service) Rules(ctx context.Context, userID uuid.UUID) ([]day.Rule, error) {
	if s.rules == nil {
		return nil, nil
	}
	return s.rules.ListRules(ctx, userID)
}

// SetRule creates or replaces one rule.
func (s *Service) SetRule(ctx context.Context, userID uuid.UUID, rule day.Rule) (day.Rule, error) {
	if err := rule.Validate(); err != nil {
		return day.Rule{}, err
	}
	return s.rules.UpsertRule(ctx, userID, rule)
}

// DeleteRule removes one rule. Removing a rule that is not there is not an
// error: the end state is the one asked for.
func (s *Service) DeleteRule(ctx context.Context, userID uuid.UUID, kind day.RuleKind) error {
	if !kind.Valid() {
		return apperr.Wrap(apperr.ErrValidation, "unknown rule kind %q", kind)
	}
	return s.rules.DeleteRule(ctx, userID, kind)
}

func markersFor(rules []day.Rule, date, now time.Time) []day.Marker {
	out := make([]day.Marker, 0, len(rules))
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		at := r.On(date)
		out = append(out, day.Marker{Kind: r.Kind, At: at, Passed: !at.After(now)})
	}
	return out
}

func (s *Service) loadFood(ctx context.Context, user users.User, date time.Time, snap *Snapshot) error {
	if s.food != nil {
		entries, err := s.food.Day(ctx, user.ID, date)
		if err != nil {
			return err
		}
		for _, e := range entries {
			snap.Food.Calories += e.Macros.Calories
			snap.Food.ProteinG += e.Macros.ProteinG
			snap.Food.CarbG += e.Macros.CarbG
			snap.Food.FatG += e.Macros.FatG
		}
	}

	// Food logged in another app and synced through Apple Health. The phone
	// never uploads what this app wrote itself, so the two add without
	// counting anything twice.
	if s.health != nil {
		next := date.AddDate(0, 0, 1)
		for metric, into := range map[string]*float64{
			metricDietaryEnergy:  &snap.Food.Calories,
			metricDietaryProtein: &snap.Food.ProteinG,
			metricDietaryCarbs:   &snap.Food.CarbG,
			metricDietaryFat:     &snap.Food.FatG,
		} {
			v, _, err := s.sum(ctx, user.ID, metric, date, next)
			if err != nil {
				return err
			}
			*into += v
		}
	}

	if s.macroGoals == nil {
		return nil
	}
	plan, err := s.macroGoals.Current(ctx, user.ID)
	switch {
	case err == nil:
		snap.Food.HasGoal = true
		snap.Food.CalorieGoal = plan.CalorieGoal
		snap.Food.ProteinGoalG = plan.ProteinG
		snap.Food.CarbGoalG = plan.CarbG
		snap.Food.FatGoalG = plan.FatG
		return nil
	case apperr.Is(err, apperr.ErrNotFound):
		return nil
	default:
		return err
	}
}

func (s *Service) loadWater(ctx context.Context, user users.User, rg timerange.Range, snap *Snapshot) error {
	snap.Water.TargetML = hydration.DefaultDailyTargetML
	if s.hydration != nil {
		days, err := s.hydration.DaysBetween(ctx, user, rg)
		if err != nil {
			return err
		}
		for _, d := range days {
			snap.Water.TotalML += d.TotalML
			if d.TargetML > 0 {
				snap.Water.TargetML = d.TargetML
			}
		}
	}
	if s.health != nil {
		ml, _, err := s.sum(ctx, user.ID, metricDietaryWater, rg.Since, rg.Until)
		if err != nil {
			return err
		}
		snap.Water.TotalML += int(ml)
	}
	return nil
}

// loadSleep prefers the device's staged night and falls back to the manual
// log. A manual rating still rides along: a watch knows how long, only the
// person knows how well.
func (s *Service) loadSleep(ctx context.Context, user users.User, rg timerange.Range, snap *Snapshot) error {
	var manual *sleep.Log
	if s.sleep != nil {
		logs, err := s.sleep.ListBetween(ctx, user, rg)
		if err != nil {
			return err
		}
		if len(logs) > 0 {
			manual = &logs[0]
		}
	}

	var blocks []day.SleepBlock
	source := ""
	if s.health != nil {
		since, until := day.NightWindow(rg.Since)
		for stage, metric := range day.StageMetrics() {
			rows, err := s.health.Between(ctx, user.ID, metric, since, until)
			if err != nil {
				return err
			}
			for _, r := range rows {
				end := r.StartedAt.Add(time.Duration(r.Value * float64(time.Minute)))
				if r.EndedAt != nil {
					end = *r.EndedAt
				}
				blocks = append(blocks, day.SleepBlock{Stage: stage, Start: r.StartedAt.In(rg.Location()), End: end.In(rg.Location())})
				source = r.Source
			}
		}
	}

	if night, ok := day.SleepFromBlocks(blocks, source); ok {
		if manual != nil {
			night.Quality = manual.Quality
		}
		snap.Sleep = &night
		return nil
	}
	if manual != nil {
		night := day.Sleep{TotalMinutes: manual.DurationMinutes, Quality: manual.Quality, Source: "manual"}
		if manual.Bedtime != "" && manual.WakeTime != "" {
			wake := day.Rule{At: manual.WakeTime}.On(rg.Since)
			bed := day.Rule{At: manual.Bedtime}.On(rg.Since)
			if bed.After(wake) {
				bed = bed.AddDate(0, 0, -1)
			}
			night.Start, night.End = &bed, &wake
		}
		snap.Sleep = &night
	}
	return nil
}

// loadMovement fills the rings and the workout card.
//
// The rings prefer the device's own totals — a watch sees movement no workout
// captures. Without a device, finished sessions stand in, so someone who only
// logs in-app still sees their rings move.
func (s *Service) loadMovement(ctx context.Context, user users.User, rg timerange.Range, snap *Snapshot) error {
	snap.Activity = day.ActivityRings{
		Move:     day.Ring{Goal: day.DefaultMoveGoalKcal},
		Exercise: day.Ring{Goal: day.DefaultExerciseGoalMins},
		Stand:    day.Ring{Goal: day.DefaultStandGoalHours},
	}

	if s.activity != nil {
		sessions, err := s.activity.ListBetween(ctx, user.ID, rg)
		if err != nil {
			return err
		}
		for _, sess := range sessions {
			if sess.EndedAt == nil {
				continue
			}
			snap.Workouts.Count++
			mins := int(sess.EndedAt.Sub(sess.StartedAt).Minutes()) - sess.TotalPausedSeconds/60
			if mins > 0 {
				snap.Workouts.Minutes += mins
			}
			if sess.CaloriesBurned != nil {
				snap.Workouts.Calories += *sess.CaloriesBurned
			}
			name := sess.ActivityCode
			if met, ok := activity.LookupMET(sess.ActivityCode); ok {
				name = met.Name
			}
			snap.Workouts.Labels = append(snap.Workouts.Labels, name)
		}
	}
	snap.Activity.Move.Value = snap.Workouts.Calories
	snap.Activity.Exercise.Value = float64(snap.Workouts.Minutes)

	if s.health == nil {
		return nil
	}
	if kcal, ok, err := s.sum(ctx, user.ID, metricActiveCalories, rg.Since, rg.Until); err != nil {
		return err
	} else if ok {
		snap.Activity.Move.Value = kcal
	}
	if mins, ok, err := s.sum(ctx, user.ID, metricExerciseMinutes, rg.Since, rg.Until); err != nil {
		return err
	} else if ok {
		snap.Activity.Exercise.Value = mins
	}
	hours, _, err := s.sum(ctx, user.ID, metricStandHours, rg.Since, rg.Until)
	snap.Activity.Stand.Value = hours
	return err
}

// loadBody takes the newer of the last manual measurement and the last weight
// a device reported. Height only ever comes from the manual record.
func (s *Service) loadBody(ctx context.Context, user users.User, snap *Snapshot) error {
	var (
		weight   *float64
		weighed  time.Time
		heightCm *float64
	)
	if s.biometrics != nil {
		b, err := s.biometrics.Current(ctx, user.ID)
		switch {
		case err == nil:
			w, h := b.WeightKg, b.HeightCm
			weight, weighed = &w, b.CreatedAt
			if h > 0 {
				heightCm = &h
			}
		case !apperr.Is(err, apperr.ErrNotFound):
			return err
		}
	}
	if s.health != nil {
		now := s.now()
		rows, err := s.health.Between(ctx, user.ID, metricBodyMass, now.AddDate(0, 0, -90), now.Add(time.Minute))
		if err != nil {
			return err
		}
		// Newest first.
		if len(rows) > 0 && rows[0].StartedAt.After(weighed) {
			w := rows[0].Value
			weight = &w
		}
	}

	snap.Body.WeightKg = weight
	snap.Body.HeightCm = heightCm
	if weight != nil && heightCm != nil {
		m := *heightCm / 100
		bmi := *weight / (m * m)
		snap.Body.BMI = &bmi
	}
	return nil
}

// loadEnergy estimates today's charge. Only for today: "how charged were you
// at some point last Tuesday" has no honest answer.
func (s *Service) loadEnergy(ctx context.Context, user users.User, rg timerange.Range, snap *Snapshot) error {
	if !snap.IsToday || s.health == nil {
		return nil
	}

	in := day.EnergyInputs{Now: snap.Now}

	// Sleep and activity are computed by sibling loaders concurrently, so this
	// reads what it needs directly rather than racing them for the snapshot.
	var sleepSnap Snapshot
	if err := s.loadSleep(ctx, user, rg, &sleepSnap); err != nil {
		return err
	}
	if sleepSnap.Sleep != nil {
		m := sleepSnap.Sleep.TotalMinutes
		in.SleepMinutes = &m
		in.WokeAt = sleepSnap.Sleep.End
	}
	kcal, _, err := s.sum(ctx, user.ID, metricActiveCalories, rg.Since, rg.Until)
	if err != nil {
		return err
	}
	in.ActiveKcal = kcal

	baselineSince := rg.Since.AddDate(0, 0, -baselineDays)
	if in.HRV, in.HRVBaseline, err = s.todayAndBaseline(ctx, user.ID, metricHRV, baselineSince, rg); err != nil {
		return err
	}
	if in.RestingHR, in.RestingHRBaseline, err = s.todayAndBaseline(ctx, user.ID, metricRestingHR, baselineSince, rg); err != nil {
		return err
	}

	if pct, ok := day.Energy(in); ok {
		snap.EnergyPercent = &pct
	}
	return nil
}

func (s *Service) todayAndBaseline(ctx context.Context, userID uuid.UUID, metric string, since time.Time, rg timerange.Range) (*float64, *float64, error) {
	rows, err := s.health.Between(ctx, userID, metric, since, rg.Until)
	if err != nil {
		return nil, nil, err
	}
	var today, base []float64
	for _, r := range rows {
		if rg.Contains(r.StartedAt) {
			today = append(today, r.Value)
		} else {
			base = append(base, r.Value)
		}
	}
	return mean(today), mean(base), nil
}

// sum totals one metric over a window, reporting whether anything was there.
func (s *Service) sum(ctx context.Context, userID uuid.UUID, metric string, since, until time.Time) (float64, bool, error) {
	if s.health == nil {
		return 0, false, nil
	}
	rows, err := s.health.Between(ctx, userID, metric, since, until)
	if err != nil {
		return 0, false, err
	}
	total := 0.0
	for _, r := range rows {
		total += r.Value
	}
	return total, len(rows) > 0, nil
}

func mean(vs []float64) *float64 {
	if len(vs) == 0 {
		return nil
	}
	total := 0.0
	for _, v := range vs {
		total += v
	}
	m := total / float64(len(vs))
	return &m
}
