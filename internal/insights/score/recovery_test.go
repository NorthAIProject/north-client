package score

import "testing"

func signals(hrv, rhr, sleep *float64) []RecoverySignal {
	sig := func(key string, z *float64) RecoverySignal {
		if z == nil {
			return RecoverySignal{Key: key}
		}
		return RecoverySignal{Key: key, Z: *z, Known: true}
	}
	return []RecoverySignal{sig(RecoveryHRV, hrv), sig(RecoveryRestingHR, rhr), sig(RecoverySleep, sleep)}
}

func z(v float64) *float64 { return &v }

func TestRecoveryReadsEachSignalTheRightWayUp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		hrv, rhr, slp *float64
		points        int
		verdict       Verdict
	}{
		{"everything at the usual", z(0), z(0), z(0), 75, VerdictOK},
		// HRV up is good; resting heart rate up is the bad direction.
		{"hrv up, resting HR down", z(2), z(-2), nil, 90, VerdictStrong},
		{"hrv down, resting HR up", z(-2), z(2), nil, 45, VerdictLow},
		{"ordinary wobble stays ok", z(-0.9), z(0.9), z(-0.9), 71, VerdictOK},
		{"a deviation and a half under", z(-1.5), nil, z(-1.5), 58, VerdictUneven},
		{"HRV alone well under", z(-1.5), z(0), z(0), 68, VerdictUneven},
		{"far outliers clamp", z(9), z(-9), nil, 100, VerdictStrong},
		{"far under bottoms out", z(-9), nil, z(-9), 0, VerdictLow},
	}
	for _, c := range cases {
		r := Recovery(signals(c.hrv, c.rhr, c.slp))
		if !r.HasData || r.Points != c.points || r.Verdict() != c.verdict {
			t.Errorf("%s: %d %s (has data %v), want %d %s", c.name, r.Points, r.Verdict(), r.HasData, c.points, c.verdict)
		}
	}
}

// One signal is a reading, not a recovery: a single low HRV night must not
// render as a confident score about the whole body.
func TestRecoveryNeedsTwoSignals(t *testing.T) {
	t.Parallel()
	r := Recovery(signals(z(-2), nil, nil))
	if r.HasData || r.Verdict() != VerdictUnknown {
		t.Errorf("one signal scored %+v", r)
	}
	if len(r.Components) != 3 || r.Components[0].Known != true || r.Components[1].Known {
		t.Errorf("components %+v", r.Components)
	}
}

func TestRecoveryWeightsSumToOneHundred(t *testing.T) {
	t.Parallel()
	if recoveryWeightHRV+recoveryWeightRestingHR+recoveryWeightSleep != 100 {
		t.Error("recovery weights must sum to 100")
	}
}
