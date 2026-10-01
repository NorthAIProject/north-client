package capture

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/capture/captured"
)

// The review's field names are a contract with the batch handler in
// internal/meals: line=N names a kept line, and its ingredient and grams are
// ingredient_id_N and quantity_grams_N. Nothing else would notice them drift
// apart — the form would post, and add nothing.
func TestMealVoiceReviewPostsIndexedLines(t *testing.T) {
	meal := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	breast := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	var b strings.Builder
	err := MealVoice(MealVoiceData{
		MealID: meal.String(),
		Text:   "200 g chicken breast and some rice",
		Parsed: true,
		Draft: captured.FoodDraft{
			Lines: []captured.FoodLine{
				{Query: "chicken breast", Grams: 200, IngredientID: breast, MatchedName: "Chicken breast",
					Candidates: []captured.Candidate{{ID: breast, Name: "Chicken breast"}}},
				{Query: "rice", Grams: 100, Uncertain: true,
					Candidates: []captured.Candidate{{ID: uuid.New(), Name: "Rice, white"}, {ID: uuid.New(), Name: "Rice, brown"}}},
			},
			Unparsed: []string{"a glass of water"},
		},
	}).Render(context.Background(), &b)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	html := b.String()

	for _, want := range []string{
		`action="/app/nutrition/meals/` + meal.String() + `/ingredients/batch"`,
		`name="line"`,
		`value="0"`,
		`value="1"`,
		`name="ingredient_id_0"`,
		`name="quantity_grams_0"`,
		`name="ingredient_id_1"`,
		`value="200"`,
		"estimated",
		"a glass of water",
		"200 g chicken breast and some rice",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("review does not contain %s", want)
		}
	}
}
