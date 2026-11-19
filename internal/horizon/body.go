package horizon

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/bodymap"
	"github.com/NorthAIProject/north-client/internal/horizon/forecast"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

const metricBodyMass = "body_mass"

// bodyData is what the weight, waist and figure forecasts are read from.
type bodyData struct {
	weighIns []forecast.Point
	waists   []forecast.Point
	heightCm float64
	// latest is the newest check's measurements: the girths the figure
	// keeps, since only weight and waist are forecast.
	latest   progresscheck.Measurements
	hasCheck bool
	targetKg *float64
}

// body gathers every weight the person has given, from all three places one
// can land: typed into the app, synced from a phone or scale, or entered
// with a progress check. A check whose weight was only carried over from an
// earlier one is not a weigh-in and is skipped.
func (s *Service) body(ctx context.Context, user users.User, now time.Time) (bodyData, error) {
	var out bodyData
	since := now.Add(-bodyWindow)

	if s.o.Measurements != nil {
		rows, err := s.o.Measurements.History(ctx, user.ID, 400)
		if err != nil {
			return bodyData{}, err
		}
		for i, b := range rows {
			if i == 0 {
				out.heightCm = b.HeightCm // newest first
			}
			if b.WeightKg > 0 && !b.CreatedAt.Before(since) {
				out.weighIns = append(out.weighIns, forecast.Point{At: b.CreatedAt.In(user.Location()), Value: b.WeightKg})
			}
		}
	}
	if s.o.Readings != nil {
		rows, err := s.o.Readings.Between(ctx, user.ID, metricBodyMass, since, now)
		if err != nil {
			return bodyData{}, err
		}
		for _, r := range rows {
			if r.Value > 0 {
				out.weighIns = append(out.weighIns, forecast.Point{At: r.StartedAt.In(user.Location()), Value: r.Value})
			}
		}
	}
	if s.o.Checks != nil {
		checks, err := s.o.Checks.History(ctx, user)
		if err != nil {
			return bodyData{}, err
		}
		var newest time.Time
		for _, c := range checks {
			at := checkTime(c, user)
			if !at.Before(newest) {
				newest, out.latest, out.hasCheck = at, c.Measurements, true
			}
			if at.Before(since) {
				continue
			}
			if c.WeightKg != nil && *c.WeightKg > 0 && !c.WeightCarried {
				out.weighIns = append(out.weighIns, forecast.Point{At: at, Value: *c.WeightKg})
			}
			if c.WaistCm != nil && *c.WaistCm > 0 {
				out.waists = append(out.waists, forecast.Point{At: at, Value: *c.WaistCm})
			}
		}
	}
	if s.o.Targets != nil {
		prefs, err := s.o.Targets.Get(ctx, user.ID)
		if err != nil && !apperr.Is(err, apperr.ErrNotFound) {
			return bodyData{}, err
		}
		if err == nil {
			out.targetKg = prefs.TargetWeightKg
		}
	}
	return out, nil
}

// checkTime is noon of the day a check was measured on, in the person's
// zone: a date has no time of day, and noon keeps it on its own day
// wherever it is read.
func checkTime(c progresscheck.Check, user users.User) time.Time {
	y, m, d := c.MeasuredOn.Date()
	return time.Date(y, m, d, 12, 0, 0, 0, user.Location())
}

// shapeAt is the figure's shape the given days ahead: the latest check's
// girths, with the waist and the overall build moved to where their
// forecasts put them. Chest and hips hold, because nothing forecasts them.
// It is the same function that shapes today's figure, fed forecast numbers.
func (b bodyData) shapeAt(weight, waist forecast.Series, offsetDays int) bodymap.Shape {
	if !b.hasCheck {
		return bodymap.Shape{}
	}
	m := b.latest
	if r, ok := nearest(waist, offsetDays); ok {
		v := r.Mid
		m.WaistCm = &v
	}
	var bmi float64
	kg := 0.0
	if r, ok := nearest(weight, offsetDays); ok {
		kg = r.Mid
	} else if m.WeightKg != nil {
		kg = *m.WeightKg
	}
	if kg > 0 && b.heightCm > 0 {
		h := b.heightCm / 100
		bmi = kg / (h * h)
	}
	return progresscheck.Shape(m, b.heightCm, bmi, bodymap.ShellBaseline)
}

// nearest is a series' range at offsetDays, or at its last day when the
// series stops short of it: past where a forecast dares to go, the figure
// holds its last shape rather than snapping back to today's.
func nearest(s forecast.Series, offsetDays int) (forecast.Range, bool) {
	if s.Status != forecast.StatusReady {
		return forecast.Range{}, false
	}
	if r, ok := s.At(offsetDays); ok {
		return r, true
	}
	last, ok := s.Last()
	if !ok || offsetDays < last.OffsetDays {
		return forecast.Range{}, false
	}
	return last.Range, true
}
