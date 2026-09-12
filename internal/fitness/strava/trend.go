package strava

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The training trend.
//
// This replaced a 3D landscape whose vertical axis was MET-minutes. The unit
// was defensible arithmetic — duration times a per-sport intensity constant,
// the same shape as session-RPE with a lookup table standing in for the
// self-report — and it was still the wrong thing to put in front of somebody.
// Nobody has a feel for a MET-minute. Worse, the scene only ever showed what
// had happened; it could not say whether anything was changing, which is the
// one question a training history is opened to answer.
//
// So this is deliberately smaller and says more. Minutes, because a person
// knows what an hour of running is. Every session kept separate, because a day
// summed into one number throws away the thing somebody did. And a trailing
// mean to compare the present against, because "a lot" only means anything
// next to what is normal for you.
//
// What it does not do is claim to measure effort. North stores duration,
// distance, elevation and average speed — no heart rate, no power, nothing
// about how hard a session felt. A fitness curve built on top of that would be
// hours trained wearing a better name, which is the criticism the same charts
// attract elsewhere. This chart talks about volume and consistency, and says
// so in those words.

const (
	// normalWindowDays is how far back "normal" looks.
	//
	// Four weeks: long enough that one heavy Saturday does not redefine the
	// baseline, short enough to follow somebody who is genuinely building or
	// genuinely stopping. It is the chronic window the acute:chronic workload
	// literature settled on for the same reason.
	normalWindowDays = 28

	// recentWindowDays is "lately" — the block being compared to normal.
	recentWindowDays = 7

	// defaultTrendWeeks is how much history the chart draws.
	defaultTrendWeeks = 12

	// maxTrendWeeks bounds what one request may ask for, so a hand-edited URL
	// cannot ask the database for a decade in one go.
	maxTrendWeeks = 52
)

// TrendShape is what the last seven days amount to, next to what is normal.
//
// An enum rather than a sentence. The bands are a judgement about training and
// belong with the arithmetic; the wording belongs to whatever is rendering,
// and keeping the two apart is what stops this package importing a vocabulary.
type TrendShape int

const (
	// ShapeNoHistory is an account with nothing in the window at all.
	ShapeNoHistory TrendShape = iota

	// ShapeTooNew is the honest answer for somebody with less than a full
	// normal window behind them: there is training, but no baseline to judge
	// it against. Calling a first week "a big week" would be inventing a trend
	// out of a single data point.
	ShapeTooNew

	ShapeWellDown
	ShapeEasier
	ShapeSteady
	ShapeBuilding
	ShapeBigWeek
)

// TrendDay is one bar: a calendar day and the sessions in it.
//
// Every day in the window gets one, including the days nothing happened. A
// chart with rest days omitted has an axis where the gaps between bars mean
// nothing, and a fortnight off looks like a fortnight of training drawn closer
// together.
type TrendDay struct {
	Date     time.Time
	Sessions []Session
	Minutes  float64

	// Future marks a day in the current week that has not happened yet. It is
	// not a rest day — nobody chose to rest on a Thursday that has not
	// arrived — and drawing it as one would put a small lie at the end of
	// every chart opened before Sunday.
	Future bool
}

// Trend is the whole chart, plus the handful of figures the sentence above it
// is built from.
type Trend struct {
	// Days runs oldest first, one per calendar day, with no gaps.
	Days []TrendDay

	// Normal is the trailing mean of daily minutes, aligned index for index
	// with Days. Same unit as the bars, so the line can be read against them
	// rather than beside them.
	Normal []float64

	RecentMinutes float64 // moving minutes in the last seven days
	NormalMinutes float64 // what seven days normally holds

	// Ratio is RecentMinutes over NormalMinutes. Zero when there is no
	// baseline to divide by.
	Ratio float64

	// StreakWeeks is how many consecutive weeks, counting back from the one
	// being lived in, hold at least one session.
	StreakWeeks int

	Shape TrendShape

	// Totals over the drawn window, for the figures beside the chart.
	Sessions    int
	DistanceM   float64
	ElevationM  float64
	ActiveDays  int
	CountedDays int
}

// Trend reads somebody's training and works out what it is doing.
//
// Reads North's own copy rather than calling Strava, like everything else on
// this page: opening it is fast, it works when Strava is down, and it costs
// nothing against the rate limit.
func (s *Service) Trend(ctx context.Context, userID uuid.UUID, loc *time.Location, weeks int) (Trend, error) {
	if loc == nil {
		loc = time.UTC
	}
	if weeks <= 0 || weeks > maxTrendWeeks {
		weeks = defaultTrendWeeks
	}

	return s.trendAt(ctx, userID, loc, weeks, time.Now().In(loc))
}

// trendAt is Trend with "today" handed in, so the whole thing can be tested
// without waiting for a Tuesday.
func (s *Service) trendAt(ctx context.Context, userID uuid.UUID, loc *time.Location, weeks int, now time.Time) (Trend, error) {
	last := startOfDay(now.In(loc))
	first := last.AddDate(0, 0, -(weeks*7 - 1))

	// The line needs a full normal window of history behind the chart's first
	// day, or the left-hand end would climb out of nothing — an artefact of
	// where the window happens to start, drawn as though it were somebody
	// getting fitter. Those extra days are read, used, and then dropped.
	since := first.AddDate(0, 0, -normalWindowDays)
	until := last.AddDate(0, 0, 1)

	activities, err := s.repo.ActivitiesBetween(ctx, userID, since, until)
	if err != nil {
		return Trend{}, err
	}

	return buildTrend(activities, loc, since, first, last, now), nil
}

// buildTrend is the whole calculation, as a pure function over rows.
func buildTrend(activities []Activity, loc *time.Location, since, first, last, now time.Time) Trend {
	// Every day from the start of the look-back to the end of the chart, so
	// indexing by day offset is exact and no day can be missing.
	total := daysBetween(since, last) + 1
	all := make([]TrendDay, total)
	for i := range all {
		all[i] = TrendDay{Date: since.AddDate(0, 0, i)}
	}

	today := startOfDay(now.In(loc))
	for _, a := range activities {
		session := sessionOf(a, loc)
		i := daysBetween(since, startOfDay(session.StartedAt))
		if i < 0 || i >= len(all) {
			continue
		}
		all[i].Sessions = append(all[i].Sessions, session)
		all[i].Minutes += session.Minutes()
	}

	for i := range all {
		all[i].Future = all[i].Date.After(today)
	}

	offset := daysBetween(since, first)
	out := Trend{
		Days:   all[offset:],
		Normal: trailingMean(all, offset, normalWindowDays),
	}

	for _, day := range out.Days {
		out.Sessions += len(day.Sessions)
		if len(day.Sessions) > 0 {
			out.ActiveDays++
		}
		if !day.Future {
			out.CountedDays++
		}
		for _, session := range day.Sessions {
			out.DistanceM += session.DistanceM
			out.ElevationM += session.ElevationM
		}
	}

	out.RecentMinutes = sumLastDays(all, recentWindowDays)
	out.NormalMinutes = meanOverDays(all, len(all)-recentWindowDays, normalWindowDays) * recentWindowDays
	out.StreakWeeks = streakWeeks(all, today)
	out.Ratio, out.Shape = shapeOf(out.RecentMinutes, out.NormalMinutes, historyDays(all))
	return out
}

// trailingMean is the mean of the preceding window for each drawn day.
//
// Trailing rather than centred: a centred mean would need days that have not
// happened, so the right-hand end — the part anybody actually looks at — would
// be built from a shorter window than the rest and would drift for reasons
// that have nothing to do with training.
func trailingMean(all []TrendDay, offset, window int) []float64 {
	out := make([]float64, 0, len(all)-offset)
	for i := offset; i < len(all); i++ {
		out = append(out, meanOverDays(all, i+1, window))
	}
	return out
}

// meanOverDays averages the `window` days ending just before `end`.
//
// Days that have not happened are left out of both the sum and the count. A
// Thursday counted as a rest day would drag the line down every time the page
// is opened mid-week, and the line would rise again on Sunday with no training
// behind the change.
func meanOverDays(all []TrendDay, end, window int) float64 {
	start := end - window
	if start < 0 {
		start = 0
	}

	var sum float64
	var n int
	for i := start; i < end && i < len(all); i++ {
		if all[i].Future {
			continue
		}
		sum += all[i].Minutes
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func sumLastDays(all []TrendDay, window int) float64 {
	var sum float64
	for i := len(all) - window; i < len(all); i++ {
		if i < 0 || all[i].Future {
			continue
		}
		sum += all[i].Minutes
	}
	return sum
}

// historyDays is how many days of real record there are, counted from the
// first session. It is what decides whether there is enough to judge against.
func historyDays(all []TrendDay) int {
	for i, day := range all {
		if len(day.Sessions) > 0 {
			return len(all) - i
		}
	}
	return 0
}

// shapeOf turns the two figures into a ratio and a verdict.
func shapeOf(recent, normal float64, history int) (float64, TrendShape) {
	if history == 0 {
		return 0, ShapeNoHistory
	}
	// Without a full normal window behind it, there is training but nothing to
	// judge it against. Saying so is the honest answer; calling a first week
	// "a big week" would be inventing a trend from one data point.
	if history < normalWindowDays || normal <= 0 {
		return 0, ShapeTooNew
	}

	ratio := recent / normal
	switch {
	case ratio >= 1.5:
		return ratio, ShapeBigWeek
	case ratio >= 1.15:
		return ratio, ShapeBuilding
	case ratio >= 0.85:
		return ratio, ShapeSteady
	case ratio >= 0.5:
		return ratio, ShapeEasier
	default:
		return ratio, ShapeWellDown
	}
}

// streakWeeks counts back over whole weeks that hold at least one session.
//
// The week being lived in is allowed to be empty without breaking the streak —
// it is Monday morning for somebody, and telling them a nine-week run ended
// because they have not been out yet today would be both wrong and unkind. It
// simply does not count toward the total until something lands in it.
func streakWeeks(all []TrendDay, today time.Time) int {
	weekStart := startOfWeek(today)

	var streak int
	for {
		var found bool
		for _, day := range all {
			if day.Date.Before(weekStart) || !day.Date.Before(weekStart.AddDate(0, 0, 7)) {
				continue
			}
			if len(day.Sessions) > 0 {
				found = true
				break
			}
		}

		if !found {
			// The current week is allowed to be empty; any earlier one ends it.
			if weekStart.Equal(startOfWeek(today)) {
				weekStart = weekStart.AddDate(0, 0, -7)
				continue
			}
			return streak
		}

		streak++
		weekStart = weekStart.AddDate(0, 0, -7)

		// Ran off the start of what was read; the streak is at least this long.
		if len(all) == 0 || weekStart.Before(all[0].Date) {
			return streak
		}
	}
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// startOfWeek is the Monday on or before t.
func startOfWeek(t time.Time) time.Time {
	d := startOfDay(t)
	offset := (int(d.Weekday()) + 6) % 7 // Monday is 0
	return d.AddDate(0, 0, -offset)
}

// daysBetween counts calendar days, not 24-hour spans, so a clock change does
// not put a day in the wrong bar.
func daysBetween(from, to time.Time) int {
	from = startOfDay(from)
	to = startOfDay(to)
	return int(to.Sub(from).Round(24*time.Hour) / (24 * time.Hour))
}
