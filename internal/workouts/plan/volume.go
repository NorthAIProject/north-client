package plan

import "math"

// Volume is how hard a week trains against the plan as written, chosen in the
// weekly review. It changes what a week asks for without editing the plan:
// next week the plan is back as written unless the review says otherwise.
type Volume string

const (
	VolumeHold   Volume = "hold"
	VolumeBuild  Volume = "build"
	VolumeDeload Volume = "deload"
)

// Valid reports whether v is one of the three volumes.
func (v Volume) Valid() bool {
	return v == VolumeHold || v == VolumeBuild || v == VolumeDeload
}

// deloadShare is the share of sets a deload week keeps: enough to keep the
// movement practised, little enough to recover.
const deloadShare = 0.6

// buildExercises is how many of a day's exercises get an extra set in a build
// week: the first ones, which are the main lifts in a generated plan.
const buildExercises = 2

// SetsFor is the number of sets an exercise at index in a day asks for this
// week. Hold, and anything unknown, is the plan as written.
func (v Volume) SetsFor(index, sets int) int {
	switch v {
	case VolumeDeload:
		return max(1, int(math.Round(float64(sets)*deloadShare)))
	case VolumeBuild:
		if index < buildExercises {
			return sets + 1
		}
		return sets
	default:
		return sets
	}
}

// Day returns d with its sets adjusted for the week. d is not changed.
func (v Volume) Day(d PlanDay) PlanDay {
	exercises := make([]Exercise, len(d.Exercises))
	for i, e := range d.Exercises {
		e.Sets = v.SetsFor(i, e.Sets)
		exercises[i] = e
	}
	d.Exercises = exercises
	return d
}
