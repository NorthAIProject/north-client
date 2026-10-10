package planimport

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/meals"
)

// A dietitian's plan counts in kitchen measures — slices, cups, spoons,
// pieces — far more often than in grams. When the reader gives no estimate,
// each of those still has to become a weight, flagged, or the line is lost.
func TestKitchenMeasuresBecomeEstimatedWeights(t *testing.T) {
	cases := []struct {
		qty        float64
		unit       string
		ingredient *meals.Ingredient
		want       float64
	}{
		{2, "fatias", nil, 60},
		{1, "chávena", nil, 240},
		{2, "colheres de sopa", nil, 30},
		{1, "colher de sobremesa", nil, 10},
		{1, "peça", nil, 120},
		{1, "iogurte", nil, 125},
		{1, "lata", nil, 120},
		{1, "punhado", nil, 30},
		{3, "tortitas", nil, 24},
		{2, "Slices", nil, 60},
		{1, "cup", nil, 240},
		// The catalog's own serving beats a typical weight.
		{2, "fatias", &meals.Ingredient{ServingSizeGrams: 40}, 80},
	}
	for _, c := range cases {
		qty := c.qty
		f := &FoodDraft{Food: "x", Quantity: &qty, Unit: c.unit}
		got := (*Service)(nil).derivedGrams(f, c.ingredient)
		if got == nil || *got != c.want {
			t.Errorf("%g %s: grams = %v, want %g", c.qty, c.unit, got, c.want)
			continue
		}
		if !f.Estimated || len(f.Flags) == 0 {
			t.Errorf("%g %s: estimated = %v, flags = %v; want an estimated, flagged weight", c.qty, c.unit, f.Estimated, f.Flags)
		}
	}
}

func TestGramsStayExactAndUnknownUnitsStayEmpty(t *testing.T) {
	qty := 150.0
	f := &FoodDraft{Food: "rice", Quantity: &qty, Unit: "g"}
	if got := (*Service)(nil).derivedGrams(f, nil); got == nil || *got != 150 || f.Estimated {
		t.Errorf("150 g: grams = %v, estimated = %v; want exactly 150, not estimated", got, f.Estimated)
	}
	f = &FoodDraft{Food: "x", Quantity: &qty, Unit: "bananas worth"}
	if got := (*Service)(nil).derivedGrams(f, nil); got != nil {
		t.Errorf("unknown unit: grams = %v, want none", *got)
	}
}
