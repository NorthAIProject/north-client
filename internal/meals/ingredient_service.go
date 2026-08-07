package meals

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type IngredientService struct {
	repo *Repository
}

func NewIngredientService(repo *Repository) *IngredientService {
	return &IngredientService{repo: repo}
}

// IngredientInput is an ingredient as submitted, its macros always per 100g.
type IngredientInput struct {
	Name             string
	Brand            string
	Category         string
	ServingSizeGrams float64
	Per100g          Macros

	FiberGPer100g        float64
	SugarGPer100g        float64
	SodiumMgPer100g      float64
	PotassiumMgPer100g   float64
	CholesterolMgPer100g float64
}

func ValidateIngredient(in IngredientInput) (IngredientInput, error) {
	var errs apperr.FieldErrors

	in.Name = strings.TrimSpace(in.Name)
	switch {
	case in.Name == "":
		errs = errs.Add("name", "Give the ingredient a name.")
	case len(in.Name) > 200:
		errs = errs.Add("name", "Keep the name under 200 characters.")
	}

	in.Category = strings.TrimSpace(in.Category)
	if in.Category == "" {
		in.Category = CategoryOther
	} else if !slices.Contains(Categories, in.Category) {
		errs = errs.Add("category", "Choose one of the listed categories.")
	}

	if in.ServingSizeGrams <= 0 {
		in.ServingSizeGrams = 100
	}

	if in.Per100g.Calories < 0 {
		errs = errs.Add("calories_per_100g", "Calories cannot be negative.")
	}

	return in, errs.OrNil()
}

func (s *IngredientService) Create(ctx context.Context, userID uuid.UUID, in IngredientInput) (Ingredient, error) {
	clean, err := ValidateIngredient(in)
	if err != nil {
		return Ingredient{}, err
	}
	return s.repo.CreateIngredient(ctx, userID, toIngredient(clean))
}

func (s *IngredientService) Get(ctx context.Context, id uuid.UUID) (Ingredient, error) {
	return s.repo.GetIngredient(ctx, id)
}

// Search returns the shared/global set plus the user's own.
func (s *IngredientService) Search(ctx context.Context, userID uuid.UUID, query string, limit int) ([]Ingredient, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	return s.repo.SearchIngredients(ctx, userID, strings.TrimSpace(query), limit)
}

func (s *IngredientService) Update(ctx context.Context, id, userID uuid.UUID, in IngredientInput) (Ingredient, error) {
	clean, err := ValidateIngredient(in)
	if err != nil {
		return Ingredient{}, err
	}
	return s.repo.UpdateIngredient(ctx, id, userID, toIngredient(clean))
}

func (s *IngredientService) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.DeleteIngredient(ctx, id, userID)
}

func toIngredient(in IngredientInput) Ingredient {
	return Ingredient{
		Name: in.Name, Brand: in.Brand, Category: in.Category,
		ServingSizeGrams: in.ServingSizeGrams, Per100g: in.Per100g,
		FiberGPer100g: in.FiberGPer100g, SugarGPer100g: in.SugarGPer100g,
		SodiumMgPer100g: in.SodiumMgPer100g, PotassiumMgPer100g: in.PotassiumMgPer100g,
		CholesterolMgPer100g: in.CholesterolMgPer100g,
	}
}
