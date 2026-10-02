package social

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social/facebook"
)

// Facebook is what Connect Facebook needs from Facebook. *facebook.Client
// satisfies it.
type Facebook interface {
	AuthCodeURL(state, redirectURI string) string
	// Import turns a code into the person's app-scoped id and their friends'
	// ids. The token it gets on the way is never returned.
	Import(ctx context.Context, code, redirectURI string) (facebook.Account, error)
}

const (
	// facebookStateTTL matches the Strava flows.
	facebookStateTTL = 10 * time.Minute
	// FacebookImportTTL is how long the people an import found stay on the
	// page. Long enough to go through the list and follow; short enough that
	// it is a hand-off and not a copy of somebody's friend list.
	FacebookImportTTL = time.Hour
)

// ErrFacebookTaken is a Facebook account already connected to another Khepri
// account. It is refused, not moved.
var ErrFacebookTaken = apperr.Wrap(apperr.ErrConflict, "that facebook account is connected to another khepri account")

// WithFacebook turns Connect Facebook on. Without it every Facebook method is
// not found and no surface offers it.
func (s *Service) WithFacebook(fb Facebook) *Service { s.facebook = fb; return s }

// FacebookEnabled reports whether this deployment has a Meta app configured.
func (s *Service) FacebookEnabled() bool { return s.facebook != nil }

func (s *Service) requireFacebook() error {
	if s.facebook == nil {
		return apperr.Wrap(apperr.ErrNotFound, "facebook is not configured")
	}
	return nil
}

// FacebookAuthURL is the consent dialog for a flow whose state the caller
// keeps, as the web flow does in a cookie.
func (s *Service) FacebookAuthURL(state, redirectURI string) (string, error) {
	if err := s.requireFacebook(); err != nil {
		return "", err
	}
	return s.facebook.AuthCodeURL(state, redirectURI), nil
}

// BeginNativeFacebook stores a state for this account and returns the consent
// URL. The iOS app's sign-in sheet carries no session, so the state is what
// FinishNativeFacebook finds the account by.
func (s *Service) BeginNativeFacebook(ctx context.Context, userID uuid.UUID, redirectURI string) (string, error) {
	if err := s.requireFacebook(); err != nil {
		return "", err
	}
	state, err := NewOAuthState()
	if err != nil {
		return "", err
	}
	if err := s.repo.SaveFacebookState(ctx, hashState(state), userID, time.Now().Add(facebookStateTTL)); err != nil {
		return "", err
	}
	return s.facebook.AuthCodeURL(state, redirectURI), nil
}

// FinishNativeFacebook completes a connection begun by BeginNativeFacebook.
// An unknown, expired or reused state is not found.
func (s *Service) FinishNativeFacebook(ctx context.Context, state, code, redirectURI string) (FacebookFriends, error) {
	if err := s.requireFacebook(); err != nil {
		return FacebookFriends{}, err
	}
	if strings.TrimSpace(state) == "" {
		return FacebookFriends{}, apperr.ErrNotFound
	}
	userID, err := s.repo.TakeFacebookState(ctx, hashState(state))
	if err != nil {
		return FacebookFriends{}, err
	}
	return s.ConnectFacebook(ctx, userID, code, redirectURI)
}

// ConnectFacebook links the Facebook account behind code to this account,
// whatever email either has, and finds which of its Facebook friends are
// here. Only the link is kept, and the found accounts for an hour; the token
// and the friend list are not.
func (s *Service) ConnectFacebook(ctx context.Context, userID uuid.UUID, code, redirectURI string) (FacebookFriends, error) {
	if err := s.requireFacebook(); err != nil {
		return FacebookFriends{}, err
	}
	account, err := s.facebook.Import(ctx, code, redirectURI)
	if err != nil {
		return FacebookFriends{}, err
	}
	if err = s.repo.LinkFacebook(ctx, userID, account.ID); err != nil {
		if errors.Is(err, errFacebookTaken) {
			return FacebookFriends{}, ErrFacebookTaken
		}
		return FacebookFriends{}, err
	}
	var found []uuid.UUID
	if len(account.FriendIDs) > 0 {
		if found, err = s.repo.FacebookFriendsHere(ctx, userID, account.FriendIDs); err != nil {
			return FacebookFriends{}, err
		}
	}
	if err = s.repo.SaveFacebookImport(ctx, userID, found, time.Now().Add(FacebookImportTTL)); err != nil {
		return FacebookFriends{}, err
	}
	return s.FacebookFriends(ctx, userID)
}

// FacebookFriends is whether Facebook is connected and who the last import,
// if it has not expired, found. It answers with Facebook switched off too,
// so a link made while it was on can still be seen and removed.
func (s *Service) FacebookFriends(ctx context.Context, userID uuid.UUID) (FacebookFriends, error) {
	var out FacebookFriends
	var err error
	if out.Connected, err = s.repo.FacebookConnected(ctx, userID); err != nil {
		return FacebookFriends{}, err
	}
	out.People = []Connection{}
	if !out.Connected {
		return out, nil
	}
	ids, importedAt, err := s.repo.FacebookImport(ctx, userID)
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		return out, nil
	case err != nil:
		return FacebookFriends{}, err
	}
	out.ImportedAt = importedAt
	if len(ids) == 0 {
		return out, nil
	}
	if out.People, err = s.repo.PeopleByIDs(ctx, userID, ids); err != nil {
		return FacebookFriends{}, err
	}
	return out, nil
}

// DisconnectFacebook removes the link and the last import. It works with
// Facebook switched off too: a link already made must always be removable.
func (s *Service) DisconnectFacebook(ctx context.Context, userID uuid.UUID) error {
	return s.repo.UnlinkFacebook(ctx, userID)
}

// NewOAuthState mints an opaque CSRF state for an authorization request.
func NewOAuthState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", apperr.Wrap(err, "generate oauth state")
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashState(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}
