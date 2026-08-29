package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	messagingdb "github.com/NorthAIProject/north-client/internal/messaging/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Link is one platform account bound to a North account.
type Link struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Platform     string
	ExternalID   string
	LastUpdateID int64

	// AccountID is the platform account whose sequence LastUpdateID belongs to.
	AccountID  string
	CreatedAt  time.Time
	LastSeenAt *time.Time
}

type Repository struct {
	q *messagingdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: messagingdb.New(pool)}
}

// ClaimUpdate resolves the sender and rejects a redelivery in one statement.
//
// Returns ErrNotFound when the chat is not linked *or* when this update has
// already been handled — the caller separates the two with Get, which only
// costs a query off the common path. See the query's comment for why the two
// checks cannot be split.
//
// updateID of 0 means the platform has no delivery counter, so the watermark
// is skipped and this is a plain lookup with a touch.
func (r *Repository) ClaimUpdate(ctx context.Context, platform, externalID string, updateID int64, accountID string) (Link, error) {
	if updateID == 0 {
		return r.Get(ctx, platform, externalID)
	}

	row, err := r.q.ClaimMessagingUpdate(ctx, messagingdb.ClaimMessagingUpdateParams{
		Platform: platform, ExternalID: externalID, LastUpdateID: updateID,
		AccountID: accountID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Link{}, apperr.ErrNotFound
		}
		return Link{}, apperr.Wrap(err, "claim messaging update")
	}
	return linkFromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, platform, externalID string) (Link, error) {
	row, err := r.q.GetMessagingLink(ctx, messagingdb.GetMessagingLinkParams{
		Platform: platform, ExternalID: externalID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Link{}, apperr.ErrNotFound
		}
		return Link{}, apperr.Wrap(err, "get messaging link")
	}
	return linkFromDB(row), nil
}

func (r *Repository) ListByUser(ctx context.Context, userID uuid.UUID) ([]Link, error) {
	rows, err := r.q.ListMessagingLinksByUser(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list messaging links")
	}
	out := make([]Link, 0, len(rows))
	for _, row := range rows {
		out = append(out, linkFromDB(row))
	}
	return out, nil
}

// Insert binds a chat to an account.
//
// A chat already bound to the same account is a success: somebody redeeming a
// second code from the same chat has asked for a state that already holds.
// Bound to a different account it is a conflict, because the alternative —
// silently moving the link — would let anyone who can message the bot take
// over a chat that is not theirs. This is linkGoogleIdentity's rule, and it is
// the same rule for the same reason.
func (r *Repository) Insert(ctx context.Context, userID uuid.UUID, platform, externalID string) (Link, error) {
	row, err := r.q.InsertMessagingLink(ctx, messagingdb.InsertMessagingLinkParams{
		UserID: userID, Platform: platform, ExternalID: externalID,
	})
	if err == nil {
		return linkFromDB(row), nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return Link{}, apperr.Wrap(err, "insert messaging link")
	}

	existing, getErr := r.Get(ctx, platform, externalID)
	if getErr != nil {
		return Link{}, apperr.Wrap(getErr, "insert messaging link: read existing")
	}
	if existing.UserID != userID {
		return Link{}, apperr.Wrap(apperr.ErrConflict, "this chat is already linked to another account")
	}
	return existing, nil
}

// Delete unlinks a platform from an account. Reports whether anything was
// linked, so a settings page can tell "disconnected" from "was not connected".
func (r *Repository) Delete(ctx context.Context, userID uuid.UUID, platform string) (bool, error) {
	rows, err := r.q.DeleteMessagingLink(ctx, messagingdb.DeleteMessagingLinkParams{
		UserID: userID, Platform: platform,
	})
	if err != nil {
		return false, apperr.Wrap(err, "delete messaging link")
	}
	return rows > 0, nil
}

// InsertCode stores a link code's hash, replacing any earlier one for this
// person and platform so only one code is ever live.
//
// The plaintext never reaches here.
func (r *Repository) InsertCode(ctx context.Context, codeHash []byte, userID uuid.UUID, platform string, expiresAt time.Time) error {
	if err := r.q.DeleteMessagingLinkCodesForUser(ctx, messagingdb.DeleteMessagingLinkCodesForUserParams{
		UserID: userID, Platform: platform,
	}); err != nil {
		return apperr.Wrap(err, "clear messaging link codes")
	}
	if err := r.q.CreateMessagingLinkCode(ctx, messagingdb.CreateMessagingLinkCodeParams{
		CodeHash: codeHash, UserID: userID, Platform: platform, ExpiresAt: expiresAt,
	}); err != nil {
		return apperr.Wrap(err, "create messaging link code")
	}
	return nil
}

// RedeemCode spends a code and reports whose it was.
//
// Expiry and single use are enforced in the statement, so two messages racing
// with the same code produce one winner. The error is never wrapped with the
// hash: an error string is where a credential-shaped value ends up in a log.
func (r *Repository) RedeemCode(ctx context.Context, codeHash []byte, platform string) (uuid.UUID, error) {
	userID, err := r.q.RedeemMessagingLinkCode(ctx, messagingdb.RedeemMessagingLinkCodeParams{
		CodeHash: codeHash, Platform: platform,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, apperr.ErrNotFound
		}
		return uuid.Nil, errors.New("redeem messaging link code: query failed")
	}
	return userID, nil
}

// DeleteExpiredCodes clears codes nobody ever sent. Spent codes stay; their
// tombstone is what lets a replay be recognised rather than merely missed.
func (r *Repository) DeleteExpiredCodes(ctx context.Context, before time.Time) error {
	if err := r.q.DeleteExpiredMessagingLinkCodes(ctx, before); err != nil {
		return apperr.Wrap(err, "delete expired messaging link codes")
	}
	return nil
}

// uniqueViolation is Postgres' SQLSTATE for a duplicate key.
const uniqueViolation = "23505"

func linkFromDB(row messagingdb.MessagingLink) Link {
	return Link{
		ID:           row.ID,
		UserID:       row.UserID,
		Platform:     row.Platform,
		ExternalID:   row.ExternalID,
		LastUpdateID: row.LastUpdateID,
		AccountID:    row.AccountID,
		CreatedAt:    row.CreatedAt,
		LastSeenAt:   row.LastSeenAt,
	}
}
