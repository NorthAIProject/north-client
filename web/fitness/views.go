package fitness

import "github.com/NorthAIProject/north-client/internal/fitness/fitnessview"

// The hub's view is declared in internal/fitness/fitnessview, where the JSON
// API can use it without importing the templates. These aliases keep the
// page's signatures as they were.
type (
	Instruments    = fitnessview.Instruments
	HubData        = fitnessview.HubData
	StrengthView   = fitnessview.StrengthView
	StrengthRow    = fitnessview.StrengthRow
	StrengthRecord = fitnessview.StrengthRecord
	CardioView     = fitnessview.CardioView
	WeekBar        = fitnessview.WeekBar
	WeekView       = fitnessview.WeekView
	RecentRow      = fitnessview.RecentRow
	TrendDay       = fitnessview.TrendDay
	Trend          = fitnessview.Trend
	Weight         = fitnessview.Weight
)
