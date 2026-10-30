// Package fitnessview holds the fitness hub's view: the week, recent
// sessions, device trends and the month's lifting and cardio, built by
// internal/fitness from the person's data.
//
// The browser's template in web/fitness renders it and the JSON API projects
// it, so the types live here rather than in the template. internal/fitness
// imports web/fitness for its browser handlers, so a view declared in
// internal/fitness itself could not be imported back by the template — hence
// a package of its own.
package fitnessview

import (
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// Instruments holds the plan and nutrition state the side rail reads.
type Instruments struct {
	PlanID          uuid.UUID
	NextSession     *plan.PlanDay
	DoneToday       *plan.PlanDay
	HasMealProgress bool
	MealProgress    *meals.Progress
}

// HubData is everything the fitness hub renders.
type HubData struct {
	StravaStatus strava.Status
	Instruments  Instruments
	Notice       string

	Week   WeekView
	Recent []RecentRow

	// Steps and VO2 are nil unless a device has pushed that metric; the page
	// leaves the card out rather than drawing a flat line of zeros.
	Steps *Trend
	VO2   *Trend

	Weight *Weight

	// Strength and Cardio are the last 30 days; nil leaves the card out.
	Strength *StrengthView
	Cardio   *CardioView

	// DeviceReadings is the last week from a wearable or phone, already
	// rendered as sentences by internal/health. Empty is the normal state.
	DeviceReadings    []string
	HasDeviceReadings bool

	Location *time.Location
	Now      time.Time
}

// HasPlan reports whether the person has a training plan to show.
func (d HubData) HasPlan() bool {
	return d.Instruments.PlanID != uuid.Nil
}

// StrengthView is the month's lifting, from logged sets.
type StrengthView struct {
	Workouts  int
	Sets      int
	VolumeKg  float64
	Exercises []StrengthRow
	Record    *StrengthRecord
}

// StrengthRow is one exercise's best weight and estimated max, and how far
// the estimate moved over the month.
type StrengthRow struct {
	Name     string
	BestKg   float64
	E1RMKg   float64
	ChangeKg float64
}

// StrengthRecord is the personal record the card features, with the set
// that set it.
type StrengthRecord struct {
	Exercise string
	WeightKg float64
	Reps     int
	E1RMKg   float64
	On       time.Time
}

// CardioView is the month's runs, rides and walks.
type CardioView struct {
	Sessions        int
	Minutes         int
	DistanceKm      float64
	Runs            int
	BestPaceSeconds float64
	Best5KSeconds   float64
	LongestKm       float64
	RestingHR       float64
}

// WeekBar is one day of the weekly strip.
type WeekBar struct {
	Label   string
	Minutes float64
	IsToday bool
}

// WeekView is the "this week" card: three totals over a strip of days.
type WeekView struct {
	Bars       []WeekBar
	Sessions   int
	Minutes    float64
	DistanceKm float64
	Calories   float64
}

// RecentRow is one completed session in the recent list.
type RecentRow struct {
	Name       string
	Category   string
	Code       string
	Source     string
	EndedAt    time.Time
	Duration   time.Duration
	DistanceKm float64
	Calories   float64
}

// TrendDay is one day of a device metric.
type TrendDay struct {
	Day   time.Time
	Value float64
}

// Trend is a device metric card: a chart and the days behind it, oldest first.
type Trend struct {
	Chart viz.Props
	Days  []TrendDay
}

// Weight is the latest weigh-in and its change from the one before.
type Weight struct {
	Kg       float64
	DeltaKg  float64
	HasDelta bool
	At       time.Time
}
