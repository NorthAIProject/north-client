package score

// Weights for the progress domain. They sum to 100 — see the weights test.
const (
	progressWeightNotes   = 40
	progressWeightOverdue = 35
	progressWeightStreak  = 25
)

// ProgressInput is counts rather than records.
//
// Goals live in a slice package that is not a leaf, and nothing here needs a
// goal's title or category — only how many there are and how much they moved.
// Taking integers keeps this package importable from anywhere.
type ProgressInput struct {
	ActiveGoals int

	// Notes is goal updates written during the window.
	Notes int

	// Overdue is milestones past their target date, across all goals.
	Overdue int

	// Streak is consecutive days checked in, as of now.
	Streak int

	WindowDays int
}

// A note a week per goal is a tended goal, and a week of check-ins is a live
// streak. Both are the point at which the component stops asking for more.
const (
	notesPerGoalPerWeek = 1.0
	streakTargetDays    = 7.0
)

// overdueTolerance is the number of slipped milestones per goal at which the
// component reaches zero. One slipped milestone is a week; four per goal is a
// plan that has stopped describing reality.
const overdueTolerance = 2.0

// Progress reads how much the goals moved, how far they have slipped, and
// whether the check-in habit is alive.
func Progress(in ProgressInput) Score {
	return New("progress", []Component{
		notesComponent(in),
		overdueComponent(in),
		streakComponent(in),
	})
}

func notesComponent(in ProgressInput) Component {
	c := Component{Key: "notes", Weight: progressWeightNotes}
	if in.ActiveGoals <= 0 || in.WindowDays <= 0 {
		return c
	}

	c.Known = true
	weeks := float64(in.WindowDays) / 7
	target := notesPerGoalPerWeek * float64(in.ActiveGoals) * weeks
	c.Earned = award(c.Weight, float64(in.Notes)/target)
	return c
}

func overdueComponent(in ProgressInput) Component {
	c := Component{Key: "overdue", Weight: progressWeightOverdue}
	if in.ActiveGoals <= 0 {
		return c
	}

	// Measured per goal, not in absolute terms: three slipped milestones
	// across ten goals is a different situation from three across one.
	c.Known = true
	slip := float64(in.Overdue) / float64(in.ActiveGoals)
	c.Earned = award(c.Weight, 1-progress(slip, 0, overdueTolerance))
	return c
}

func streakComponent(in ProgressInput) Component {
	c := Component{Key: "streak", Weight: progressWeightStreak}
	if in.Streak <= 0 {
		return c
	}

	c.Known = true
	c.Earned = award(c.Weight, float64(in.Streak)/streakTargetDays)
	return c
}
