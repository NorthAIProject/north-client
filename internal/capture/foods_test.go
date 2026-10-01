package capture_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/meals"
)

func food(source, query string, grams float64) capture.Item {
	return capture.Item{Kind: capture.KindFood, Source: source, Food: &capture.Food{Query: query, Grams: grams}}
}

// The names carry a nonsense prefix because the shared catalog is seeded by
// migration: "chicken" alone would also find rows this test knows nothing
// about, and the ambiguity it asserts would be the seed data's, not its own.
func (f *fixture) addIngredient(t *testing.T, name string) meals.Ingredient {
	t.Helper()
	created, err := meals.NewIngredientService(meals.NewRepository(f.pool)).Create(context.Background(), f.user.ID, meals.IngredientInput{
		Name:     name,
		Category: meals.CategoryProtein,
		Per100g:  meals.Macros{Calories: 100},
	})
	if err != nil {
		t.Fatalf("create ingredient %q: %v", name, err)
	}
	return created
}

func TestParseFoodsOffersIngredientsForEachPortion(t *testing.T) {
	f := newFixture(t)
	breast := f.addIngredient(t, "Zqx chicken breast")
	f.addIngredient(t, "Zqx rice, white")
	f.addIngredient(t, "Zqx rice, brown")

	eggs := food("two zqx eggs", "zqx egg", 120)
	eggs.Uncertain = true
	f.parser.draft = capture.Draft{
		Items: []capture.Item{
			food("200 grams of zqx chicken breast", "zqx chicken breast", 200),
			food("100 grams of zqx rice", "zqx rice", 100),
			eggs,
			water(250),
		},
		Unparsed: []string{"and then a nap"},
	}

	got, err := f.svc.ParseFoods(context.Background(), f.user, "anything")
	if err != nil {
		t.Fatalf("parse foods: %v", err)
	}
	if f.parser.sawHabits != nil {
		t.Errorf("a meal parse was offered %d habits; it has no use for them", len(f.parser.sawHabits))
	}
	if len(got.Lines) != 3 {
		t.Fatalf("got %d lines, want 3 (water is not a portion): %+v", len(got.Lines), got.Lines)
	}

	t.Run("a lone match is pre-selected", func(t *testing.T) {
		line := got.Lines[0]
		if line.IngredientID != breast.ID || line.MatchedName != breast.Name {
			t.Errorf("match = %v %q, want %v %q", line.IngredientID, line.MatchedName, breast.ID, breast.Name)
		}
		if line.Grams != 200 || line.Problem != "" {
			t.Errorf("line = %+v", line)
		}
	})

	t.Run("an ambiguous name offers every candidate and chooses none", func(t *testing.T) {
		line := got.Lines[1]
		if line.IngredientID != uuid.Nil {
			t.Errorf("an ambiguous name was resolved to %q", line.MatchedName)
		}
		if len(line.Candidates) != 2 {
			t.Errorf("candidates = %+v, want both rices", line.Candidates)
		}
		if line.Problem != "" {
			t.Errorf("choosing between two rices is a choice, not a problem: %q", line.Problem)
		}
	})

	t.Run("a name with no match says so", func(t *testing.T) {
		line := got.Lines[2]
		if line.Problem == "" || line.IngredientID != uuid.Nil || len(line.Candidates) != 0 {
			t.Errorf("line = %+v", line)
		}
		if !line.Uncertain {
			t.Error("an estimated quantity lost its uncertainty on the way through")
		}
	})

	t.Run("anything that is not food is unparsed, after what the parser left", func(t *testing.T) {
		if len(got.Unparsed) != 2 || got.Unparsed[0] != "and then a nap" || got.Unparsed[1] != "water" {
			t.Errorf("unparsed = %q", got.Unparsed)
		}
	})
}

// Empty is an answer, not an error: silence or a sentence with no food in it
// parses to no lines, and the page says so.
func TestParseFoodsWithNothingToEatIsEmptyNotNil(t *testing.T) {
	f := newFixture(t)
	f.parser.draft = capture.Draft{}

	got, err := f.svc.ParseFoods(context.Background(), f.user, "hello")
	if err != nil {
		t.Fatalf("parse foods: %v", err)
	}
	if got.Lines == nil {
		t.Fatal("lines is nil; it encodes as null and every client has to guard it")
	}
}
