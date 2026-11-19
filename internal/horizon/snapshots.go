package horizon

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/horizon/forecast"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Horizon has two track records, and says which one it is showing.
//
// The backtest is recomputed on every read: the forecasts the method would
// have made in past weeks, scored against what followed. It exists from the
// first day, and is worded "would have landed".
//
// The recorded one is forecasts actually made and stored at the time, scored
// once their day arrives. It cannot be flattered by a later change to the
// method, so once a series has enough of them it replaces the backtest, and
// is worded "landed".
const (
	RecordBacktest = "backtest"
	RecordRecorded = "recorded"
)

// SnapshotHorizons are the weeks ahead a forecast is recorded at.
var SnapshotHorizons = []int{4, 8}

// recordHorizonWeeks is the horizon the shown record is for.
const recordHorizonWeeks = 4

// snapshotEvery is how often a person's forecasts are recorded.
const snapshotEvery = 7 * forecast.Day

// Series keys in the snapshot table.
const (
	SeriesWeight = "weight"
	SeriesWaist  = "waist"
)

// LiftSeries is a lift's key in the snapshot table.
func LiftSeries(key string) string { return "lift:" + key }

// Record is a track record and which kind it is.
type Record struct {
	forecast.TrackRecord
	Kind string
}

// Records is each series' track record at four weeks: the recorded one where
// enough stored forecasts have come due, the backtest otherwise.
func (s *Service) Records(ctx context.Context, user users.User, h Horizon, points map[string][]forecast.Point) (map[string]Record, error) {
	out := map[string]Record{SeriesWeight: {h.Weight.Track, RecordBacktest}, SeriesWaist: {h.Waist.Track, RecordBacktest}}
	for _, l := range h.Lifts {
		out[LiftSeries(l.Key)] = Record{l.Track, RecordBacktest}
	}
	if s.o.Snapshots == nil {
		return out, nil
	}
	stored, err := s.o.Snapshots.List(ctx, user.ID, recordHorizonWeeks, h.At.Add(-bodyWindow))
	if err != nil {
		return nil, err
	}
	recorded := map[string]forecast.TrackRecord{}
	for _, snap := range stored {
		due := noon(snap.MadeOn, user).Add(time.Duration(snap.HorizonWeeks) * 7 * forecast.Day)
		if due.After(h.At) {
			continue
		}
		actual, ok := forecast.ObservedNear(points[snap.SeriesKey], due)
		if !ok {
			continue // nothing was measured around the day: it cannot be scored
		}
		r := recorded[snap.SeriesKey]
		r.HorizonWeeks = snap.HorizonWeeks
		r.Total++
		if actual >= snap.Low && actual <= snap.High {
			r.Hits++
		}
		recorded[snap.SeriesKey] = r
	}
	for key, r := range recorded {
		if _, shown := out[key]; shown && r.Shown() {
			out[key] = Record{r, RecordRecorded}
		}
	}
	return out, nil
}

// Points is every series' observations by snapshot key, for scoring stored
// forecasts against.
func (s *Service) Points(ctx context.Context, user users.User) (map[string][]forecast.Point, error) {
	sets, _, now, err := s.o.Lifts.History(ctx, user)
	if err != nil {
		return nil, err
	}
	body, err := s.body(ctx, user, now)
	if err != nil {
		return nil, err
	}
	out := map[string][]forecast.Point{
		SeriesWeight: forecast.DailyMeans(body.weighIns),
		SeriesWaist:  body.waists,
	}
	for _, h := range forecast.LiftHistories(sets) {
		out[LiftSeries(h.Key)] = h.Points
	}
	return out, nil
}

// RecordSnapshots stores this week's forecasts for a person, unless some
// were stored within the last week. It reports whether it wrote any. Only
// the plain forecast is ever recorded, and only series that are ready.
func (s *Service) RecordSnapshots(ctx context.Context, user users.User) (bool, error) {
	if s.o.Snapshots == nil {
		return false, nil
	}
	h, err := s.Forecast(ctx, user, forecast.Scenario{})
	if err != nil {
		return false, err
	}
	y, m, d := h.At.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	last, err := s.o.Snapshots.LatestDay(ctx, user.ID)
	if err != nil {
		return false, err
	}
	if !last.IsZero() && today.Sub(last) < snapshotEvery {
		return false, nil
	}

	var snaps []Snapshot
	add := func(key string, series forecast.Series) {
		if series.Status != forecast.StatusReady {
			return
		}
		for _, weeks := range SnapshotHorizons {
			if r, ok := series.At(weeks * 7); ok {
				snaps = append(snaps, Snapshot{MadeOn: today, SeriesKey: key, HorizonWeeks: weeks, Low: r.Low, Mid: r.Mid, High: r.High})
			}
		}
	}
	for _, l := range h.Lifts {
		add(LiftSeries(l.Key), l.Series)
	}
	add(SeriesWeight, h.Weight)
	add(SeriesWaist, h.Waist)
	if len(snaps) == 0 {
		return false, nil
	}
	return true, s.o.Snapshots.Add(ctx, user.ID, snaps)
}

// noon of a stored calendar date in the person's zone.
func noon(day time.Time, user users.User) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, 12, 0, 0, 0, user.Location())
}
