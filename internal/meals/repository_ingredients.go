package meals

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mealsdb "github.com/NorthAIProject/north-client/internal/meals/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func (r *Repository) CreateIngredient(ctx context.Context, userID uuid.UUID, in Ingredient) (Ingredient, error) {
	row, err := r.q.CreateIngredient(ctx, mealsdb.CreateIngredientParams{
		UserID:               &userID,
		Name:                 in.Name,
		Brand:                in.Brand,
		Category:             in.Category,
		ServingSizeGrams:     in.ServingSizeGrams,
		CaloriesPer100g:      in.Per100g.Calories,
		ProteinGPer100g:      in.Per100g.ProteinG,
		FatGPer100g:          in.Per100g.FatG,
		CarbsGPer100g:        in.Per100g.CarbG,
		FiberGPer100g:        in.FiberGPer100g,
		SugarGPer100g:        in.SugarGPer100g,
		SodiumMgPer100g:      in.SodiumMgPer100g,
		PotassiumMgPer100g:   in.PotassiumMgPer100g,
		CholesterolMgPer100g: in.CholesterolMgPer100g,
	})
	if err != nil {
		return Ingredient{}, apperr.Wrap(err, "create ingredient")
	}
	return ingredientFromDB(row), nil
}

func (r *Repository) GetIngredient(ctx context.Context, id uuid.UUID) (Ingredient, error) {
	row, err := r.q.GetIngredient(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Ingredient{}, apperr.ErrNotFound
		}
		return Ingredient{}, apperr.Wrap(err, "get ingredient")
	}
	return ingredientFromDB(row), nil
}

// SearchIngredients returns the shared/global set plus the user's own,
// matching a case-insensitive substring of name.
func (r *Repository) SearchIngredients(ctx context.Context, userID uuid.UUID, query string, limit int) ([]Ingredient, error) {
	rows, err := r.q.SearchIngredients(ctx, mealsdb.SearchIngredientsParams{
		UserID: &userID,
		Lower:  "%" + query + "%",
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "search ingredients")
	}

	out := make([]Ingredient, 0, len(rows))
	for _, row := range rows {
		out = append(out, ingredientFromDB(row))
	}
	return out, nil
}

// UpdateIngredient only succeeds against an ingredient the user owns; shared
// ingredients are read-only to everyone.
func (r *Repository) UpdateIngredient(ctx context.Context, id, userID uuid.UUID, in Ingredient) (Ingredient, error) {
	row, err := r.q.UpdateIngredient(ctx, mealsdb.UpdateIngredientParams{
		ID:                   id,
		UserID:               &userID,
		Name:                 in.Name,
		Brand:                in.Brand,
		Category:             in.Category,
		ServingSizeGrams:     in.ServingSizeGrams,
		CaloriesPer100g:      in.Per100g.Calories,
		ProteinGPer100g:      in.Per100g.ProteinG,
		FatGPer100g:          in.Per100g.FatG,
		CarbsGPer100g:        in.Per100g.CarbG,
		FiberGPer100g:        in.FiberGPer100g,
		SugarGPer100g:        in.SugarGPer100g,
		SodiumMgPer100g:      in.SodiumMgPer100g,
		PotassiumMgPer100g:   in.PotassiumMgPer100g,
		CholesterolMgPer100g: in.CholesterolMgPer100g,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Ingredient{}, apperr.ErrNotFound
		}
		return Ingredient{}, apperr.Wrap(err, "update ingredient")
	}
	return ingredientFromDB(row), nil
}

func (r *Repository) DeleteIngredient(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.DeleteIngredient(ctx, mealsdb.DeleteIngredientParams{ID: id, UserID: &userID}), "delete ingredient")
}

func ingredientFromDB(row mealsdb.Ingredient) Ingredient {
	return Ingredient{
		ID:                   row.ID,
		UserID:               row.UserID,
		Name:                 row.Name,
		Brand:                row.Brand,
		Category:             row.Category,
		ServingSizeGrams:     row.ServingSizeGrams,
		Per100g:              Macros{Calories: row.CaloriesPer100g, ProteinG: row.ProteinGPer100g, FatG: row.FatGPer100g, CarbG: row.CarbsGPer100g},
		FiberGPer100g:        row.FiberGPer100g,
		SugarGPer100g:        row.SugarGPer100g,
		SodiumMgPer100g:      row.SodiumMgPer100g,
		PotassiumMgPer100g:   row.PotassiumMgPer100g,
		CholesterolMgPer100g: row.CholesterolMgPer100g,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}
