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

	// Ported from the FitMe project's activity list, which stores calories per
	// hour rather than METs. Converted by dividing by the 70kg reference body
	// weight its figures are built on, then rounded to one decimal.
	//
	// Where the two lists overlapped they agreed to within a few percent —
	// downhill skiing 6.2 against 6.0, golf 4.7 against 4.8, martial arts 10.4
	// against 10.3 — so the entries above were kept and the duplicates
	// dropped. The one real disagreement was hatha yoga, which FitMe puts at
	// 4.1 against the Compendium's 2.5; the existing value stands.
	{"cycling_stationary_very_light", "Stationary cycling (very light)", "cardio", 3.1},
	{"cycling_stationary_light", "Stationary cycling (light)", "cardio", 5.7},
	{"cycling_stationary_moderate", "Stationary cycling (moderate)", "cardio", 7.3},
	{"cycling_stationary_vigorous", "Stationary cycling (vigorous)", "cardio", 10.9},
	{"cycling_stationary_very_vigorous", "Stationary cycling (very vigorous)", "cardio", 13.0},
	{"cycling_mountain", "Mountain biking / BMX", "cardio", 8.8},
	{"unicycling", "Unicycling", "cardio", 5.2},
	{"race_walking", "Race walking", "cardio", 6.7},
	{"walking_with_children", "Walking with children or a stroller", "cardio", 2.6},
	{"step_aerobics", "Step aerobics", "cardio", 8.8},
	{"water_aerobics", "Water aerobics", "cardio", 4.1},
	{"water_jogging", "Water jogging", "cardio", 8.3},
	{"ballroom_dancing_slow", "Ballroom dancing (slow)", "cardio", 3.1},
	{"ballroom_dancing_fast", "Ballroom dancing (fast)", "cardio", 5.7},

	{"canoeing_light", "Canoeing or rowing (light)", "sport", 3.1},
	{"canoeing_moderate", "Canoeing or rowing (moderate)", "sport", 7.3},
	{"canoeing_vigorous", "Canoeing or rowing (vigorous)", "sport", 12.4},
	{"sculling_competition", "Sculling or crew (competition)", "sport", 12.4},
	{"kayaking_whitewater", "Whitewater kayaking or rafting", "sport", 5.2},
	{"sailing", "Sailing or windsurfing", "sport", 3.1},
	{"surfing", "Surfing", "sport", 3.1},
	{"water_skiing", "Water skiing", "sport", 6.2},

	{"skiing_downhill_light", "Downhill skiing (light)", "sport", 5.2},
	{"skiing_downhill_racing", "Downhill skiing (racing)", "sport", 8.3},
	{"skiing_cross_country_slow", "Cross-country skiing (slow)", "sport", 7.3},
	{"skiing_cross_country_moderate", "Cross-country skiing (moderate)", "sport", 8.3},
	{"skiing_cross_country_vigorous", "Cross-country skiing (vigorous)", "sport", 9.3},
	{"skiing_cross_country_racing", "Cross-country skiing (racing)", "sport", 14.5},

	{"ice_hockey", "Ice hockey", "sport", 8.3},
	{"kickboxing", "Kickboxing", "sport", 10.4},
	{"table_tennis", "Table tennis", "sport", 4.1},
	{"volleyball", "Volleyball", "sport", 3.1},
	{"water_volleyball", "Water volleyball", "sport", 3.1},
	{"basketball_shooting", "Basketball (shooting baskets)", "sport", 4.7},
	{"baseball_softball", "Baseball or softball", "sport", 5.2},
	{"playing_catch", "Playing catch", "sport", 2.6},
	{"golf_pulling_clubs", "Golf (walking, pulling clubs)", "sport", 4.5},
	{"horseback_riding", "Horseback riding (walking)", "sport", 2.6},
	{"coaching_sports", "Coaching a team sport", "sport", 4.1},
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
