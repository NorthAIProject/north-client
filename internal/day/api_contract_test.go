package day_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/day"
	dayd "github.com/NorthAIProject/north-client/internal/day/day"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestDayResponseShape(t *testing.T) {
	t.Parallel()

	date := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	at := func(h, m int) time.Time { return date.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	energy, daylight, quality := 32, 84, 4
	weight, height, bmi := 83.9, 181.0, 25.6
	start, end := at(-1, 0), at(7, 13)

	apitest.AssertGolden(t, "day.golden.json", day.Project(day.Snapshot{
		Date: date, Now: at(14, 46), IsToday: true,
		EnergyPercent: &energy, DaylightMinutes: &daylight,
		Food: dayd.Food{
			Calories: 1750, ProteinG: 107, CarbG: 133, FatG: 80,
			HasGoal: true, CalorieGoal: 2200, ProteinGoalG: 160, CarbGoalG: 220, FatGoalG: 75,
		},
		Water: dayd.Water{TotalML: 1550, TargetML: 2500},
		Activity: dayd.ActivityRings{
			Move:     dayd.Ring{Value: 501, Goal: 500},
			Exercise: dayd.Ring{Value: 62, Goal: 30},
			Stand:    dayd.Ring{Value: 12, Goal: 12},
		},
		Sleep: &dayd.Sleep{
			TotalMinutes: 433, Start: &start, End: &end, Quality: &quality, Source: "apple_health",
			StageMinutes: map[dayd.SleepStage]int{dayd.StageDeep: 64, dayd.StageREM: 109, dayd.StageCore: 260, dayd.StageAwake: 38},
			Blocks:       []dayd.SleepBlock{{Stage: dayd.StageCore, Start: start, End: at(1, 0)}},
		},
		Workouts: dayd.Workouts{Count: 2, Minutes: 62, Calories: 410, Labels: []string{"Dog walk", "Morning run"}},
		Body:     dayd.Body{WeightKg: &weight, HeightCm: &height, BMI: &bmi},
		Streak:   91,
		Timeline: []dashboard.Entry{{
			Kind: dashboard.KindFood, At: at(15, 30), Title: "Chicken rice bowl", Detail: "650 kcal", Href: "/app/nutrition/log", Icon: "utensils",
		}},
		Markers: []dayd.Marker{{Kind: dayd.RuleKitchenCloses, At: at(20, 0)}},
	}))
}

func TestRulesResponseShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "day_rules.golden.json", day.ProjectRules([]dayd.Rule{
		{Kind: dayd.RuleKitchenCloses, At: "20:00", Enabled: true},
	}))
}
