package forecast

import (
	"sort"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

// Reasons a lighter week is suggested. Codes; the service words them.
const (
	ReasonLiftsStalled   = "lifts_stalled"
	ReasonLongSinceLight = "long_since_lighter_week"
	ReasonRecoveryLow    = "recovery_low"
	ReasonRepsFalling    = "reps_falling"
)

// Deload thresholds.
const (
	// deloadReasons is how many independent signs it takes. One is noise:
	// a bad week of sleep, or a single stuck lift, does not call for it.
	deloadReasons = 2
	// stalledLifts is how many lifts must be stuck or sliding.
	stalledLifts = 2
	// weeksSinceLight is how long training has run without a lighter week.
	weeksSinceLight = 6
	// lowRecoveryDays is how many of the last seven mornings read low.
	lowRecoveryDays = 3
	// lighterWeek is a week whose working sets are under this share of the
	// four weeks before it.
	lighterWeek = 0.7
)

// DeloadInput is what the suggestion is read from.
type DeloadInput struct {
	// Progress is each forecast lift's state.
	Progress []string
	// WeeksSinceLighter is training weeks since the last lighter one, or
	// negative when there is too little history to tell.
	WeeksSinceLighter int
	// LowRecoveryDays is how many of the last seven mornings the recovery
	// score read low. Zero when there is no recovery data.
	LowRecoveryDays int
	// RepsFalling is true when some lift's reps at the same load have
	// fallen two sessions running.
	RepsFalling bool
}

// Deload is the suggestion to take a lighter week.
type Deload struct {
	Suggested bool
	// Reasons are the signs that were present, whether or not there were
	// enough of them to suggest it.
	Reasons []string
}

// SuggestDeload suggests a lighter week when at least two independent signs
// agree. It only ever suggests: the person chooses the week's volume.
func SuggestDeload(in DeloadInput) Deload {
	var reasons []string
	stalled := 0
	for _, p := range in.Progress {
		if Stalled(p) {
			stalled++
		}
	}
	if stalled >= stalledLifts {
		reasons = append(reasons, ReasonLiftsStalled)
	}
	if in.WeeksSinceLighter >= weeksSinceLight {
		reasons = append(reasons, ReasonLongSinceLight)
	}
	if in.LowRecoveryDays >= lowRecoveryDays {
		reasons = append(reasons, ReasonRecoveryLow)
	}
	if in.RepsFalling {
		reasons = append(reasons, ReasonRepsFalling)
	}
	return Deload{Suggested: len(reasons) >= deloadReasons, Reasons: reasons}
}

// WeeksSinceLighter counts the full training weeks since the last lighter
// one, weeks running Monday to Sunday in loc. A week with no sets at all is
// rest and resets the count like a lighter week does. Negative when fewer
// than five full weeks of history exist, because a lighter week is only
// defined against the four before it.
func WeeksSinceLighter(sets []lift.Set, now time.Time, loc *time.Location) int {
	thisWeek := weekStart(now.In(loc))
	counts := map[time.Time]int{}
	var first time.Time
	for _, s := range lift.Working(sets) {
		if s.PerformedAt.After(now) {
			continue
		}
		w := weekStart(s.PerformedAt.In(loc))
		counts[w]++
		if first.IsZero() || w.Before(first) {
			first = w
		}
	}
	if first.IsZero() {
		return -1
	}

	// Full weeks only, oldest first: the week in progress is not over.
	var weeks []int
	for w := first; w.Before(thisWeek); w = w.AddDate(0, 0, 7) {
		weeks = append(weeks, counts[w])
	}
	if len(weeks) < 5 {
		return -1
	}

	since := 0
	for i := len(weeks) - 1; i >= 4; i-- {
		before := float64(weeks[i-1]+weeks[i-2]+weeks[i-3]+weeks[i-4]) / 4
		if weeks[i] == 0 || (before > 0 && float64(weeks[i]) < before*lighterWeek) {
			return since
		}
		since++
	}
	return since
}

func weekStart(t time.Time) time.Time {
	y, m, d := t.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	back := (int(day.Weekday()) + 6) % 7 // Monday is 0
	return day.AddDate(0, 0, -back)
}

// RepsFalling reports whether any lift's best reps at one load have fallen
// in each of its last two sessions: the same bar, fewer reps, twice. That is
// fatigue showing before the estimated max does.
func RepsFalling(sets []lift.Set, now time.Time) bool {
	type session struct {
		day    time.Time
		weight float64
		reps   int
	}
	byLift := map[string]map[time.Time]session{}
	for _, s := range lift.Working(sets) {
		if s.PerformedAt.After(now) || s.WeightKg <= 0 {
			continue
		}
		key, day := s.Key(), dayOf(s)
		if byLift[key] == nil {
			byLift[key] = map[time.Time]session{}
		}
		// A session is summed up by its heaviest set, and the most reps
		// done at that load.
		cur, ok := byLift[key][day]
		if !ok || s.WeightKg > cur.weight || (s.WeightKg == cur.weight && s.Reps > cur.reps) {
			byLift[key][day] = session{day: day, weight: s.WeightKg, reps: s.Reps}
		}
	}
	for _, days := range byLift {
		sessions := make([]session, 0, len(days))
		for _, s := range days {
			sessions = append(sessions, s)
		}
		sort.Slice(sessions, func(i, j int) bool { return sessions[i].day.Before(sessions[j].day) })
		n := len(sessions)
		if n < 3 {
			continue
		}
		a, b, c := sessions[n-3], sessions[n-2], sessions[n-1]
		if a.weight == b.weight && b.weight == c.weight && a.reps > b.reps && b.reps > c.reps {
			return true
		}
	}
	return false
}
