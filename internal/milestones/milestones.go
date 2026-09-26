// Package milestones keeps "months since" trackers — the dentist, a haircut —
// for the vitals strip. They are neither logs nor habits: nothing is
// scheduled, nothing is missed, but their age is worth seeing.
package milestones

import "github.com/NorthAIProject/north-client/internal/milestones/milestone"

type Tracker = milestone.Tracker
