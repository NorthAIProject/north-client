package social

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	socialdb "github.com/NorthAIProject/north-client/internal/social/db"
)

// errTooManyStarts is returned by ReserveVerification when either count is
// already at its limit.
var errTooManyStarts = errors.New("too many verification starts")

// pendingVerification is the code an account is waiting on.
type pendingVerification struct {
	ID     uuid.UUID
	Number string
}

// StartLimit is how many texts may have been sent since a moment.
type StartLimit struct {
	Since time.Time
	Max   int
}

// VerifiedPhone is the account's number and when it was verified; empty and
// zero when it has none.
func (r *Repository) VerifiedPhone(ctx context.Context, userID uuid.UUID) (string, time.Time, error) {
	row, err := r.q.GetPhone(ctx, userID)
	if err != nil {
		return "", time.Time{}, apperr.Wrap(err, "get phone")
	}
	if row.PhoneE164 == nil || row.PhoneVerifiedAt == nil {
		return "", time.Time{}, nil
	}
	return *row.PhoneE164, *row.PhoneVerifiedAt, nil
}

// PendingVerification is the latest open text sent after since. ErrNotFound
// when there is none.
func (r *Repository) PendingVerification(ctx context.Context, userID uuid.UUID, since time.Time) (pendingVerification, error) {
	row, err := r.q.PendingPhoneVerification(ctx, socialdb.PendingPhoneVerificationParams{UserID: userID, Since: since})
	if errors.Is(err, pgx.ErrNoRows) {
		return pendingVerification{}, apperr.ErrNotFound
	}
	if err != nil {
		return pendingVerification{}, apperr.Wrap(err, "pending phone verification")
	}
	return pendingVerification{ID: row.ID, Number: row.PhoneE164}, nil
}

// ReserveVerification records a text about to be sent, unless the account or
// the number has already had its limit, and closes any earlier open one. It
// also prunes rows older than pruneBefore, which no limit looks at any more.
func (r *Repository) ReserveVerification(ctx context.Context, userID uuid.UUID, number string,
	byUser, byNumber StartLimit, pruneBefore time.Time,
) (uuid.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "begin phone start")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)

	if err = q.LockUserForPhone(ctx, userID); err != nil {
		return uuid.Nil, apperr.Wrap(err, "lock account for phone start")
	}
	if err = q.PrunePhoneVerifications(ctx, pruneBefore); err != nil {
		return uuid.Nil, apperr.Wrap(err, "prune phone verifications")
	}
	n, err := q.CountPhoneStartsByUser(ctx, socialdb.CountPhoneStartsByUserParams{UserID: userID, Since: byUser.Since})
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "count phone starts")
	}
	if n >= int64(byUser.Max) {
		return uuid.Nil, errTooManyStarts
	}
	n, err = q.CountPhoneStartsByNumber(ctx, socialdb.CountPhoneStartsByNumberParams{PhoneE164: number, Since: byNumber.Since})
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "count phone starts")
	}
	if n >= int64(byNumber.Max) {
		return uuid.Nil, errTooManyStarts
	}
	if err = q.ClosePhoneVerifications(ctx, userID); err != nil {
		return uuid.Nil, apperr.Wrap(err, "close phone verifications")
	}
	row, err := q.CreatePhoneVerification(ctx, socialdb.CreatePhoneVerificationParams{UserID: userID, PhoneE164: number})
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "create phone verification")
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, apperr.Wrap(err, "commit phone start")
	}
	return row.ID, nil
}

func (r *Repository) CloseVerification(ctx context.Context, id uuid.UUID) error {
	return apperr.Wrap(r.q.ClosePhoneVerification(ctx, id), "close phone verification")
}

func (r *Repository) CloseVerifications(ctx context.Context, userID uuid.UUID) error {
	return apperr.Wrap(r.q.ClosePhoneVerifications(ctx, userID), "close phone verifications")
}

// CountCheck records one code checked against a text and returns how many
// have been, this one included.
func (r *Repository) CountCheck(ctx context.Context, id uuid.UUID) (int, error) {
	n, err := r.q.CountPhoneCheck(ctx, id)
	if err != nil {
		return 0, apperr.Wrap(err, "count phone check")
	}
	return int(n), nil
}

// VerifyPhone gives the account the number, takes it from any other account
// holding it, and closes the text, in one transaction.
func (r *Repository) VerifyPhone(ctx context.Context, userID, verificationID uuid.UUID, number string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin verify phone")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	if err = q.ClosePhoneVerification(ctx, verificationID); err != nil {
		return apperr.Wrap(err, "close phone verification")
	}
	if err = q.ReleasePhone(ctx, socialdb.ReleasePhoneParams{Phone: &number, Keeper: userID}); err != nil {
		return apperr.Wrap(err, "release phone")
	}
	if err = q.SetPhone(ctx, socialdb.SetPhoneParams{ID: userID, Phone: &number}); err != nil {
		return apperr.Wrap(err, "set phone")
	}
	return apperr.Wrap(tx.Commit(ctx), "commit verify phone")
}

func (r *Repository) ClearPhone(ctx context.Context, userID uuid.UUID) error {
	return apperr.Wrap(r.q.ClearPhone(ctx, userID), "clear phone")
}
