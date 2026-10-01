package meals_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func validIngredient() meals.IngredientInput {
	return meals.IngredientInput{
		Name:     "Chicken breast",
		Category: meals.CategoryProtein,
		Per100g:  meals.Macros{Calories: 165, ProteinG: 31, FatG: 3.6, CarbG: 0},
	}
}

func TestCreateAndGetIngredient(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "fernando@north.test")
	svc := meals.NewIngredientService(meals.NewRepository(pool))
	ctx := context.Background()

	created, err := svc.Create(ctx, user.ID, validIngredient())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ServingSizeGrams != 100 {
		t.Fatalf("serving size default = %v, want 100", created.ServingSizeGrams)
	}

	fetched, err := svc.Get(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Name != "Chicken breast" {
		t.Fatalf("name = %q", fetched.Name)
	}

	// A private ingredient belongs to the account that made it. Reading one by
	// id used to skip that check, which let a hand-crafted ingredient_id on the
	// food-log, meal-plan and capture-commit forms copy somebody else's food —
	// its name and its whole macro profile — into the caller's own row.
	stranger := newUser(t, pool, "stranger@north.test")
	if _, err := svc.Get(ctx, created.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("another account read a private ingredient: err = %v, want ErrNotFound", err)
	}
}

func TestMacrosForScalesFromPer100g(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "fernando@north.test")
	svc := meals.NewIngredientService(meals.NewRepository(pool))

	created, err := svc.Create(context.Background(), user.ID, validIngredient())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	macros := created.MacrosFor(200) // double the 100g profile
	if macros.Calories != 330 {
		t.Fatalf("calories = %v, want 330", macros.Calories)
	}
	if macros.ProteinG != 62 {
		t.Fatalf("protein = %v, want 62", macros.ProteinG)
	}
}

func TestSearchExcludesAnotherUsersPrivateIngredient(t *testing.T) {
	pool := testdb.New(t)
	owner := newUser(t, pool, "owner@north.test")
	stranger := newUser(t, pool, "stranger@north.test")
	svc := meals.NewIngredientService(meals.NewRepository(pool))
	ctx := context.Background()

	created, err := svc.Create(ctx, owner.ID, validIngredient())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Asserted by identity rather than by result count: the shared catalog is
	// seeded by migration, so both users legitimately see a page of chicken.
	// Counting would only ever have worked against an empty table.
	strangerResults, err := svc.Search(ctx, stranger.ID, "chicken", 100)
	if err != nil {
		t.Fatalf("search as stranger: %v", err)
	}
	if containsIngredient(strangerResults, created.ID) {
		t.Fatal("a stranger can see another user's private ingredient")
	}
	for _, found := range strangerResults {
		if found.UserID != nil {
			t.Errorf("a stranger's results should be shared rows only, got one owned by %v", *found.UserID)
		}
	}

	// The owner still finds their own.
	ownerResults, err := svc.Search(ctx, owner.ID, "chicken", 100)
	if err != nil {
		t.Fatalf("search as owner: %v", err)
	}
	if !containsIngredient(ownerResults, created.ID) {
		t.Fatal("the owner cannot see their own ingredient")
	}
}

// A spoken or typed name rarely arrives in the catalog's word order, and a
// voice parse hands search "chicken breast" whether the row is called that or
// "Breast, chicken". Every word must appear; their order must not matter.
//
// The names carry a nonsense prefix because the shared catalog is seeded by
// migration: a real word would also match rows these assertions know nothing
// about.
func TestSearchMatchesEveryWordInAnyOrder(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "words@north.test")
	svc := meals.NewIngredientService(meals.NewRepository(pool))
	ctx := context.Background()

	create := func(name string) meals.Ingredient {
		t.Helper()
		in := validIngredient()
		in.Name = name
		created, err := svc.Create(ctx, user.ID, in)
		if err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		return created
	}
	breast := create("Zqx breast, roasted")
	thigh := create("Zqx thigh, roasted")

	found, err := svc.Search(ctx, user.ID, "Roasted  ZQX breast", 100)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !containsIngredient(found, breast.ID) {
		t.Error("words out of order did not find the ingredient that has them all")
	}
	if containsIngredient(found, thigh.ID) {
		t.Error("an ingredient missing one of the words was returned")
	}
}

// LIKE treats % and _ as wildcards. Unescaped, "50%" searched for "50" and
// "_" for any character at all.
func TestSearchTreatsWildcardsLiterally(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "wildcards@north.test")
	svc := meals.NewIngredientService(meals.NewRepository(pool))
	ctx := context.Background()

	for _, name := range []string{"Zqx chocolate 50% cocoa", "Zqx chocolate 500 bar"} {
		in := validIngredient()
		in.Name = name
		if _, err := svc.Create(ctx, user.ID, in); err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
	}

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{query: "zqx 50%", want: []string{"Zqx chocolate 50% cocoa"}},
		{query: "zqx _", want: nil},
		{query: "zqx chocolate", want: []string{"Zqx chocolate 50% cocoa", "Zqx chocolate 500 bar"}},
	} {
		found, err := svc.Search(ctx, user.ID, tc.query, 100)
		if err != nil {
			t.Fatalf("search %q: %v", tc.query, err)
		}
		// Sorted here rather than trusted from ORDER BY: where "50%" falls
		// against "500" is the database collation's call, not this test's.
		var got []string
		for _, f := range found {
			got = append(got, f.Name)
		}
		sort.Strings(got)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("search %q = %q, want %q", tc.query, got, tc.want)
		}
	}
}

// An empty box lists the catalog rather than nothing, as it always has.
func TestSearchWithEmptyQueryListsEverything(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "empty@north.test")
	svc := meals.NewIngredientService(meals.NewRepository(pool))

	found, err := svc.Search(context.Background(), user.ID, "   ", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("an empty query returned nothing; the seeded catalog should fill the page")
	}
}

func containsIngredient(found []meals.Ingredient, id uuid.UUID) bool {
	for _, ingredient := range found {
		if ingredient.ID == id {
			return true
		}
	}
	return false
}

func TestUpdateFailsAgainstAnotherUsersIngredient(t *testing.T) {
	pool := testdb.New(t)
	owner := newUser(t, pool, "owner@north.test")
	stranger := newUser(t, pool, "stranger@north.test")
	svc := meals.NewIngredientService(meals.NewRepository(pool))
	ctx := context.Background()

	created, err := svc.Create(ctx, owner.ID, validIngredient())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	in := validIngredient()
	in.Name = "Hijacked"
	if _, err := svc.Update(ctx, created.ID, stranger.ID, in); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestValidationRejectsMissingName(t *testing.T) {
	t.Parallel()

	in := validIngredient()
	in.Name = ""

	_, err := meals.ValidateIngredient(in)
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}
