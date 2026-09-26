package day

import (
	"math"
	"time"
)

// EnergyInputs is everything the energy estimate reads. Pointers are absent
// signals, never zeros: a missing HRV is not an HRV of 0.
type EnergyInputs struct {
	SleepMinutes *int

	HRV         *float64
	HRVBaseline *float64

	RestingHR         *float64
	RestingHRBaseline *float64

	// ActiveKcal is what has been burned so far today.
	ActiveKcal float64

	// WokeAt is when the night ended. Nil falls back to 07:00 on Now's date.
	WokeAt *time.Time
	Now    time.Time
}

const (
	sleepNeedMinutes = 8 * 60

	// Two hours of full tank after waking, then a steady drain. The numbers are
	// a heuristic, chosen so an ordinary day ends in the 20-40% band, not a
	// physiological model; the product claim is "roughly how charged you are".
	energyGraceHours   = 2.0
	energyDrainPerHour = 4.0
	energyDrainPerKcal = 3.0 / 100
)

// Energy estimates how charged someone is right now, from 0 to 100.
//
// It reports false when there is nothing to base an estimate on — no sleep and
// no recovery signal — because a made-up 70% is worse than an empty tile.
func Energy(in EnergyInputs) (int, bool) {
	if in.SleepMinutes == nil && in.HRV == nil && in.RestingHR == nil {
		return 0, false
	}

	sleep := 0.7
	if in.SleepMinutes != nil {
		sleep = clamp(float64(*in.SleepMinutes)/sleepNeedMinutes, 0, 1.1)
	}

	recovery := 0.6
	if ratio, ok := ratioOf(in.HRV, in.HRVBaseline); ok {
		recovery = 0.5 + (ratio-1)*1.5
	}
	// Resting heart rate reads the other way round: lower than usual is better.
	if ratio, ok := ratioOf(in.RestingHRBaseline, in.RestingHR); ok {
		recovery += (ratio - 1) * 1.5
	}
	recovery = clamp(recovery, 0, 1)

	capacity := 100 * (0.6*math.Min(sleep, 1) + 0.4*recovery)

	woke := time.Date(in.Now.Year(), in.Now.Month(), in.Now.Day(), 7, 0, 0, 0, in.Now.Location())
	if in.WokeAt != nil {
		woke = *in.WokeAt
	}
	awake := in.Now.Sub(woke).Hours()
	drain := math.Max(awake-energyGraceHours, 0)*energyDrainPerHour + in.ActiveKcal*energyDrainPerKcal

	return int(math.Round(clamp(capacity-drain, 0, 100))), true
}

func ratioOf(num, den *float64) (float64, bool) {
	if num == nil || den == nil || *den <= 0 {
		return 0, false
	}
	return *num / *den, true
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}
