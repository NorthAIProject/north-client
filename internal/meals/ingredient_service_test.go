package meals_test

import (
	"context"
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

	fetched, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Name != "Chicken breast" {
		t.Fatalf("name = %q", fetched.Name)
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
