package activity

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// RouteTotals is the distance and climb a GPS provider recorded over a window.
//
// It lives in this leaf package so internal/fitness/strava can return one
// without the activity slice importing the strava slice — strava already
// imports activity, and the reverse would be a cycle.
type RouteTotals struct {
	Activities int
	DistanceM  float64
	ElevationM float64
}

// TrainingWindow is how a stretch of days went, as the coach needs to read it.
//
// The coach used to see only today's calorie burn, which is enough to comment
// on a session and not enough to comment on a week. Whether someone is ramping
// up, holding steady, or has quietly stopped is the question a training coach
// exists to answer, and it needs several days at once.
type TrainingWindow struct {
	// Days is the length of the window, used for the "no rest days at all in
	// seven" reading rather than shown as a number on its own.
	Days int

	Sessions int
	Duration time.Duration
	Calories float64

	// PriorCalories is the same-length window immediately before this one. The
	// direction of travel is more useful to a coach than the absolute number,
	// which means nothing without a body weight and a training history.
	PriorCalories float64

	// RestDays counts calendar days in the user's own timezone with no
	// completed session, not gaps between sessions: three sessions on one day
	// still leaves six rest days in a week, and a coach should say so.
	RestDays int

	// TopSports is the busiest activity labels first, at most two. More than
	// two reads as a list rather than a characterisation.
	TopSports []string

	// Route is nil unless a GPS provider is connected and recorded something.
	// Distance and climb are not on North's own sessions, so a manual logger
	// legitimately has none.
	Route *RouteTotals
}

// NewTrainingWindow rolls completed sessions up into the window they fall in.
//
// Open sessions are ignored: a run in progress has no final duration or burn,
// and counting it would make the same week read differently depending on when
// the coach was asked.
func NewTrainingWindow(sessions []Session, since time.Time, days int, loc *time.Location) TrainingWindow {
	if loc == nil {
		loc = time.UTC
	}

	w := TrainingWindow{Days: days}

	trained := make(map[string]bool, days)
	counts := make(map[string]int, len(sessions))

	for _, s := range sessions {
		if s.EndedAt == nil {
			continue
		}

		w.Sessions++
		w.Duration += s.Elapsed(*s.EndedAt)
		if s.CaloriesBurned != nil {
			w.Calories += *s.CaloriesBurned
		}

		// Keyed on the end, matching the window the sessions were selected by.
		// Keying on the start would let a session that began before the window
		// count as a rest day inside it.
		trained[s.EndedAt.In(loc).Format(time.DateOnly)] = true
		counts[s.ActivityCode]++
	}

	w.RestDays = restDays(since, days, loc, trained)
	w.TopSports = topSports(counts)

	return w
}

// restDays walks the window a calendar day at a time rather than dividing a
// duration by 24h, because days in a timezone that observes daylight saving
// are not all 24 hours long.
func restDays(since time.Time, days int, loc *time.Location, trained map[string]bool) int {
	start := since.In(loc)
	rest := 0
	for i := range days {
		day := start.AddDate(0, 0, i).Format(time.DateOnly)
		if !trained[day] {
			rest++
		}
	}
	return rest
}

// topSports returns the two busiest activity labels, ties broken by code so
// the same week always reads the same way.
func topSports(counts map[string]int) []string {
	if len(counts) == 0 {
		return nil
	}

	codes := make([]string, 0, len(counts))
	for code := range counts {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool {
		if counts[codes[i]] != counts[codes[j]] {
			return counts[codes[i]] > counts[codes[j]]
		}
		return codes[i] < codes[j]
	})

	if len(codes) > 2 {
		codes = codes[:2]
	}

	labels := make([]string, len(codes))
	for i, code := range codes {
		labels[i] = sportLabel(code)
	}
	return labels
}

// sportLabel is the MET's display name without its parenthetical qualifier:
// "Running (8 km/h)" is a precise thing to log and a clumsy thing to read back
// in a sentence. An unknown code falls back to itself rather than vanishing.
func sportLabel(code string) string {
	met, ok := LookupMET(code)
	if !ok {
		return code
	}
	if i := strings.Index(met.Name, " ("); i > 0 {
		return met.Name[:i]
	}
	return met.Name
}

// Summary is the window as the two or three lines the coach reads.
//
// Returns a slice rather than one string because Context.FitnessSummary is
// rendered as bullets, one entry per line.
func (w TrainingWindow) Summary() []string {
	if w.Sessions == 0 {
		return nil
	}

	lines := []string{w.volumeLine(), w.recoveryLine()}
	if route := w.routeLine(); route != "" {
		lines = append(lines, route)
	}
	return lines
}

func (w TrainingWindow) volumeLine() string {
	line := fmt.Sprintf("Last %d days: %s, %s, ~%.0f kcal",
		w.Days, plural(w.Sessions, "session"), formatDuration(w.Duration), w.Calories)

	switch len(w.TopSports) {
	case 0:
	case 1:
		line += " — " + w.TopSports[0]
	default:
		line += " — mostly " + w.TopSports[0] + " and " + w.TopSports[1]
	}
	return line
}

func (w TrainingWindow) recoveryLine() string {
	rest := plural(w.RestDays, "rest day")
	if w.RestDays == 0 {
		rest = "no rest days"
	}
	return rest + "; " + w.loadTrend()
}

// loadTrend compares this window's burn with the one before it.
//
// A week with nothing before it is reported as exactly that. Expressing it as a
// percentage would mean dividing by zero, and rounding that up to "up 100%"
// would tell the coach a story about a trend that does not exist yet.
func (w TrainingWindow) loadTrend() string {
	if w.PriorCalories <= 0 {
		return "nothing logged the week before"
	}

	change := (w.Calories - w.PriorCalories) / w.PriorCalories * 100
	if math.Abs(change) < 5 {
		return "training load about the same as the week before"
	}
	if change > 0 {
		return fmt.Sprintf("training load up %.0f%% on the week before", change)
	}
	return fmt.Sprintf("training load down %.0f%% on the week before", -change)
}

func (w TrainingWindow) routeLine() string {
	if w.Route == nil || w.Route.DistanceM <= 0 {
		return ""
	}

	line := fmt.Sprintf("Recorded routes: %.1f km", w.Route.DistanceM/1000)
	if w.Route.ElevationM > 0 {
		line += fmt.Sprintf(" with %.0f m of climb", w.Route.ElevationM)
	}
	return line
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// formatDuration reads as a person would say it: "4h20m", "35m". Rounded to
// the minute, because seconds of training time are noise at this scale.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	hours := int(d / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	if hours == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dh%02dm", hours, minutes)
}
