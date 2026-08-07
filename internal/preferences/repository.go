package preferences

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	preferencesdb "github.com/NorthAIProject/north-client/internal/preferences/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *preferencesdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: preferencesdb.New(pool)}
}

func (r *Repository) Get(ctx context.Context, userID uuid.UUID) (Preferences, error) {
	row, err := r.q.GetUserPreferences(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Preferences{}, apperr.ErrNotFound
		}
		return Preferences{}, apperr.Wrap(err, "get user preferences")
	}
	return fromDB(row), nil
}

func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, in Input) (Preferences, error) {
	row, err := r.q.UpsertUserPreferences(ctx, preferencesdb.UpsertUserPreferencesParams{
		UserID: userID, UnitsSystem: in.UnitsSystem, DefaultGoal: in.DefaultGoal, DefaultMacroSplit: in.DefaultMacroSplit,
	})
	if err != nil {
		return Preferences{}, apperr.Wrap(err, "upsert user preferences")
	}
	return fromDB(row), nil
}

func fromDB(row preferencesdb.UserPreference) Preferences {
	return Preferences{
		UserID: row.UserID, UnitsSystem: row.UnitsSystem,
		DefaultGoal: row.DefaultGoal, DefaultMacroSplit: row.DefaultMacroSplit,
		UpdatedAt: row.UpdatedAt,
	}
}
