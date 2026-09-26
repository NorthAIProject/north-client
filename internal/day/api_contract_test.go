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
	energy, daylight, screen, quality := 32, 84, 248, 4
	weight, height, bmi, target := 83.9, 181.0, 25.6, 80.0
	start, end := at(-1, 0), at(7, 13)

	apitest.AssertGolden(t, "day.golden.json", day.Project(day.Snapshot{
		Date: date, Now: at(14, 46), IsToday: true,
		EnergyPercent: &energy, DaylightMinutes: &daylight, ScreenMinutes: &screen,
		Level:    21,
		Caffeine: dayd.Caffeine{TotalMG: 172, ActiveMG: 72, LimitMG: 400},
		Fast: &dayd.Fast{
			StartedAt: at(2, 0), TargetHours: 16, Elapsed: 12*time.Hour + 46*time.Minute, Phase: "fat_burning", Fraction: 0.8,
		},
		Nutrients:  dayd.Nutrients{Covered: []string{"omega3"}, Missing: []string{"vitamin_a", "vitamin_c"}},
		Milestones: []dayd.Milestone{{ID: "77777777-7777-7777-7777-777777777777", Name: "Dentist", MonthsSince: 4, Fraction: 0.67}},
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
		Body: dayd.Body{
			WeightKg: &weight, HeightCm: &height, BMI: &bmi, TargetWeightKg: &target,
			BloodPressure: &dayd.BloodPressure{Systolic: 122, Diastolic: 79, At: at(8, 0)},
			Soreness:      []dayd.Soreness{{Region: "quads", Severity: 2}},
		},
		Streak: 91,
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
