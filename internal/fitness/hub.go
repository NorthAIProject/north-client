package fitness

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
	fitnesspages "github.com/NorthAIProject/north-client/web/fitness"
)

const calorieWindow = 7

// healthWindow matches the window the coach reads, so the page and the
// conversation never quote different numbers at the same person.
const healthWindow = 7

// Options wires the slices the fitness hub composes.
type Options struct {
	Activity *activity.Service
	Workouts *workouts.Service
	Strava   *strava.Service
	Meals    *meals.TrackMealProgressService
	Health   *health.Service

	// Biometrics supplies the weight card. Optional: without it the card
	// shows its empty state.
	Biometrics *biometrics.Service
}

type Service struct {
	activity *activity.Service
	workouts *workouts.Service
	strava   *strava.Service
	meals    *meals.TrackMealProgressService
	health   *health.Service
	bio      *biometrics.Service
}

func NewService(opts Options) *Service {
	return &Service{
		activity: opts.Activity,
		workouts: opts.Workouts,
		strava:   opts.Strava,
		meals:    opts.Meals,
		health:   opts.Health,
		bio:      opts.Biometrics,
	}
}

// DayPoint is one label/value pair for instrument charts.
type DayPoint struct {
	Label string
	Value float64
}

// Snapshot is everything the fitness hub renders.
type Snapshot struct {
	Calories7d    float64
	CalorieSeries []DayPoint

	PlanID      uuid.UUID
	NextSession *plan.PlanDay

	StravaStatus strava.Status

	MealProgress    *meals.Progress
	HasMealProgress bool

	// DeviceReadings is the last week of whatever a wearable or phone has
	// pushed, already rendered as sentences. Empty for the ordinary case of an
	// account with nothing attached.
	DeviceReadings    []string
	HasDeviceReadings bool

	Week   Week
	Recent []RecentSession

	// Steps and VO2 are empty unless a device has pushed that metric.
	Steps []DailyValue
	VO2   []DailyValue

	Weight    WeightReading
	HasWeight bool
}

func (s Snapshot) HasCalorieChart() bool {
	for _, d := range s.CalorieSeries {
		if d.Value > 0 {
			return true
		}
	}
	return s.Calories7d > 0
}

// Load gathers the hub view. Real errors fail the page; missing optional
// slices are empty sections.
func (s *Service) Load(ctx context.Context, user users.User) (Snapshot, error) {
	var snap Snapshot

	loc := user.Location()
	now := time.Now().In(loc)
	since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(calorieWindow - 1))

	if s.activity != nil {
		total, err := s.activity.TotalCaloriesSince(ctx, user.ID, since)
		if err != nil {
			return Snapshot{}, err
		}
		snap.Calories7d = total

		sessions, err := s.activity.List(ctx, user.ID, 100)
		if err != nil {
			return Snapshot{}, err
		}
		snap.CalorieSeries = buildCalorieSeries(loc, since, sessions)
		snap.Week = buildWeek(loc, now, sessions)
		snap.Recent = buildRecent(sessions)
	} else {
		snap.CalorieSeries = emptyCalorieSeries(loc)
		snap.Week = buildWeek(loc, now, nil)
	}

	if s.workouts != nil {
		stored, err := s.workouts.LatestPlan(ctx, user.ID)
		switch {
		case err == nil:
			snap.PlanID = stored.ID
			if day, ok := stored.Plan.NextSession(now); ok {
				snap.NextSession = &day
			}
		case !apperr.Is(err, apperr.ErrNotFound):
			return Snapshot{}, err
		}
	}

	if s.strava != nil {
		status, err := s.strava.Status(ctx, user.ID)
		if err != nil {
			// Marked unavailable rather than rendered as "not connected". A
			// decrypt failure and a never-connected account used to look
			// identical here, and the button that state offers is Connect —
			// which would overwrite a working credential to fix a problem
			// that was never a missing connection.
			slog.Default().Error("read strava status",
				slog.Any("error", err), slog.String("user_id", user.ID.String()))
			status = strava.Status{Configured: s.strava.Configured(), Unavailable: true}
		}
		snap.StravaStatus = status
	}

	if s.meals != nil {
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		progress, err := s.meals.ForDay(ctx, user.ID, today)
		switch {
		case err == nil:
			snap.MealProgress = &progress
			snap.HasMealProgress = true
		case !apperr.Is(err, apperr.ErrNotFound):
			return Snapshot{}, err
		}
	}

	if s.health != nil {
		lines, err := s.health.Summary(ctx, user.ID, now, healthWindow)
		if err != nil {
			return Snapshot{}, err
		}
		snap.DeviceReadings = lines
		snap.HasDeviceReadings = len(lines) > 0

		if err := s.loadDeviceTrends(ctx, user, now, &snap); err != nil {
			return Snapshot{}, err
		}
	}

	if s.bio != nil {
		history, err := s.bio.History(ctx, user.ID, 2)
		if err != nil {
			return Snapshot{}, err
		}
		snap.Weight, snap.HasWeight = latestWeight(history)
	}

	return snap, nil
}

// loadDeviceTrends reads the per-day series behind the steps and VO2 max
// cards. A metric nobody has pushed leaves its series nil, which the page
// reads as "no card", not as a row of zeros.
func (s *Service) loadDeviceTrends(ctx context.Context, user users.User, now time.Time, snap *Snapshot) error {
	loc := user.Location()
	until := now.Add(time.Minute)

	steps, err := s.health.Between(ctx, user.ID, "steps", trailingDaysAt(loc, now, stepsWindow)[0], until)
	if err != nil {
		return err
	}
	if len(steps) > 0 {
		snap.Steps = dailyTotals(loc, now, stepsWindow, steps)
	}

	vo2, err := s.health.Between(ctx, user.ID, "vo2max", trailingDaysAt(loc, now, vo2Window)[0], until)
	if err != nil {
		return err
	}
	snap.VO2 = dailyMeans(loc, vo2)
	return nil
}

func buildView(snap Snapshot) fitnesspages.Instruments {
	return fitnesspages.Instruments{
		PlanID:          snap.PlanID,
		NextSession:     snap.NextSession,
		HasMealProgress: snap.HasMealProgress,
		MealProgress:    snap.MealProgress,
	}
}

// buildHubData turns a snapshot into the page's view model.
func buildHubData(snap Snapshot, loc *time.Location, now time.Time, notice string) fitnesspages.HubData {
	data := fitnesspages.HubData{
		StravaStatus:      snap.StravaStatus,
		Instruments:       buildView(snap),
		Notice:            notice,
		Week:              weekView(snap),
		Recent:            recentRows(snap.Recent),
		Steps:             trendView("fitness-steps", "var(--north-signal)", snap.Steps),
		VO2:               trendView("fitness-vo2", "var(--north-agent)", snap.VO2),
		DeviceReadings:    snap.DeviceReadings,
		HasDeviceReadings: snap.HasDeviceReadings,
		Location:          loc,
		Now:               now,
	}
	if snap.HasWeight {
		data.Weight = &fitnesspages.Weight{
			Kg:       snap.Weight.Kg,
			DeltaKg:  snap.Weight.DeltaKg,
			HasDelta: snap.Weight.HasDelta,
			At:       snap.Weight.At,
		}
	}
	return data
}

func weekView(snap Snapshot) fitnesspages.WeekView {
	bars := make([]fitnesspages.WeekBar, len(snap.Week.Days))
	for i, d := range snap.Week.Days {
		bars[i] = fitnesspages.WeekBar{Label: d.Label, Minutes: d.Minutes, IsToday: d.IsToday}
	}
	return fitnesspages.WeekView{
		Bars:       bars,
		Sessions:   snap.Week.Sessions,
		Minutes:    snap.Week.Minutes,
		DistanceKm: snap.Week.DistanceKm,
		Calories:   snap.Calories7d,
	}
}

func recentRows(sessions []RecentSession) []fitnesspages.RecentRow {
	out := make([]fitnesspages.RecentRow, len(sessions))
	for i, s := range sessions {
		out[i] = fitnesspages.RecentRow{
			Name:       s.Name,
			Category:   s.Category,
			Code:       s.ActivityCode,
			Source:     s.Source,
			EndedAt:    s.EndedAt,
			Duration:   s.Duration,
			DistanceKm: s.DistanceKm,
			Calories:   s.Calories,
		}
	}
	return out
}

// trendView builds a device metric card, or nil when the series has no
// reading at all, so the page leaves the card out.
func trendView(id, colorVar string, series []DailyValue) *fitnesspages.Trend {
	// Today is usually still counting and often empty; a line that dives to
	// zero at its right edge reads as a collapse rather than "not synced yet".
	for len(series) > 0 && series[len(series)-1].Value <= 0 {
		series = series[:len(series)-1]
	}
	var hasData bool
	labels := make([]string, len(series))
	values := make([]float64, len(series))
	days := make([]fitnesspages.TrendDay, len(series))
	for i, d := range series {
		labels[i] = d.Day.Format("2 Jan")
		values[i] = d.Value
		days[i] = fitnesspages.TrendDay{Day: d.Day, Value: d.Value}
		hasData = hasData || d.Value > 0
	}
	if !hasData {
		return nil
	}
	return &fitnesspages.Trend{
		Chart: viz.AreaLine(id, labels, values, colorVar),
		Days:  days,
	}
}

func buildCalorieSeries(loc *time.Location, since time.Time, sessions []activity.Session) []DayPoint {
	days := trailingDays(loc, calorieWindow)
	totals := make(map[string]float64, len(days))
	for _, d := range days {
		totals[d.Format("2006-01-02")] = 0
	}

	for _, session := range sessions {
		if session.Status != activity.StatusCompleted || session.EndedAt == nil || session.CaloriesBurned == nil {
			continue
		}
		ended := session.EndedAt.In(loc)
		if ended.Before(since) {
			continue
		}
		key := time.Date(ended.Year(), ended.Month(), ended.Day(), 0, 0, 0, 0, loc).Format("2006-01-02")
		if _, ok := totals[key]; ok {
			totals[key] += *session.CaloriesBurned
		}
	}

	out := make([]DayPoint, len(days))
	for i, d := range days {
		key := d.Format("2006-01-02")
		out[i] = DayPoint{Label: d.Format("Mon"), Value: totals[key]}
	}
	return out
}

func emptyCalorieSeries(loc *time.Location) []DayPoint {
	days := trailingDays(loc, calorieWindow)
	out := make([]DayPoint, len(days))
	for i, d := range days {
		out[i] = DayPoint{Label: d.Format("Mon"), Value: 0}
	}
	return out
}

func trailingDays(loc *time.Location, count int) []time.Time {
	return trailingDaysAt(loc, time.Now(), count)
}
