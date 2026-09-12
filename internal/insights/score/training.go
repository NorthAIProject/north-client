package score

import (
	"sort"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
)

// Weights for the training domain. They sum to 100 — see the weights test.
const (
	trainingWeightFrequency   = 40
	trainingWeightVolume      = 30
	trainingWeightConsistency = 30
)

// sessionsPerWeek is the cadence that earns full marks on frequency.
const sessionsPerWeek = 3.0

// minSessionDays is how many separate days it takes before a rhythm exists to
// judge. Two sessions in one afternoon are one day of training.
const minSessionDays = 2

// spanTargetShare is how much of the window training has to reach across to
// earn full marks. Deliberately not 1.0: nobody trains on the first and last
// day of every window, and demanding it would make the component unreachable.
const spanTargetShare = 0.7

// Training reads how often somebody trained, how the burn compares with the
// window before, and whether the sessions were spread or bunched.
//
// calories and prior are the totals the insights service already loads for the
// current and previous windows.
func Training(sessions []activity.Session, calories, prior float64, windowDays int) Score {
	days := sessionDays(sessions)

	return New("training", []Component{
		frequencyComponent(len(sessions), windowDays),
		volumeComponent(calories, prior),
		consistencyComponent(days, windowDays),
	})
}

func frequencyComponent(sessions, windowDays int) Component {
	c := Component{Key: "frequency", Weight: trainingWeightFrequency}
	if sessions == 0 || windowDays <= 0 {
		return c
	}

	c.Known = true
	target := sessionsPerWeek * float64(windowDays) / 7
	c.Earned = award(c.Weight, float64(sessions)/target)
	return c
}

func volumeComponent(calories, prior float64) Component {
	c := Component{Key: "volume", Weight: trainingWeightVolume}
	// No previous window means no comparison to make. The dashboard's delta
	// takes the same position: without a prior, there is no claim.
	if prior <= 0 {
		return c
	}

	c.Known = true
	c.Earned = award(c.Weight, calories/prior)
	return c
}

// consistencyComponent measures how much of the window the training reached
// across, rather than the widest gap inside it.
//
// The gap version scored a fortnight of daily sessions followed by a fortnight
// of nothing as perfectly consistent, because every gap it could see was one
// day. What distinguishes a month of training from a burst is where the last
// session sits, and only the span sees that.
func consistencyComponent(days []time.Time, windowDays int) Component {
	c := Component{Key: "consistency", Weight: trainingWeightConsistency}
	if len(days) < minSessionDays || windowDays <= 0 {
		return c
	}

	first, last := days[0], days[len(days)-1]
	span := last.Sub(first).Hours()/24 + 1

	c.Known = true
	c.Earned = award(c.Weight, span/(spanTargetShare*float64(windowDays)))
	return c
}

// sessionDays is the distinct local days that carried a session, in order.
// Distinct because two sessions in one afternoon are one day of training, and
// counting them twice would report a rhythm nobody kept.
func sessionDays(sessions []activity.Session) []time.Time {
	seen := make(map[time.Time]struct{}, len(sessions))
	for _, s := range sessions {
		day := s.StartedAt.Truncate(24 * time.Hour)
		seen[day] = struct{}{}
	}

	out := make([]time.Time, 0, len(seen))
	for day := range seen {
		out = append(out, day)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}
