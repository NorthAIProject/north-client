package biometrics

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	biometricsdb "github.com/NorthAIProject/north-client/internal/biometrics/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q    *biometricsdb.Queries
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: biometricsdb.New(pool), pool: pool}
}

// RecordCurrent retires the previous current row and inserts a new one, in a
// single transaction so a reader never sees zero or two current rows.
func (r *Repository) RecordCurrent(ctx context.Context, userID uuid.UUID, in Input) (Biometric, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Biometric{}, apperr.Wrap(err, "begin biometrics transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := r.q.WithTx(tx)

	if err = qtx.UnsetCurrentBiometrics(ctx, userID); err != nil {
		return Biometric{}, apperr.Wrap(err, "unset current biometrics")
	}

	row, err := qtx.InsertBiometric(ctx, biometricsdb.InsertBiometricParams{
		UserID:      userID,
		WeightKg:    in.WeightKg,
		HeightCm:    in.HeightCm,
		DateOfBirth: pgDate(in.DateOfBirth),
		Sex:         in.Sex,
	})
	if err != nil {
		return Biometric{}, apperr.Wrap(err, "insert biometric")
	}

	if err := tx.Commit(ctx); err != nil {
		return Biometric{}, apperr.Wrap(err, "commit biometrics transaction")
	}

	return fromDB(row), nil
}

func (r *Repository) Current(ctx context.Context, userID uuid.UUID) (Biometric, error) {
	row, err := r.q.CurrentBiometric(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Biometric{}, apperr.ErrNotFound
		}
		return Biometric{}, apperr.Wrap(err, "current biometric")
	}
	return fromDB(row), nil
}

func (r *Repository) History(ctx context.Context, userID uuid.UUID, limit int) ([]Biometric, error) {
	rows, err := r.q.ListBiometrics(ctx, biometricsdb.ListBiometricsParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list biometrics")
	}

	out := make([]Biometric, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func fromDB(row biometricsdb.UserBiometric) Biometric {
	b := Biometric{
		ID:        row.ID,
		UserID:    row.UserID,
		WeightKg:  row.WeightKg,
		HeightCm:  row.HeightCm,
		Sex:       row.Sex,
		IsCurrent: row.IsCurrent,
		CreatedAt: row.CreatedAt,
	}
	if row.DateOfBirth.Valid {
		b.DateOfBirth = row.DateOfBirth.Time
	}
	return b
}

// pgDate converts a validated, always-present date of birth to Postgres's
// date type. Unlike goals' target_date, this field is required, so there is
// no zero-means-null case to handle here.
func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true}
}
