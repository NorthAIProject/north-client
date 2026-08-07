package meals_test

import (
	"context"
	"testing"

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

	if _, err := svc.Create(ctx, owner.ID, validIngredient()); err != nil {
		t.Fatalf("create: %v", err)
	}

	// A user-created ingredient (non-null user_id) is private to its owner.
	strangerResults, err := svc.Search(ctx, stranger.ID, "chicken", 10)
	if err != nil {
		t.Fatalf("search as stranger: %v", err)
	}
	if len(strangerResults) != 0 {
		t.Fatalf("a stranger should not see another user's ingredient, got %d", len(strangerResults))
	}

	// The owner still finds their own.
	ownerResults, err := svc.Search(ctx, owner.ID, "chicken", 10)
	if err != nil {
		t.Fatalf("search as owner: %v", err)
	}
	if len(ownerResults) != 1 {
		t.Fatalf("the owner should see their own ingredient, got %d", len(ownerResults))
	}
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
