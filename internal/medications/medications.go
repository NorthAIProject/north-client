// Package medications keeps the medications a person takes, the schedule they
// take them on, and each dose taken or skipped. It records what it is told and
// never advises on a dose; reminders are the alarm a person set, nothing more.
package medications

import "github.com/NorthAIProject/north-client/internal/medications/medication"

type (
	Medication = medication.Medication
	Dose       = medication.Dose
	Day        = medication.Day
	Input      = medication.Input
	Reminder   = medication.Reminder
)
