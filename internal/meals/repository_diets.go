package meals

import (
	"context"

	"github.com/google/uuid"

	mealsdb "github.com/NorthAIProject/north-client/internal/meals/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func (r *Repository) ListDiets(ctx context.Context) ([]Diet, error) {
	rows, err := r.q.ListDiets(ctx)
	if err != nil {
		return nil, apperr.Wrap(err, "list diets")
	}
	return dietsFromDB(rows), nil
}

func (r *Repository) UserDiets(ctx context.Context, userID uuid.UUID) ([]Diet, error) {
	rows, err := r.q.UserDiets(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "user diets")
	}
	return dietsFromDB(rows), nil
}

// SetUserDiets replaces the full set in a transaction, so a reader never
// sees a moment with neither the old nor the new selection.
func (r *Repository) SetUserDiets(ctx context.Context, userID uuid.UUID, dietIDs []uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin set diets transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteUserDiets(ctx, userID); err != nil {
		return apperr.Wrap(err, "clear user diets")
	}
	for _, dietID := range dietIDs {
		if err := qtx.AddUserDiet(ctx, mealsdb.AddUserDietParams{UserID: userID, DietID: dietID}); err != nil {
			return apperr.Wrap(err, "add user diet")
		}
	}

	return apperr.Wrap(tx.Commit(ctx), "commit set diets transaction")
}

func (r *Repository) AddUserDiet(ctx context.Context, userID, dietID uuid.UUID) error {
	return apperr.Wrap(r.q.AddUserDiet(ctx, mealsdb.AddUserDietParams{UserID: userID, DietID: dietID}), "add user diet")
}

func (r *Repository) RemoveUserDiet(ctx context.Context, userID, dietID uuid.UUID) error {
	return apperr.Wrap(r.q.RemoveUserDiet(ctx, mealsdb.RemoveUserDietParams{UserID: userID, DietID: dietID}), "remove user diet")
}

func dietsFromDB(rows []mealsdb.Diet) []Diet {
	out := make([]Diet, 0, len(rows))
	for _, row := range rows {
		out = append(out, Diet{ID: row.ID, Code: row.Code, Name: row.Name, Description: row.Description})
	}
	return out
}
