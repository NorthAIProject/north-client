package insights

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/insights/score"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	"github.com/NorthAIProject/north-client/internal/users"
)

// recoveryFreshDays is how old a reading may be and still describe today. A
// resting heart rate from yesterday still does; one from last week does not,
// and scoring it would pass an old week off as this morning.
const recoveryFreshDays = 1

// RecoverySignal is one input to the recovery score as the person sees it.
type RecoverySignal struct {
	Key    string // score.RecoveryHRV, score.RecoveryRestingHR or score.RecoverySleep
	Metric metric // the catalog metric it is read and worded as
	Usual  Usual
}

// RecoveryData is today's recovery: the score and the signals behind it.
// Signals hold only the inputs that had a fresh reading and a baseline.
type RecoveryData struct {
	Score   score.Score
	Signals []RecoverySignal
}

// Recovery reads how today's body compares with its own usual: the latest
// HRV, resting heart rate and night's sleep, each against the four weeks
// before it. A reading older than yesterday is left out.
func (s *Service) Recovery(ctx context.Context, user users.User, now time.Time) (RecoveryData, error) {
	today := timerange.StartOfDay(now.In(user.Location()))
	window := usualWindow(today)

	hrv, rhr, nightly := mustMetric("hrv"), mustMetric("resting-heart-rate"), mustMetric("sleep")
	sources := []struct {
		key  string
		m    metric
		load func(ctx context.Context) ([]point, error)
	}{
		{score.RecoveryHRV, hrv, func(ctx context.Context) ([]point, error) { return hrv.load(ctx, s, user, window) }},
		{score.RecoveryRestingHR, rhr, func(ctx context.Context) ([]point, error) { return rhr.load(ctx, s, user, window) }},
		// Sleep is read the way the sleep page reads it: device nights
		// first, a manual log where there is no device.
		{score.RecoverySleep, nightly, func(ctx context.Context) ([]point, error) { return s.nights(ctx, user, window) }},
	}

	loaded := make([][]point, len(sources))
	g, gctx := errgroup.WithContext(ctx)
	for i, src := range sources {
		g.Go(func() (err error) {
			loaded[i], err = src.load(gctx)
			return
		})
	}
	if err := g.Wait(); err != nil {
		return RecoveryData{}, err
	}

	var out RecoveryData
	inputs := make([]score.RecoverySignal, 0, len(sources))
	freshFrom := today.AddDate(0, 0, -recoveryFreshDays)
	for i, src := range sources {
		u, ok := usualOf(loaded[i])
		fresh := ok && !u.Latest.At.Before(freshFrom)
		inputs = append(inputs, score.RecoverySignal{Key: src.key, Z: u.Z, Known: fresh})
		if fresh {
			out.Signals = append(out.Signals, RecoverySignal{Key: src.key, Metric: src.m, Usual: u})
		}
	}
	out.Score = score.Recovery(inputs)
	return out, nil
}

// nights loads one point per night in hours, dated by the morning it ended.
func (s *Service) nights(ctx context.Context, user users.User, rg timerange.Range) ([]point, error) {
	reader := s.nightly
	if reader == nil && s.stats != nil {
		reader = s.stats
	}
	if reader == nil {
		return nil, nil
	}
	st, err := reader.Sleep(ctx, user, rg)
	if err != nil {
		return nil, err
	}
	out := make([]point, 0, len(st.Nights))
	for _, n := range st.Nights {
		out = append(out, point{At: timerange.StartOfDay(n.Date.In(rg.Location())), Value: float64(n.Minutes) / 60})
	}
	return out, nil
}

// mustMetric is a catalog lookup for a key this package itself defines.
func mustMetric(key string) metric {
	m, ok := lookupMetric(key)
	if !ok {
		panic("insights: no catalog metric " + key)
	}
	return m
}

// recoveryWords are the words beside the recovery number. They name the
// comparison, never a diagnosis: the score knows the person's own usual and
// nothing about medicine.
var recoveryWords = map[score.Verdict]string{
	score.VerdictStrong: "Above your usual",
	score.VerdictOK:     "About your usual",
	score.VerdictUneven: "A little under your usual",
	score.VerdictLow:    "Under your usual",
}

// Low reports a morning to go easier on: a recovery that scores uneven or
// low. This is the one rule — the lighter-day offer and the morning briefing
// read it here, so the screen, the offer and the coach never disagree.
func (r RecoveryData) Low() bool {
	if !r.Score.HasData {
		return false
	}
	v := r.Score.Verdict()
	return v == score.VerdictUneven || v == score.VerdictLow
}

// Sentence is the recovery as one line: the number, its words, and each
// signal's own sentence. The coach and the screen read the same one.
func (r RecoveryData) Sentence() (string, bool) {
	if !r.Score.HasData {
		return "", false
	}
	parts := make([]string, 0, len(r.Signals))
	for _, sig := range r.Signals {
		parts = append(parts, sig.Metric.Label+" — "+usualView(sig.Metric, sig.Usual).Text)
	}
	return fmt.Sprintf("Recovery today: %d/100, %s. %s", r.Score.Points,
		strings.ToLower(recoveryWords[r.Score.Verdict()]), strings.Join(parts, " ")), true
}

// Why is what pulled the recovery off its usual: the sentences of the
// signals outside their usual range, or "" when none are.
func (r RecoveryData) Why() string {
	var parts []string
	for _, sig := range r.Signals {
		v := usualView(sig.Metric, sig.Usual)
		if v.State == stat.Usual {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s is %s your usual: %s on %s, usually %s–%s.",
			sig.Metric.Label, v.State, formatMetric(sig.Metric, v.Latest), v.Day.Format("Mon 2 Jan"), v.Low, v.High))
	}
	return strings.Join(parts, " ")
}

// Signal finds one signal by its score key.
func (r RecoveryData) Signal(key string) (RecoverySignal, bool) {
	for _, sig := range r.Signals {
		if sig.Key == key {
			return sig, true
		}
	}
	return RecoverySignal{}, false
}

// SleepReader is the slice of the stats service recovery reads nights from.
type SleepReader interface {
	Sleep(ctx context.Context, user users.User, rg timerange.Range) (stat.SleepStats, error)
}

// RecoverySource reads today's recovery and nothing else. The lighter-day
// offer and the worker's briefing need it without the rest of insights'
// dependencies, so it is built from the two readers recovery actually uses.
type RecoverySource struct {
	svc *Service
}

func NewRecoverySource(h HealthReadings, nights SleepReader) *RecoverySource {
	return &RecoverySource{svc: &Service{health: h, nightly: nights}}
}

func (r *RecoverySource) Recovery(ctx context.Context, user users.User, now time.Time) (RecoveryData, error) {
	return r.svc.Recovery(ctx, user, now)
}

// RecoveryContextSource tells the coach today's recovery, in the same words
// the Progress screen shows, beside the other device readings.
type RecoveryContextSource struct {
	svc *Service
	now func() time.Time
}

// NewRecoveryContextSource builds the source. A nil now means the real clock.
func NewRecoveryContextSource(svc *Service, now func() time.Time) *RecoveryContextSource {
	if now == nil {
		now = time.Now
	}
	return &RecoveryContextSource{svc: svc, now: now}
}

func (s *RecoveryContextSource) Name() string { return "recovery" }

func (s *RecoveryContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	r, err := s.svc.Recovery(ctx, req.User, s.now())
	if err != nil {
		return err
	}
	if line, ok := r.Sentence(); ok {
		into.DailySignals = append(into.DailySignals, line)
	}
	return nil
}

var _ coach.ContextSource = (*RecoveryContextSource)(nil)
