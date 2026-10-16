package meals

import (
	"context"

	"github.com/google/uuid"
)

type DietPreferenceService struct {
	repo *Repository
}

func NewDietPreferenceService(repo *Repository) *DietPreferenceService {
	return &DietPreferenceService{repo: repo}
}

// ListDiets is the full reference list, for a checkbox UI.
func (s *DietPreferenceService) ListDiets(ctx context.Context) ([]Diet, error) {
	return s.repo.ListDiets(ctx)
}

// UserDiets is the subset a user has selected.
func (s *DietPreferenceService) UserDiets(ctx context.Context, userID uuid.UUID) ([]Diet, error) {
	return s.repo.UserDiets(ctx, userID)
}

// SetUserDiets replaces the user's full selection in one call, for a
// checkbox form submitted as a whole.
func (s *DietPreferenceService) SetUserDiets(ctx context.Context, userID uuid.UUID, dietIDs []uuid.UUID) error {
	return s.repo.SetUserDiets(ctx, userID, dietIDs)
}

func (s *DietPreferenceService) AddUserDiet(ctx context.Context, userID, dietID uuid.UUID) error {
	return s.repo.AddUserDiet(ctx, userID, dietID)
}

func (s *DietPreferenceService) RemoveUserDiet(ctx context.Context, userID, dietID uuid.UUID) error {
	return s.repo.RemoveUserDiet(ctx, userID, dietID)
}

// UserDietNames is the person's diet preferences by name.
func (s *DietPreferenceService) UserDietNames(ctx context.Context, userID uuid.UUID) ([]string, error) {
	diets, err := s.UserDiets(ctx, userID)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(diets))
	for i, d := range diets {
		names[i] = d.Name
	}
	return names, nil
}

// Exclusions are the foods the person last said they won't eat when asking
// for a meal plan, or none.
func (s *DietPreferenceService) Exclusions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	intake, err := s.repo.LatestMealPlanIntake(ctx, userID)
	if err != nil || intake == nil {
		return nil, err
	}
	return intake.Exclusions, nil
}
