// Package progresschecks keeps a person's progress checks: weight and a few
// circumferences on a cadence they chose, with private photos as evidence. The
// latest confirmed check shapes the body figure every client draws.
//
// Named apart from internal/checkins, which is the daily mood check-in.
package progresschecks

import "github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"

type (
	Check        = progresscheck.Check
	Input        = progresscheck.Input
	Photo        = progresscheck.Photo
	Schedule     = progresscheck.Schedule
	Measurements = progresscheck.Measurements
)

type Comparison = progresscheck.Comparison
