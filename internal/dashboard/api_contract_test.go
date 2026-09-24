package dashboard_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestTodayResponseShape(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 24, 7, 15, 0, 0, time.UTC)
	quality := 4
	progress := 40
	target := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	apitest.AssertGolden(t, "today.golden.json", dashboard.TodayResponse{
		User: auth.APIUser{
			ID:          uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			Email:       "ana@example.com",
			DisplayName: "Ana",
			Timezone:    "Europe/Lisbon",
		},
		Snapshot: dashboard.TodaySnapshot{
			Range:           "today",
			CheckedInToday:  true,
			Streak:          6,
			GoalActivity7d:  3,
			PendingMemories: 2,
			Goals: []dashboard.Goal{{
				ID:             uuid.MustParse("44444444-4444-4444-4444-444444444444"),
				Title:          "Run a half marathon",
				Category:       "fitness",
				Status:         "active",
				TargetDate:     &target,
				MilestoneTotal: 5,
				MilestoneDone:  2,
				Progress:       &progress,
			}},
			LastThread: &dashboard.Thread{
				ID:        uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				Title:     "Long run pacing",
				Kind:      "coach",
				UpdatedAt: at,
			},
			NextStep: &dashboard.NextStepResponse{
				Kind: "check_in", Eyebrow: "Today", Title: "How are you arriving?",
				Body: "A 20-second check-in keeps your streak.", CTA: "Check in", Href: "/app/check-ins",
			},
			Hydration:        dashboard.HydrationResponse{TodayML: 1250, TargetML: 2500, Percent: 50},
			Sleep:            dashboard.SleepResponse{Logged: true, DurationMinutes: 452, Quality: &quality},
			ActivityCalories: 312.5,
			Timeline: []dashboard.TimelineResponse{{
				Kind: "workout", Label: "Morning", At: at, Title: "Easy run", Detail: "6.2 km", Href: "/app/fitness", Icon: "footprints",
			}},
			Deltas: dashboard.DeltasResponse{
				Hydration:  dashboard.DeltaResponse{Pct: 12.5, Direction: 1, HasPrior: true},
				SleepHours: dashboard.DeltaResponse{Pct: -4, Direction: -1, HasPrior: true},
				Calories:   dashboard.DeltaResponse{},
				CheckIns:   dashboard.DeltaResponse{Pct: 0, Direction: 0, HasPrior: true},
			},
			Nudges: []dashboard.NudgeResponse{{
				ID:        uuid.MustParse("55555555-5555-5555-5555-555555555555"),
				Kind:      "missed_check_in",
				Title:     "Yesterday went unrecorded",
				Body:      "Want to add a quick note?",
				Href:      "/app/check-ins",
				CreatedAt: at,
			}},
			Briefing: "Easy day. Hydrate before the evening session.",
		},
	})
}
