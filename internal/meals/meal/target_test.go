package meal_test

import (
	"math"
	"testing"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
)

func floatEquals(a, b, eps float64) bool {
	return math.Abs(a-b) < eps
}

func TestCarbRangeAndDefaultPct(t *testing.T) {
	tests := []struct {
		planType   string
		wantMin    float64
		wantMax    float64
		wantDefPct float64
	}{
		{meal.PlanTypeNoCarb, 0.0, 0.05, 0.05},
		{meal.PlanTypeLowCarb, 0.06, 0.25, 0.20},
		{meal.PlanTypeMidCarb, 0.26, 0.45, 0.35},
		{meal.PlanTypeHighCarb, 0.46, 0.65, 0.55},
		{meal.PlanTypeCustom, 0.0, 1.0, 1.0},
	}

	for _, tt := range tests {
		min, max := meal.CarbRange(tt.planType)
		if !floatEquals(min, tt.wantMin, 1e-4) || !floatEquals(max, tt.wantMax, 1e-4) {
			t.Errorf("CarbRange(%q) = (%v, %v), want (%v, %v)", tt.planType, min, max, tt.wantMin, tt.wantMax)
		}
		def := meal.DefaultCarbPct(tt.planType)
		if !floatEquals(def, tt.wantDefPct, 1e-4) {
			t.Errorf("DefaultCarbPct(%q) = %v, want %v", tt.planType, def, tt.wantDefPct)
		}
	}
}

func TestResolveDayTarget(t *testing.T) {
	baseProtein := 160.0
	baseFat := 70.0
	baseCarb := 200.0

	t.Run("default plan carb types", func(t *testing.T) {
		// Low carb: default 20% of 200g = 40g carb
		target := meal.ResolveDayTarget(baseProtein, baseFat, baseCarb, meal.PlanTypeLowCarb, nil, "", nil, nil, nil)
		if !floatEquals(target.ProteinG, 160.0, 1e-4) {
			t.Errorf("expected protein 160, got %v", target.ProteinG)
		}
		if !floatEquals(target.FatG, 70.0, 1e-4) {
			t.Errorf("expected fat 70, got %v", target.FatG)
		}
		if !floatEquals(target.CarbG, 40.0, 1e-4) {
			t.Errorf("expected carb 40, got %v", target.CarbG)
		}
		wantCal := 160.0*4 + 70.0*9 + 40.0*4 // 640 + 630 + 160 = 1430
		if !floatEquals(target.Calories, wantCal, 1e-4) {
			t.Errorf("expected calories %v, got %v", wantCal, target.Calories)
		}
	})

	t.Run("custom plan carb pct", func(t *testing.T) {
		customPct := 30.0 // 30% of 200g = 60g carb
		target := meal.ResolveDayTarget(baseProtein, baseFat, baseCarb, meal.PlanTypeCustom, &customPct, "", nil, nil, nil)
		if !floatEquals(target.CarbG, 60.0, 1e-4) {
			t.Errorf("expected carb 60, got %v", target.CarbG)
		}
		if !floatEquals(target.ProteinG, 160.0, 1e-4) {
			t.Errorf("expected protein 160, got %v", target.ProteinG)
		}
	})

	t.Run("day plan type overrides plan default", func(t *testing.T) {
		// Plan is low carb, but this day is high carb (55% of 200g = 110g)
		target := meal.ResolveDayTarget(baseProtein, baseFat, baseCarb, meal.PlanTypeLowCarb, nil, meal.PlanTypeHighCarb, nil, nil, nil)
		if !floatEquals(target.CarbG, 110.0, 1e-4) {
			t.Errorf("expected carb 110, got %v", target.CarbG)
		}
		if !floatEquals(target.ProteinG, 160.0, 1e-4) {
			t.Errorf("expected protein unchanged (160), got %v", target.ProteinG)
		}
	})

	t.Run("day custom grams override all", func(t *testing.T) {
		customCarbG := 15.0
		customProteinG := 180.0
		customFatG := 60.0
		target := meal.ResolveDayTarget(
			baseProtein, baseFat, baseCarb,
			meal.PlanTypeLowCarb, nil,
			meal.PlanTypeHighCarb,
			&customCarbG, &customProteinG, &customFatG,
		)
		if !floatEquals(target.CarbG, 15.0, 1e-4) {
			t.Errorf("expected carb 15, got %v", target.CarbG)
		}
		if !floatEquals(target.ProteinG, 180.0, 1e-4) {
			t.Errorf("expected protein 180, got %v", target.ProteinG)
		}
		if !floatEquals(target.FatG, 60.0, 1e-4) {
			t.Errorf("expected fat 60, got %v", target.FatG)
		}
	})
}

func TestCheckOverage(t *testing.T) {
	target := meal.DayTarget{
		Calories: 1500,
		ProteinG: 150,
		FatG:     50,
		CarbG:    100,
	}

	t.Run("within limits", func(t *testing.T) {
		consumed := meal.Macros{
			Calories: 1400,
			ProteinG: 140,
			FatG:     45,
			CarbG:    95,
		}
		overage := meal.CheckOverage(consumed, target)
		if overage.IsOver {
			t.Errorf("expected IsOver to be false, got true: %+v", overage)
		}
	})

	t.Run("exceeding carb limit", func(t *testing.T) {
		consumed := meal.Macros{
			Calories: 1550,
			ProteinG: 140,
			FatG:     45,
			CarbG:    110, // 10g over
		}
		overage := meal.CheckOverage(consumed, target)
		if !overage.IsOver {
			t.Errorf("expected IsOver to be true")
		}
		if !floatEquals(overage.CarbG, 10.0, 1e-4) {
			t.Errorf("expected CarbG overage 10, got %v", overage.CarbG)
		}
	})

	t.Run("exceeding protein limit", func(t *testing.T) {
		consumed := meal.Macros{
			Calories: 1550,
			ProteinG: 165, // 15g over
			FatG:     45,
			CarbG:    90,
		}
		overage := meal.CheckOverage(consumed, target)
		if !overage.IsOver {
			t.Errorf("expected IsOver to be true")
		}
		if !floatEquals(overage.ProteinG, 15.0, 1e-4) {
			t.Errorf("expected ProteinG overage 15, got %v", overage.ProteinG)
		}
	})

	t.Run("exceeding fat limit", func(t *testing.T) {
		consumed := meal.Macros{
			Calories: 1550,
			ProteinG: 140,
			FatG:     60, // 10g over
			CarbG:    90,
		}
		overage := meal.CheckOverage(consumed, target)
		if !overage.IsOver {
			t.Errorf("expected IsOver to be true")
		}
		if !floatEquals(overage.FatG, 10.0, 1e-4) {
			t.Errorf("expected FatG overage 10, got %v", overage.FatG)
		}
	})
}
