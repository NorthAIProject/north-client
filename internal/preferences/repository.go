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

// SetNewsTickerEnabled flips only the ticker switch, leaving the calculator
// defaults untouched (or at their column defaults for a brand-new row).
func (r *Repository) SetNewsTickerEnabled(ctx context.Context, userID uuid.UUID, enabled bool) (Preferences, error) {
	row, err := r.q.SetNewsTickerEnabled(ctx, preferencesdb.SetNewsTickerEnabledParams{UserID: userID, NewsTickerEnabled: enabled})
	if err != nil {
		return Preferences{}, apperr.Wrap(err, "set news ticker enabled")
	}
	return fromDB(row), nil
}

// SetTargetWeight sets or clears the weight to aim at, leaving every other
// setting alone. Nil clears it.
func (r *Repository) SetTargetWeight(ctx context.Context, userID uuid.UUID, kg *float64) (Preferences, error) {
	row, err := r.q.SetTargetWeight(ctx, preferencesdb.SetTargetWeightParams{UserID: userID, TargetWeightKg: kg})
	if err != nil {
		return Preferences{}, apperr.Wrap(err, "set target weight")
	}
	return fromDB(row), nil
}

func fromDB(row preferencesdb.UserPreference) Preferences {
	return Preferences{
		UserID: row.UserID, UnitsSystem: row.UnitsSystem,
		DefaultGoal: row.DefaultGoal, DefaultMacroSplit: row.DefaultMacroSplit,
		NewsTickerEnabled: row.NewsTickerEnabled,
		TargetWeightKg:    row.TargetWeightKg,
		UpdatedAt:         row.UpdatedAt,
	}
}
