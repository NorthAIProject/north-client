package fitness

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestFitnessShapes(t *testing.T) {
	t.Parallel()

	synced := time.Date(2026, 9, 24, 6, 30, 0, 0, time.UTC)
	apitest.AssertGolden(t, "strava-status.golden.json", StravaStatus{
		Configured: true, Connected: true, LastSyncedAt: &synced, LastSyncAttemptedAt: &synced,
	})
	apitest.AssertGolden(t, "strava-connect.golden.json", StravaConnect{
		AuthorizeURL: "https://www.strava.com/oauth/authorize?client_id=123&state=abc",
	})
	apitest.AssertGolden(t, "strava-connect-result.golden.json", StravaConnectResult{Result: "connected"})

	planID := uuid.MustParse("abababab-abab-abab-abab-abababababab")
	delta := -0.4
	apitest.AssertGolden(t, "fitness-hub.golden.json", FitnessHub{
		Strava: StravaStatus{Configured: true, Connected: true, LastSyncedAt: &synced},
		Week: HubWeek{
			Days:     []HubWeekDay{{Date: "2026-09-23", Minutes: 0}, {Date: "2026-09-24", Minutes: 45, IsToday: true}},
			Sessions: 1, Minutes: 45, DistanceKm: 8.2, Calories: 512,
		},
		Recent: []HubSession{{
			ActivityCode: "running_9_8kmh", Name: "Running (9.8 km/h)", Category: "running", Source: "strava",
			EndedAt: synced, DurationSeconds: 2700, DistanceKm: 8.2, Calories: 512,
		}},
		Steps:       []HubDayValue{{Date: "2026-09-24", Value: 9120}},
		VO2:         []HubDayValue{},
		Weight:      &HubWeight{Kg: 78.4, DeltaKg: &delta, At: synced},
		Strength:    &HubStrength{Workouts: 6, Sets: 72, VolumeKg: 18400, Exercises: []HubStrengthRow{{Name: "Back squat", BestKg: 100, E1RMKg: 116.7, ChangeKg: 5}}, Record: &HubStrengthRecord{Exercise: "Back squat", WeightKg: 100, Reps: 5, E1RMKg: 116.7, On: "2026-09-22"}},
		Cardio:      &HubCardio{Sessions: 4, Minutes: 180, DistanceKm: 30.5, Runs: 3, BestPaceSeconds: 320, Best5KSeconds: 1610, LongestKm: 12, RestingHR: 52},
		PlanID:      &planID,
		NextSession: &HubPlanDay{Weekday: "Friday", StartTime: "07:30", Focus: "Pull", ExerciseCount: 5},
		DoneToday:   &HubPlanDay{Weekday: "Wednesday", Focus: "Push", ExerciseCount: 6},
		Meals: &HubMeals{
			Logged: HubMacros{Calories: 1450, ProteinG: 110, CarbG: 150, FatG: 45},
			Goal:   HubMacros{Calories: 2300, ProteinG: 160, CarbG: 250, FatG: 70},
		},
		DeviceReadings: []string{"Resting heart rate averaged 52 bpm."},
	})

	apitest.AssertGolden(t, "fitness-activities.golden.json", FitnessActivities{
		Strava: StravaStatus{Configured: true, Connected: true},
		Trend: &TrainingTrend{
			Days:   []TrendDayView{{Date: "2026-09-23", Minutes: 0}, {Date: "2026-09-24", Minutes: 45}, {Date: "2026-09-25", Future: true}},
			Bands:  []TrendBandView{{Family: "run", Values: []float64{0, 45, 0}}},
			Normal: []float64{12.5, 14, 14}, RecentMinutes: 45, NormalMinutes: 98, Ratio: 0.46, StreakWeeks: 3, Shape: "well_down",
			Sessions: 1, DistanceM: 8200, ElevationM: 64, ActiveDays: 1, CountedDays: 2,
		},
		Sessions: &StravaSessionPage{
			Sessions: []StravaSessionView{{StravaID: 1234567, Name: "Morning Run", Sport: "Run", Family: "run", DistanceM: 8200, ElevationM: 64, MovingTimeS: 2700, StartedAt: &synced}},
			Page:     1, PerPage: 10, TotalPages: 1, Total: 1,
		},
	})
}
