package account

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdb "github.com/NorthAIProject/north-client/internal/account/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	pool *pgxpool.Pool
	q    *accountdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, q: accountdb.New(pool)}
}

// StorageKeys lists every object the account owns in bucket storage.
//
// Called before the erasure, never after: the keys exist only on the rows the
// erasure removes, and a bucket cannot be asked what belonged to whom.
func (r *Repository) StorageKeys(ctx context.Context, userID uuid.UUID) ([]string, error) {
	keys, err := r.q.ListUserStorageKeys(ctx, userID)
	return keys, apperr.Wrap(err, "list storage keys for erasure")
}

// Erase removes the account and records that it happened, in one transaction.
//
// Both in one transaction because they are one fact. A delete that committed
// without its record would leave no evidence the person ever asked, and a
// record that committed without its delete would claim something that had not
// happened.
//
// Returns the id of the event row, so the caller can fill in what the storage
// pass found once it has run.
func (r *Repository) Erase(ctx context.Context, userID uuid.UUID, objects int) (eventID uuid.UUID, jobs int64, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, 0, apperr.Wrap(err, "begin account erasure")
	}
	// Rollback after a successful Commit is a no-op, so this needs no flag.
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.q.WithTx(tx)

	jobs, err = q.DeleteUserJobs(ctx, userID.String())
	if err != nil {
		return uuid.Nil, 0, apperr.Wrap(err, "delete queued jobs")
	}

	rows, err := q.DeleteUser(ctx, userID)
	if err != nil {
		return uuid.Nil, 0, apperr.Wrap(err, "delete account")
	}
	if rows == 0 {
		return uuid.Nil, 0, apperr.ErrNotFound
	}

	detail, err := json.Marshal(map[string]any{"storage_objects": objects})
	if err != nil {
		return uuid.Nil, 0, apperr.Wrap(err, "encode erasure detail")
	}

	event, err := q.RecordAccountEvent(ctx, accountdb.RecordAccountEventParams{
		UserID: userID,
		Event:  EventDelete,
		Detail: detail,
	})
	if err != nil {
		return uuid.Nil, 0, apperr.Wrap(err, "record account deletion")
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, 0, apperr.Wrap(err, "commit account erasure")
	}
	return event.ID, jobs, nil
}

// RecordEvent writes a standalone account event.
func (r *Repository) RecordEvent(ctx context.Context, userID uuid.UUID, event string) error {
	_, err := r.q.RecordAccountEvent(ctx, accountdb.RecordAccountEventParams{
		UserID: userID,
		Event:  event,
	})
	return apperr.Wrap(err, "record account event %q", event)
}

// CloseErasure fills in what the storage pass found, after the account itself
// has already gone.
func (r *Repository) CloseErasure(ctx context.Context, eventID uuid.UUID, e Erasure) error {
	detail, err := json.Marshal(map[string]any{
		"storage_objects":  e.StorageObjects,
		"storage_failures": e.StorageFailures,
		"jobs":             e.Jobs,
	})
	if err != nil {
		return apperr.Wrap(err, "encode erasure detail")
	}

	return apperr.Wrap(r.q.SetAccountEventDetail(ctx, accountdb.SetAccountEventDetailParams{
		ID:     eventID,
		Detail: detail,
	}), "close erasure record")
}
