package social

import (
	"context"
	"errors"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	socialdb "github.com/NorthAIProject/north-client/internal/social/db"
)

// errFacebookTaken is returned by LinkFacebook when another account holds
// the Facebook id.
var errFacebookTaken = errors.New("facebook account linked elsewhere")

// SaveFacebookState stores a native connection's state hash, and drops
// expired ones while it is here.
func (r *Repository) SaveFacebookState(ctx context.Context, stateHash []byte, userID uuid.UUID, expiresAt time.Time) error {
	if err := r.q.DeleteExpiredFacebookOAuthStates(ctx); err != nil {
		return apperr.Wrap(err, "prune facebook states")
	}
	err := r.q.CreateFacebookOAuthState(ctx, socialdb.CreateFacebookOAuthStateParams{StateHash: stateHash, UserID: userID, ExpiresAt: expiresAt})
	return apperr.Wrap(err, "save facebook state")
}

// TakeFacebookState returns the account a state was issued to and removes
// it. ErrNotFound covers unknown, expired and already-used states alike.
func (r *Repository) TakeFacebookState(ctx context.Context, stateHash []byte) (uuid.UUID, error) {
	userID, err := r.q.TakeFacebookOAuthState(ctx, stateHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "take facebook state")
	}
	return userID, nil
}

// LinkFacebook attaches the Facebook id to the account, replacing any other
// Facebook account it had. An id held by a different account is refused,
// never moved: whoever connected it first keeps it.
func (r *Repository) LinkFacebook(ctx context.Context, userID uuid.UUID, subject string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin link facebook")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)

	owner, err := q.FacebookOwner(ctx, subject)
	switch {
	case err == nil && owner == userID:
		return nil
	case err == nil:
		return errFacebookTaken
	case !errors.Is(err, pgx.ErrNoRows):
		return apperr.Wrap(err, "facebook owner")
	}
	if err = q.UnlinkFacebook(ctx, userID); err != nil {
		return apperr.Wrap(err, "replace facebook link")
	}
	if _, err = q.LinkFacebook(ctx, socialdb.LinkFacebookParams{UserID: userID, Subject: subject}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Another account linked it between the read and the insert.
			return errFacebookTaken
		}
		return apperr.Wrap(err, "link facebook")
	}
	return apperr.Wrap(tx.Commit(ctx), "commit link facebook")
}

// UnlinkFacebook removes the link and whatever the last import found.
func (r *Repository) UnlinkFacebook(ctx context.Context, userID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin unlink facebook")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	if err = q.UnlinkFacebook(ctx, userID); err != nil {
		return apperr.Wrap(err, "unlink facebook")
	}
	if err = q.DeleteFacebookImport(ctx, userID); err != nil {
		return apperr.Wrap(err, "delete facebook import")
	}
	return apperr.Wrap(tx.Commit(ctx), "commit unlink facebook")
}

func (r *Repository) FacebookConnected(ctx context.Context, userID uuid.UUID) (bool, error) {
	ok, err := r.q.IsFacebookConnected(ctx, userID)
	if err != nil {
		return false, apperr.Wrap(err, "facebook connected")
	}
	return ok, nil
}

// FacebookFriendsHere maps Facebook ids to the accounts the viewer may be
// shown; see the query.
func (r *Repository) FacebookFriendsHere(ctx context.Context, viewerID uuid.UUID, subjects []string) ([]uuid.UUID, error) {
	ids, err := r.q.FacebookFriendsHere(ctx, socialdb.FacebookFriendsHereParams{Viewer: viewerID, Subjects: subjects})
	if err != nil {
		return nil, apperr.Wrap(err, "facebook friends here")
	}
	return ids, nil
}

func (r *Repository) SaveFacebookImport(ctx context.Context, userID uuid.UUID, people []uuid.UUID, expiresAt time.Time) error {
	if people == nil {
		people = []uuid.UUID{}
	}
	err := r.q.SaveFacebookImport(ctx, socialdb.SaveFacebookImportParams{UserID: userID, People: people, ExpiresAt: expiresAt})
	return apperr.Wrap(err, "save facebook import")
}

// FacebookImport is the last import's accounts and when it ran.
// ErrNotFound when there is none or it has expired.
func (r *Repository) FacebookImport(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, time.Time, error) {
	row, err := r.q.FacebookImport(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, apperr.ErrNotFound
	}
	if err != nil {
		return nil, time.Time{}, apperr.Wrap(err, "facebook import")
	}
	return row.People, row.ImportedAt, nil
}

// PeopleByIDs is these accounts as the viewer may see them, with how the
// viewer follows each.
func (r *Repository) PeopleByIDs(ctx context.Context, viewerID uuid.UUID, ids []uuid.UUID) ([]Connection, error) {
	rows, err := r.q.PeopleByIDs(ctx, socialdb.PeopleByIDsParams{Viewer: viewerID, Ids: ids})
	if err != nil {
		return nil, apperr.Wrap(err, "people by ids")
	}
	out := make([]Connection, 0, len(rows))
	for _, row := range rows {
		out = append(out, Connection{Person: Person{ID: row.ID, DisplayName: row.DisplayName, Handle: util.Val(row.Handle)}, Status: row.Following})
	}
	return out, nil
}
