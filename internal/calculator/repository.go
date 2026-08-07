package calculator

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	calculatordb "github.com/NorthAIProject/north-client/internal/calculator/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q    *calculatordb.Queries
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: calculatordb.New(pool), pool: pool}
}

// RecordCurrent retires the previous current plan and inserts the new one, in
// a single transaction so a reader never sees zero or two current plans.
func (r *Repository) RecordCurrent(ctx context.Context, userID uuid.UUID, plan MacroPlan) (MacroPlan, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return MacroPlan{}, apperr.Wrap(err, "begin macro plan transaction")
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)

	if err := qtx.UnsetCurrentMacroPlans(ctx, userID); err != nil {
		return MacroPlan{}, apperr.Wrap(err, "unset current macro plans")
	}

	row, err := qtx.InsertMacroPlan(ctx, calculatordb.InsertMacroPlanParams{
		UserID:        userID,
		WeightKg:      plan.WeightKg,
		HeightCm:      plan.HeightCm,
		Age:           int16(plan.Age),
		Sex:           plan.Sex,
		ActivityLevel: plan.ActivityLevel,
		Goal:          plan.Goal,
		MacroSplit:    plan.MacroSplit,
		Bmr:           plan.BMR,
		Tdee:          plan.TDEE,
		CalorieGoal:   plan.CalorieGoal,
		ProteinG:      plan.ProteinG,
		FatG:          plan.FatG,
		CarbG:         plan.CarbG,
	})
	if err != nil {
		return MacroPlan{}, apperr.Wrap(err, "insert macro plan")
	}

	if err := tx.Commit(ctx); err != nil {
		return MacroPlan{}, apperr.Wrap(err, "commit macro plan transaction")
	}

	return fromDB(row), nil
}

func (r *Repository) Current(ctx context.Context, userID uuid.UUID) (MacroPlan, error) {
	row, err := r.q.CurrentMacroPlan(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MacroPlan{}, apperr.ErrNotFound
		}
		return MacroPlan{}, apperr.Wrap(err, "current macro plan")
	}
	return fromDB(row), nil
}

func (r *Repository) History(ctx context.Context, userID uuid.UUID, limit int) ([]MacroPlan, error) {
	rows, err := r.q.ListMacroPlans(ctx, calculatordb.ListMacroPlansParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list macro plans")
	}

	out := make([]MacroPlan, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func fromDB(row calculatordb.UserMacroPlan) MacroPlan {
	return MacroPlan{
		ID:            row.ID,
		UserID:        row.UserID,
		WeightKg:      row.WeightKg,
		HeightCm:      row.HeightCm,
		Age:           int(row.Age),
		Sex:           row.Sex,
		ActivityLevel: row.ActivityLevel,
		Goal:          row.Goal,
		MacroSplit:    row.MacroSplit,
		BMR:           row.Bmr,
		TDEE:          row.Tdee,
		CalorieGoal:   row.CalorieGoal,
		ProteinG:      row.ProteinG,
		FatG:          row.FatG,
		CarbG:         row.CarbG,
		IsCurrent:     row.IsCurrent,
		CreatedAt:     row.CreatedAt,
	}
}
