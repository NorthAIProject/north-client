package apns

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	apnsdb "github.com/NorthAIProject/north-client/internal/apns/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *apnsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: apnsdb.New(pool)}
}

// Upsert stores a device, moving it to this person if another held it.
func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, in Input) (Device, error) {
	row, err := r.q.UpsertAPNsDevice(ctx, apnsdb.UpsertAPNsDeviceParams{
		UserID:      userID,
		Token:       in.Token,
		Topic:       in.Topic,
		Environment: in.Environment,
	})
	if err != nil {
		return Device{}, apperr.Wrap(err, "upsert apns device")
	}
	return fromDB(row), nil
}

func (r *Repository) ListByUser(ctx context.Context, userID uuid.UUID) ([]Device, error) {
	rows, err := r.q.ListAPNsDevices(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list apns devices")
	}
	out := make([]Device, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

// DeleteByToken removes the person's own device. Scoped to the user so nobody
// can silence an install that is not theirs.
func (r *Repository) DeleteByToken(ctx context.Context, userID uuid.UUID, token string) error {
	if _, err := r.q.DeleteAPNsDeviceByToken(ctx, apnsdb.DeleteAPNsDeviceByTokenParams{UserID: userID, Token: token}); err != nil {
		return apperr.Wrap(err, "delete apns device")
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	return apperr.Wrap(r.q.DeleteAPNsDevice(ctx, id), "delete gone apns device")
}

func (r *Repository) MarkUsed(ctx context.Context, id uuid.UUID) error {
	return apperr.Wrap(r.q.MarkAPNsDeviceUsed(ctx, id), "mark apns device used")
}

func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID) error {
	return apperr.Wrap(r.q.MarkAPNsDeviceFailed(ctx, id), "mark apns device failed")
}

func fromDB(row apnsdb.ApnsDevice) Device {
	return Device{
		ID:          row.ID,
		UserID:      row.UserID,
		Token:       row.Token,
		Topic:       row.Topic,
		Environment: row.Environment,
		CreatedAt:   row.CreatedAt,
		LastUsedAt:  row.LastUsedAt,
		FailedAt:    row.FailedAt,
	}
}
