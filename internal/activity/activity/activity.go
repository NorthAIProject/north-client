// Package activity holds the shape of a tracked exercise session and the MET
// reference table used to estimate its calorie burn.
//
// A leaf, so the activity service and anything that renders a session can
// both import it without importing each other. See CLAUDE.md on slice layout.
package activity

import (
	"time"

	"github.com/google/uuid"
)

// Status values a session moves through: active -> paused -> active -> ...
// -> completed, or cancelled from any open state.
const (
	StatusActive    = "active"
	StatusPaused    = "paused"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

// Source distinguishes a session started in-app from one a future provider
// sync (Strava, etc.) writes in directly. Only 'manual' is produced today.
const (
	SourceManual = "manual"
	SourceStrava = "strava"
)

// MET is a Compendium-of-Physical-Activities-style metabolic equivalent
// value: calories burned per kg of body weight per hour is MET * 1.
type MET struct {
	Code     string
	Name     string
	Category string
	Value    float64
}

// METTable is a small, curated reference list rather than a database table:
// it needs no joins, and a code review can see the whole list at once. Grows
// by adding a line here, no migration needed.
var METTable = []MET{
	{"walking_slow", "Walking (slow pace)", "cardio", 2.8},
	{"walking_moderate", "Walking (moderate pace)", "cardio", 3.5},
	{"walking_brisk", "Walking (brisk pace)", "cardio", 4.3},
	{"hiking", "Hiking", "cardio", 6.0},
	{"running_8kmh", "Running (8 km/h)", "cardio", 8.3},
	{"running_9_8kmh", "Running (9.8 km/h)", "cardio", 9.8},
	{"running_11_3kmh", "Running (11.3 km/h)", "cardio", 11.0},
	{"running_fast", "Running (fast, >13 km/h)", "cardio", 12.8},
	{"cycling_leisure", "Cycling (leisure)", "cardio", 4.0},
	{"cycling_moderate", "Cycling (moderate, 16-19 km/h)", "cardio", 8.0},
	{"cycling_vigorous", "Cycling (vigorous, 19-22 km/h)", "cardio", 10.0},
	{"swimming_leisure", "Swimming (leisure)", "cardio", 6.0},
	{"swimming_moderate", "Swimming (moderate effort)", "cardio", 8.3},
	{"swimming_vigorous", "Swimming (vigorous effort)", "cardio", 9.8},
	{"rowing_moderate", "Rowing machine (moderate)", "cardio", 7.0},
	{"rowing_vigorous", "Rowing machine (vigorous)", "cardio", 8.5},
	{"elliptical", "Elliptical trainer", "cardio", 5.0},
	{"stair_climbing", "Stair climbing", "cardio", 8.8},
	{"jump_rope", "Jump rope", "cardio", 11.8},
	{"hiit", "HIIT circuit", "cardio", 8.0},
	{"dancing", "Dancing (general)", "cardio", 5.5},
	{"strength_training_light", "Strength training (light)", "strength", 3.5},
	{"strength_training", "Strength training (general)", "strength", 5.0},
	{"strength_training_vigorous", "Strength training (vigorous)", "strength", 6.0},
	{"calisthenics", "Calisthenics (bodyweight)", "strength", 4.0},
	{"yoga", "Yoga", "flexibility", 2.5},
	{"pilates", "Pilates", "flexibility", 3.0},
	{"stretching", "Stretching", "flexibility", 2.3},
	{"basketball", "Basketball (game)", "sport", 8.0},
	{"soccer", "Soccer (casual)", "sport", 7.0},
	{"tennis", "Tennis (singles)", "sport", 8.0},
	{"golf", "Golf (walking, carrying clubs)", "sport", 4.8},
	{"boxing", "Boxing (training)", "sport", 9.0},
	{"martial_arts", "Martial arts", "sport", 10.3},
	{"climbing", "Rock climbing", "sport", 8.0},
	{"skiing", "Downhill skiing", "sport", 6.0},
}

// LookupMET finds a MET entry by code.
func LookupMET(code string) (MET, bool) {
	for _, m := range METTable {
		if m.Code == code {
			return m, true
		}
	}
	return MET{}, false
}

// METCodes is the ordered set of valid activity codes, for validation and the
// UI's select list.
func METCodes() []string {
	codes := make([]string, len(METTable))
	for i, m := range METTable {
		codes[i] = m.Code
	}
	return codes
}

// Session is one tracked bout of exercise.
type Session struct {
	ID     uuid.UUID
	UserID uuid.UUID

	ActivityCode string
	Source       string
	Status       string

	// WeightKgSnapshot is captured at Start, so a later weight change never
	// rewrites an already-completed session's calorie burn.
	WeightKgSnapshot float64

	StartedAt          time.Time
	PausedAt           *time.Time
	TotalPausedSeconds int
	EndedAt            *time.Time
	CaloriesBurned     *float64

	ExternalID *string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Elapsed is the active exercise time, excluding any paused time, as of at.
// For a paused session it freezes at PausedAt; for a completed session it
// freezes at EndedAt; for an active session it counts up to at.
func (s Session) Elapsed(at time.Time) time.Duration {
	end := at
	if s.EndedAt != nil {
		end = *s.EndedAt
	} else if s.Status == StatusPaused && s.PausedAt != nil {
		end = *s.PausedAt
	}

	elapsed := end.Sub(s.StartedAt) - time.Duration(s.TotalPausedSeconds)*time.Second
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

// IsOpen reports whether the session is still active or paused, i.e. not yet
// finished one way or another.
func (s Session) IsOpen() bool {
	return s.Status == StatusActive || s.Status == StatusPaused
}
