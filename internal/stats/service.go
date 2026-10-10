// Package stats reads everything a person logs — sleep, cardio, food,
// caffeine, screens, check-ins, training — over a window and says what it
// adds up to, and which of it moves together. It owns no tables: each figure
// comes from the slice that stores it, and the arithmetic lives in stat.
package stats

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/caffeine"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/day/day"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/screentime"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	"github.com/NorthAIProject/north-client/internal/users"
)

// SleepTargetMinutes is the night the debt is counted against: eight hours,
// the middle of the seven to nine most adults need.
const SleepTargetMinutes = 8 * 60

// Default cutoffs when the person has not set the day rules.
const (
	defaultCaffeineCutoffHour = 14
	defaultKitchenClosesHour  = 21
)

// Sources are the slices stats reads. Each is optional: a missing one leaves
// its figures empty rather than failing the page.
type Sources struct {
	Sleep interface {
		ListBetween(ctx context.Context, user users.User, rg timerange.Range) ([]sleep.Log, error)
	}
	Health interface {
		Between(ctx context.Context, userID uuid.UUID, metric string, since, until time.Time) ([]health.Stored, error)
	}
	Activity interface {
		ListBetween(ctx context.Context, userID uuid.UUID, rg timerange.Range) ([]activity.Session, error)
	}
	Food interface {
		Range(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]meals.FoodLogEntry, error)
	}
	MacroGoals meals.MacroGoalLookup
	Biometrics interface {
		Current(ctx context.Context, userID uuid.UUID) (biometrics.Biometric, error)
	}
	Caffeine interface {
		Between(ctx context.Context, user users.User, rg timerange.Range) ([]caffeine.Entry, error)
	}
	ScreenTime interface {
		Between(ctx context.Context, user users.User, rg timerange.Range) ([]screentime.Day, error)
	}
	CheckIns interface {
		ListBetween(ctx context.Context, userID uuid.UUID, rg timerange.Range) ([]checkins.CheckIn, error)
	}
	Lifts interface {
		Between(ctx context.Context, user users.User, rg timerange.Range) ([]lifts.Set, error)
	}
	Rules interface {
		Rules(ctx context.Context, userID uuid.UUID) ([]day.Rule, error)
	}
}

type Service struct {
	src Sources
}

func NewService(src Sources) *Service { return &Service{src: src} }

// Nights builds one night per date in the window from the device's staged
// blocks, falling back to a manual log. Four queries for the whole window,
// however long it is.
func (s *Service) Nights(ctx context.Context, user users.User, rg timerange.Range) ([]stat.Night, error) {
	loc := rg.Location()
	manual := map[string]sleep.Log{}
	if s.src.Sleep != nil {
		logs, err := s.src.Sleep.ListBetween(ctx, user, rg)
		if err != nil {
			return nil, err
		}
		for _, l := range logs {
			manual[l.LocalDate.Format(time.DateOnly)] = l
		}
	}

	type block struct {
		stage day.SleepStage
		start time.Time
		end   time.Time
	}
	var blocks []block
	source := ""
	if s.src.Health != nil {
		since, _ := day.NightWindow(rg.Since)
		_, until := day.NightWindow(rg.Until.Add(-time.Minute))
		for stage, metric := range day.StageMetrics() {
			rows, err := s.src.Health.Between(ctx, user.ID, metric, since, until)
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				end := r.StartedAt.Add(time.Duration(r.Value * float64(time.Minute)))
				if r.EndedAt != nil {
					end = *r.EndedAt
				}
				blocks = append(blocks, block{stage: stage, start: r.StartedAt.In(loc), end: end.In(loc)})
				source = r.Source
			}
		}
	}

	var out []stat.Night
	for d := timerange.StartOfDay(rg.Since.In(loc)); d.Before(rg.Until); d = d.AddDate(0, 0, 1) {
		since, until := day.NightWindow(d)
		var nightBlocks []day.SleepBlock
		for _, b := range blocks {
			if !b.start.Before(since) && b.start.Before(until) {
				nightBlocks = append(nightBlocks, day.SleepBlock{Stage: b.stage, Start: b.start, End: b.end})
			}
		}
		log, hasLog := manual[d.Format(time.DateOnly)]
		if night, ok := day.SleepFromBlocks(nightBlocks, source); ok {
			n := stat.Night{Date: d, Minutes: night.TotalMinutes, Start: night.Start, End: night.End, Stages: map[string]int{}}
			for stage, m := range night.StageMinutes {
				n.Stages[string(stage)] = m
			}
			if hasLog {
				n.Quality = log.Quality
			}
			out = append(out, n)
			continue
		}
		if hasLog {
			n := stat.Night{Date: d, Minutes: log.DurationMinutes, Quality: log.Quality}
			if log.Bedtime != "" && log.WakeTime != "" {
				wake := day.Rule{At: log.WakeTime}.On(d)
				bed := day.Rule{At: log.Bedtime}.On(d)
				if bed.After(wake) {
					bed = bed.AddDate(0, 0, -1)
				}
				n.Start, n.End = &bed, &wake
			}
			out = append(out, n)
		}
	}
	return out, nil
}

// Sleep is sleep over a window.
func (s *Service) Sleep(ctx context.Context, user users.User, rg timerange.Range) (stat.SleepStats, error) {
	nights, err := s.Nights(ctx, user, rg)
	if err != nil {
		return stat.SleepStats{}, err
	}
	return stat.Sleep(nights, SleepTargetMinutes), nil
}

// strength is the activity code a gym session is timed as; its sets are
// lifting, not cardio.
const strength = "strength_training"

// CardioStats is cardio plus the heart figures that go with it.
type CardioStats struct {
	stat.CardioStats
	RestingHR []stat.DayValue
	HRV       []stat.DayValue
	VO2Max    []stat.DayValue
}

func (s *Service) Cardio(ctx context.Context, user users.User, rg timerange.Range) (CardioStats, error) {
	var out CardioStats
	var sessions []activity.Session
	g, gctx := errgroup.WithContext(ctx)
	if s.src.Activity != nil {
		g.Go(func() (err error) {
			sessions, err = s.src.Activity.ListBetween(gctx, user.ID, rg)
			return
		})
	}
	if s.src.Health != nil {
		for metric, into := range map[string]*[]stat.DayValue{
			"resting_heart_rate": &out.RestingHR, "hrv_sdnn": &out.HRV, "vo2max": &out.VO2Max,
		} {
			g.Go(func() error {
				rows, err := s.src.Health.Between(gctx, user.ID, metric, rg.Since, rg.Until)
				if err != nil {
					return err
				}
				*into = dailyMeans(rows, rg.Location())
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		return CardioStats{}, err
	}

	var cardio []stat.Session
	for _, sess := range sessions {
		if sess.EndedAt == nil || sess.ActivityCode == strength {
			continue
		}
		c := stat.Session{
			Code: sess.ActivityCode, Name: activityName(sess.ActivityCode),
			At: sess.StartedAt.In(rg.Location()), Seconds: int(sess.Elapsed(*sess.EndedAt).Seconds()),
		}
		c.DistanceM = util.Val(sess.DistanceM)
		c.Kcal = util.Val(sess.CaloriesBurned)
		cardio = append(cardio, c)
	}
	out.CardioStats = stat.Cardio(cardio, rg.Since, rg.Until)
	return out, nil
}

// activityName is how a session is grouped: the catalog splits running and
// cycling by pace, which is noise on a breakdown by activity.
func activityName(code string) string {
	for _, family := range []struct{ prefix, name string }{
		{"running_", "Running"}, {"cycling_", "Cycling"}, {"walking_", "Walking"}, {"swimming_", "Swimming"},
	} {
		if strings.HasPrefix(code, family.prefix) {
			return family.name
		}
	}
	if met, ok := activity.LookupMET(code); ok {
		return met.Name
	}
	return code
}

// patternMetrics are the Apple Health figures patterns compare mood and
// recovery against. Heart figures are a day's average; the rest are totals.
var patternMetrics = []struct {
	name string
	mean bool
	set  func(f *stat.DayFacts, v float64)
}{
	{name: "steps", set: func(f *stat.DayFacts, v float64) { f.Steps = &v }},
	{name: "time_in_daylight", set: func(f *stat.DayFacts, v float64) { f.DaylightMin = &v }},
	{name: "stand_hours", set: func(f *stat.DayFacts, v float64) { f.StandHours = &v }},
	{name: "hrv_sdnn", mean: true, set: func(f *stat.DayFacts, v float64) { f.HRV = &v }},
	{name: "resting_heart_rate", mean: true, set: func(f *stat.DayFacts, v float64) { f.RestingHR = &v }},
	{name: "mindful_minutes", set: func(f *stat.DayFacts, v float64) { f.MindfulMin = &v }},
}

// outdoor reports whether a session was done outside. The provider's own
// flag wins when it sent one; otherwise the code decides, and a run with no
// flag counts as outdoor, which is where most runs without a treadmill
// flag happened.
func outdoor(sess activity.Session) bool {
	if sess.Indoor != nil {
		return !*sess.Indoor && moves(sess.ActivityCode)
	}
	return moves(sess.ActivityCode)
}

// moves reports an activity that covers ground and so can be done outside.
func moves(code string) bool {
	if strings.HasPrefix(code, "cycling_stationary") {
		return false
	}
	for _, prefix := range []string{"running", "walking", "cycling", "hiking"} {
		if strings.HasPrefix(code, prefix) {
			return true
		}
	}
	return false
}

// dailySums totals readings per local day, oldest first.
func dailySums(rows []health.Stored, loc *time.Location) []stat.DayValue {
	sums := map[time.Time]float64{}
	for _, r := range rows {
		sums[timerange.StartOfDay(r.StartedAt.In(loc))] += r.Value
	}
	out := make([]stat.DayValue, 0, len(sums))
	for d, v := range sums {
		out = append(out, stat.DayValue{Day: d, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day.Before(out[j].Day) })
	return out
}

// dailyMeans averages readings per local day, oldest first.
func dailyMeans(rows []health.Stored, loc *time.Location) []stat.DayValue {
	sums := map[time.Time][2]float64{}
	for _, r := range rows {
		d := timerange.StartOfDay(r.StartedAt.In(loc))
		v := sums[d]
		sums[d] = [2]float64{v[0] + r.Value, v[1] + 1}
	}
	out := make([]stat.DayValue, 0, len(sums))
	for d, v := range sums {
		out = append(out, stat.DayValue{Day: d, Value: float64(int(v[0]/v[1]*10+0.5)) / 10})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day.Before(out[j].Day) })
	return out
}

// Eating is food over a window against the calculator's targets.
func (s *Service) Eating(ctx context.Context, user users.User, rg timerange.Range) (stat.EatingStats, error) {
	entries, err := s.food(ctx, user, rg)
	if err != nil {
		return stat.EatingStats{}, err
	}
	var goalKcal, goalProtein, weight float64
	if s.src.MacroGoals != nil {
		plan, planErr := s.src.MacroGoals.Current(ctx, user.ID)
		if planErr != nil && !apperr.Is(planErr, apperr.ErrNotFound) {
			return stat.EatingStats{}, planErr
		}
		goalKcal, goalProtein, weight = plan.CalorieGoal, plan.ProteinG, plan.WeightKg
	}
	if s.src.Biometrics != nil {
		if b, bioErr := s.src.Biometrics.Current(ctx, user.ID); bioErr == nil && b.WeightKg > 0 {
			weight = b.WeightKg
		} else if bioErr != nil && !apperr.Is(bioErr, apperr.ErrNotFound) {
			return stat.EatingStats{}, bioErr
		}
	}
	cut, err := s.cutoffs(ctx, user)
	if err != nil {
		return stat.EatingStats{}, err
	}
	return stat.Eating(entries, goalKcal, goalProtein, weight, cut.kitchenHour), nil
}

func (s *Service) food(ctx context.Context, user users.User, rg timerange.Range) ([]stat.FoodEntry, error) {
	if s.src.Food == nil {
		return nil, nil
	}
	rows, err := s.src.Food.Range(ctx, user.ID, rg.Since, rg.Until.AddDate(0, 0, -1))
	if err != nil {
		return nil, err
	}
	out := make([]stat.FoodEntry, len(rows))
	for i, r := range rows {
		out[i] = stat.FoodEntry{
			At: r.LoggedAt.In(rg.Location()), Date: r.LogDate, Label: r.Label,
			Kcal: r.Macros.Calories, Protein: r.Macros.ProteinG, Carb: r.Macros.CarbG, Fat: r.Macros.FatG,
		}
	}
	return out, nil
}

type cutoffs struct {
	caffeineMin int // minutes after midnight
	kitchenMin  int
	kitchenHour int
}

// cutoffs reads the caffeine cutoff and kitchen-closes rules, or the
// defaults when they are not set.
func (s *Service) cutoffs(ctx context.Context, user users.User) (cutoffs, error) {
	c := cutoffs{caffeineMin: defaultCaffeineCutoffHour * 60, kitchenMin: defaultKitchenClosesHour * 60}
	if s.src.Rules != nil {
		rules, err := s.src.Rules.Rules(ctx, user.ID)
		if err != nil {
			return c, err
		}
		for _, r := range rules {
			m, err := day.ParseClock(r.At)
			if err != nil || !r.Enabled {
				continue
			}
			switch r.Kind {
			case day.RuleCaffeineCutoff:
				c.caffeineMin = m
			case day.RuleKitchenCloses:
				c.kitchenMin = m
			}
		}
	}
	c.kitchenHour = c.kitchenMin / 60
	return c, nil
}

// PatternsWindow is the least history patterns read, whatever window the
// page shows: a week holds too few days for any split to mean anything.
const PatternsWindow = 90

// Patterns finds what moves together over at least the last PatternsWindow
// days.
func (s *Service) Patterns(ctx context.Context, user users.User, rg timerange.Range) ([]stat.Finding, int, error) {
	loc := rg.Location()
	since := rg.Since
	if floor := timerange.StartOfDay(rg.Until.In(loc)).AddDate(0, 0, -PatternsWindow); floor.Before(since) {
		since = floor
	}
	window := timerange.Between(since, rg.Until)

	facts := map[string]*stat.DayFacts{}
	for d := timerange.StartOfDay(since.In(loc)); d.Before(rg.Until); d = d.AddDate(0, 0, 1) {
		facts[d.Format(time.DateOnly)] = &stat.DayFacts{Date: d}
	}
	at := func(t time.Time) *stat.DayFacts { return facts[timerange.StartOfDay(t.In(loc)).Format(time.DateOnly)] }

	cut, err := s.cutoffs(ctx, user)
	if err != nil {
		return nil, 0, err
	}

	var (
		nights  []stat.Night
		food    []stat.FoodEntry
		drinks  []caffeine.Entry
		screens []screentime.Day
		checks  []checkins.CheckIn
		sets    []lifts.Set
		acts    []activity.Session
		healthy = make([][]health.Stored, len(patternMetrics))
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { nights, err = s.Nights(gctx, user, window); return })
	g.Go(func() (err error) { food, err = s.food(gctx, user, window); return })
	if s.src.Caffeine != nil {
		g.Go(func() (err error) { drinks, err = s.src.Caffeine.Between(gctx, user, window); return })
	}
	if s.src.ScreenTime != nil {
		g.Go(func() (err error) { screens, err = s.src.ScreenTime.Between(gctx, user, window); return })
	}
	if s.src.CheckIns != nil {
		g.Go(func() (err error) { checks, err = s.src.CheckIns.ListBetween(gctx, user.ID, window); return })
	}
	if s.src.Lifts != nil {
		g.Go(func() (err error) { sets, err = s.src.Lifts.Between(gctx, user, window); return })
	}
	if s.src.Activity != nil {
		g.Go(func() (err error) { acts, err = s.src.Activity.ListBetween(gctx, user.ID, window); return })
	}
	if s.src.Health != nil {
		for i, m := range patternMetrics {
			g.Go(func() (err error) {
				healthy[i], err = s.src.Health.Between(gctx, user.ID, m.name, window.Since, window.Until)
				return
			})
		}
	}
	if err := g.Wait(); err != nil {
		return nil, 0, err
	}

	for _, n := range nights {
		if f := at(n.Date); f != nil {
			f.SleepMinutes = util.Ptr(n.Minutes)
		}
	}
	for _, e := range food {
		if f := at(e.Date); f != nil {
			f.AteLogged = true
			local := e.At
			if local.Hour()*60+local.Minute() >= cut.kitchenMin {
				f.LateEating = true
			}
		}
	}
	for _, d := range drinks {
		if f := at(d.LoggedAt); f != nil {
			f.HadCaffeine = true
			local := d.LoggedAt.In(loc)
			if local.Hour()*60+local.Minute() >= cut.caffeineMin {
				f.LateCaffeine = true
			}
		}
	}
	for _, sc := range screens {
		if f := at(sc.LocalDate); f != nil {
			f.ScreenMin = util.Ptr(sc.Minutes)
		}
	}
	for _, c := range checks {
		if f := at(c.LocalDate); f != nil {
			mood, energy := c.Mood, c.Energy
			f.Mood, f.Energy = &mood, &energy
		}
	}
	for _, set := range sets {
		if f := at(set.PerformedAt); f != nil {
			f.Trained = true
		}
	}
	for _, a := range acts {
		if f := at(a.StartedAt); f != nil && a.EndedAt != nil {
			f.Trained = true
			if outdoor(a) {
				f.OutdoorWorkout = true
			}
		}
	}
	for i, m := range patternMetrics {
		daily := dailySums(healthy[i], loc)
		if m.mean {
			daily = dailyMeans(healthy[i], loc)
		}
		for _, dv := range daily {
			if f := at(dv.Day); f != nil {
				m.set(f, dv.Value)
			}
		}
	}

	days := make([]stat.DayFacts, 0, len(facts))
	for _, f := range facts {
		days = append(days, *f)
	}
	return stat.Patterns(days), len(days), nil
}
