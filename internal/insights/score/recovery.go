package score

import "math"

// Recovery signal keys, in the order the score reads them.
const (
	RecoveryHRV       = "recovery.hrv"
	RecoveryRestingHR = "recovery.resting_heart_rate"
	RecoverySleep     = "recovery.sleep"
)

// Weights for recovery. HRV leads because it moves first after hard training,
// a late night or an oncoming cold; resting heart rate and sleep confirm it.
// They sum to 100 — see TestRecoveryWeightsSumToOneHundred.
const (
	recoveryWeightHRV       = 40
	recoveryWeightRestingHR = 30
	recoveryWeightSleep     = 30
)

// recoveryMinSignals is how many signals a recovery needs. One is a reading:
// a single low HRV night is not a statement about the whole body.
const recoveryMinSignals = 2

// RecoverySignal is one of today's readings against the person's own usual,
// as standard deviations from it (see stat.Baseline.Place).
type RecoverySignal struct {
	Key   string
	Z     float64
	Known bool
}

// Recovery reads how today's body compares with its own usual: HRV (higher
// is better), resting heart rate (lower is better) and last night's sleep
// (longer is better).
//
// A day at the usual scores 75, an "ok", because usual is fine. Inside the
// usual range (within one deviation) a signal moves the score only a little,
// so ordinary wobble never adds up to a low morning; outside it, each further
// deviation below costs a quarter of the signal's weight and each above adds
// a tenth. A drop reads more strongly than a rise: the score exists to say
// when to ease off, and a good day needs no warning.
func Recovery(signals []RecoverySignal) Score {
	weights := map[string]int{
		RecoveryHRV:       recoveryWeightHRV,
		RecoveryRestingHR: recoveryWeightRestingHR,
		RecoverySleep:     recoveryWeightSleep,
	}
	better := map[string]float64{RecoveryHRV: 1, RecoveryRestingHR: -1, RecoverySleep: 1}

	out := Score{Domain: "recovery"}
	var earned float64
	known := 0
	for _, s := range signals {
		weight, ok := weights[s.Key]
		if !ok {
			continue
		}
		c := Component{Key: s.Key, Weight: weight, Known: s.Known}
		if s.Known {
			share := recoveryShare(better[s.Key] * s.Z)
			c.Earned = int(math.Round(share * float64(weight)))
			earned += share * float64(weight)
			out.Coverage += weight
			known++
		}
		out.Components = append(out.Components, c)
	}
	if known < recoveryMinSignals {
		return out
	}
	out.HasData = true
	out.Points = int(math.Round(earned * 100 / float64(out.Coverage)))
	return out
}

// recoveryShare is the share of a signal's weight earned at v deviations from
// the usual, already turned so that positive is the good direction. It is
// continuous: 0.70 at one deviation under, 0.80 at one over.
func recoveryShare(v float64) float64 {
	switch {
	case v <= -1:
		return math.Max(0.70+0.25*(v+1), 0)
	case v < 1:
		return 0.75 + 0.05*v
	default:
		return math.Min(0.80+0.1*(v-1), 1)
	}
}
