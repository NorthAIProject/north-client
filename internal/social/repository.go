package social

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	socialdb "github.com/NorthAIProject/north-client/internal/social/db"
)

type Repository struct {
	pool *pgxpool.Pool
	q    *socialdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, q: socialdb.New(pool)}
}

// errHandleTaken is returned by SetHandle when another account holds it.
var errHandleTaken = errors.New("handle taken")

func (r *Repository) SetHandle(ctx context.Context, userID uuid.UUID, handle *string) error {
	_, err := r.q.SetHandle(ctx, socialdb.SetHandleParams{ID: userID, Handle: handle})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return errHandleTaken
	}
	if err != nil {
		return apperr.Wrap(err, "set handle")
	}
	return nil
}

func (r *Repository) Handle(ctx context.Context, userID uuid.UUID) (string, error) {
	h, err := r.q.GetHandle(ctx, userID)
	if err != nil {
		return "", apperr.Wrap(err, "get handle")
	}
	return deref(h), nil
}

func (r *Repository) PersonByHandle(ctx context.Context, handle string) (Person, error) {
	row, err := r.q.PersonByHandle(ctx, &handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return Person{}, apperr.ErrNotFound
	}
	if err != nil {
		return Person{}, apperr.Wrap(err, "person by handle")
	}
	return Person{ID: row.ID, DisplayName: row.DisplayName, Handle: deref(row.Handle)}, nil
}

func (r *Repository) PersonByID(ctx context.Context, id uuid.UUID) (Person, error) {
	row, err := r.q.PersonByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Person{}, apperr.ErrNotFound
	}
	if err != nil {
		return Person{}, apperr.Wrap(err, "person by id")
	}
	return Person{ID: row.ID, DisplayName: row.DisplayName, Handle: deref(row.Handle)}, nil
}

// InviteFor returns the inviter's code for a channel, creating one with the
// code given if there is none yet.
func (r *Repository) InviteFor(ctx context.Context, inviterID uuid.UUID, channel, newCode string) (Invite, error) {
	existing, err := r.q.InviteFor(ctx, socialdb.InviteForParams{InviterID: inviterID, Channel: channel})
	if err == nil {
		return Invite{Code: existing.Code, InviterID: existing.InviterID, Channel: existing.Channel}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Invite{}, apperr.Wrap(err, "get invite")
	}
	created, err := r.q.CreateInvite(ctx, socialdb.CreateInviteParams{Code: newCode, InviterID: inviterID, Channel: channel})
	if errors.Is(err, pgx.ErrNoRows) {
		// Another request created it first; read theirs.
		return r.InviteFor(ctx, inviterID, channel, newCode)
	}
	if err != nil {
		return Invite{}, apperr.Wrap(err, "create invite")
	}
	return Invite{Code: created.Code, InviterID: created.InviterID, Channel: created.Channel}, nil
}

func (r *Repository) InviteByCode(ctx context.Context, code string) (InvitePreview, error) {
	row, err := r.q.InviteByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return InvitePreview{}, apperr.ErrNotFound
	}
	if err != nil {
		return InvitePreview{}, apperr.Wrap(err, "invite by code")
	}
	return InvitePreview{
		Code: row.Code, Channel: row.Channel,
		Inviter: Person{ID: row.InviterID, DisplayName: row.DisplayName, Handle: deref(row.Handle)},
	}, nil
}

// Redemption is what one Redeem call changed.
type Redemption struct {
	// Attributed is true for the account's first invite, the one that counts
	// as having brought them in.
	Attributed bool
	// Connected is true when the two were not already following each other.
	Connected bool
}

// Redeem connects the two both ways and, if this is the account's first
// invite, records it as the one that brought them, in one transaction.
func (r *Repository) Redeem(ctx context.Context, inviteeID uuid.UUID, code string, inviterID uuid.UUID) (Redemption, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Redemption{}, apperr.Wrap(err, "begin redeem")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)

	var out Redemption
	switch _, err = q.Redeem(ctx, socialdb.RedeemParams{InviteeID: inviteeID, Code: code}); {
	case err == nil:
		out.Attributed = true
	case !errors.Is(err, pgx.ErrNoRows):
		return Redemption{}, apperr.Wrap(err, "redeem invite")
	}

	// Following each other is what an invite means: they asked, you came. A
	// friend who was already on Khepri is connected the same way.
	for _, pair := range [][2]uuid.UUID{{inviteeID, inviterID}, {inviterID, inviteeID}} {
		before, getErr := q.GetFollow(ctx, socialdb.GetFollowParams{FollowerID: pair[0], FolloweeID: pair[1]})
		if getErr != nil && !errors.Is(getErr, pgx.ErrNoRows) {
			return Redemption{}, apperr.Wrap(getErr, "read follow")
		}
		if getErr != nil || before.Status != StatusAccepted {
			out.Connected = true
		}
		if _, err = q.UpsertFollow(ctx, socialdb.UpsertFollowParams{
			FollowerID: pair[0], FolloweeID: pair[1], Status: StatusAccepted,
		}); err != nil {
			return Redemption{}, apperr.Wrap(err, "connect invite")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Redemption{}, apperr.Wrap(err, "commit redeem")
	}
	return out, nil
}

func (r *Repository) Joined(ctx context.Context, inviterID uuid.UUID) (int, error) {
	n, err := r.q.CountRedemptions(ctx, inviterID)
	if err != nil {
		return 0, apperr.Wrap(err, "count redemptions")
	}
	return int(n), nil
}

func (r *Repository) Follow(ctx context.Context, followerID, followeeID uuid.UUID, status string) (string, error) {
	row, err := r.q.UpsertFollow(ctx, socialdb.UpsertFollowParams{FollowerID: followerID, FolloweeID: followeeID, Status: status})
	if err != nil {
		return "", apperr.Wrap(err, "follow")
	}
	return row.Status, nil
}

// FollowStatus is "" when there is no follow either pending or accepted.
func (r *Repository) FollowStatus(ctx context.Context, followerID, followeeID uuid.UUID) (string, error) {
	row, err := r.q.GetFollow(ctx, socialdb.GetFollowParams{FollowerID: followerID, FolloweeID: followeeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", apperr.Wrap(err, "get follow")
	}
	return row.Status, nil
}

func (r *Repository) Accept(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
	n, err := r.q.AcceptFollow(ctx, socialdb.AcceptFollowParams{FollowerID: followerID, FolloweeID: followeeID})
	if err != nil {
		return false, apperr.Wrap(err, "accept follow")
	}
	return n > 0, nil
}

func (r *Repository) Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	if _, err := r.q.DeleteFollow(ctx, socialdb.DeleteFollowParams{FollowerID: followerID, FolloweeID: followeeID}); err != nil {
		return apperr.Wrap(err, "unfollow")
	}
	return nil
}

func (r *Repository) Followers(ctx context.Context, userID uuid.UUID) ([]Connection, error) {
	rows, err := r.q.ListFollowers(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list followers")
	}
	out := make([]Connection, 0, len(rows))
	for _, row := range rows {
		out = append(out, connection(row.ID, row.DisplayName, row.Handle, row.Status, row.CreatedAt))
	}
	return out, nil
}

func (r *Repository) Following(ctx context.Context, userID uuid.UUID) ([]Connection, error) {
	rows, err := r.q.ListFollowing(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list following")
	}
	out := make([]Connection, 0, len(rows))
	for _, row := range rows {
		out = append(out, connection(row.ID, row.DisplayName, row.Handle, row.Status, row.CreatedAt))
	}
	return out, nil
}

func (r *Repository) Counts(ctx context.Context, userID uuid.UUID) (followers, following int, err error) {
	row, err := r.q.CountFollows(ctx, userID)
	if err != nil {
		return 0, 0, apperr.Wrap(err, "count follows")
	}
	return int(row.Followers), int(row.Following), nil
}

// Block records the block and removes every follow between the two, in one
// transaction, so a blocked person is never left following for a moment.
func (r *Repository) Block(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin block")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	if err = q.Block(ctx, socialdb.BlockParams{BlockerID: blockerID, BlockedID: blockedID}); err != nil {
		return apperr.Wrap(err, "block")
	}
	if err = q.DeleteFollowsBetween(ctx, socialdb.DeleteFollowsBetweenParams{FollowerID: blockerID, FolloweeID: blockedID}); err != nil {
		return apperr.Wrap(err, "remove follows on block")
	}
	if err = tx.Commit(ctx); err != nil {
		return apperr.Wrap(err, "commit block")
	}
	return nil
}

func (r *Repository) Unblock(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	if _, err := r.q.Unblock(ctx, socialdb.UnblockParams{BlockerID: blockerID, BlockedID: blockedID}); err != nil {
		return apperr.Wrap(err, "unblock")
	}
	return nil
}

func (r *Repository) Blocked(ctx context.Context, a, b uuid.UUID) (bool, error) {
	blocked, err := r.q.IsBlockedEitherWay(ctx, socialdb.IsBlockedEitherWayParams{BlockerID: a, BlockedID: b})
	if err != nil {
		return false, apperr.Wrap(err, "check block")
	}
	return blocked, nil
}

func (r *Repository) BlockedList(ctx context.Context, blockerID uuid.UUID) ([]Person, error) {
	rows, err := r.q.ListBlocked(ctx, blockerID)
	if err != nil {
		return nil, apperr.Wrap(err, "list blocked")
	}
	out := make([]Person, 0, len(rows))
	for _, row := range rows {
		out = append(out, Person{ID: row.ID, DisplayName: row.DisplayName, Handle: deref(row.Handle)})
	}
	return out, nil
}

func connection(id uuid.UUID, name string, handle *string, status string, since time.Time) Connection {
	return Connection{Person: Person{ID: id, DisplayName: name, Handle: deref(handle)}, Status: status, Since: since}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
