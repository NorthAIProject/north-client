package strava

import (
	"context"
	"crypto/sha256"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// NativeCallbackPath is where Strava returns a connection started from the iOS
// app. It is public: the app's sign-in sheet carries no session, so the state
// alone says whose connection it is.
const NativeCallbackPath = "/api/v1/fitness/strava/callback"

// nativeStateTTL matches the web flow's cookie lifetime.
const nativeStateTTL = 10 * time.Minute

// BeginNativeConnect issues a state for this person and returns the consent
// URL, which sends Strava back to NativeCallbackPath rather than to the web
// page. Strava accepts any path on the registered callback domain.
func (s *Service) BeginNativeConnect(ctx context.Context, userID uuid.UUID, baseURL string) (string, error) {
	return s.beginConnect(ctx, userID, strings.TrimRight(baseURL, "/")+NativeCallbackPath)
}

// WebCallbackPath is where Strava returns a connection started from the web
// app. The page there is the web app's own (the SvelteKit front end owns the
// browser redirect); it hands the state and code to FinishWebConnect.
const WebCallbackPath = "/app/fitness/strava/callback"

// BeginWebConnect issues a state for this person and returns the consent URL,
// sending Strava back to callbackURL — the web app's WebCallbackPath on
// whatever origin serves it. Strava itself refuses any host outside the
// registered callback domain.
func (s *Service) BeginWebConnect(ctx context.Context, userID uuid.UUID, callbackURL string) (string, error) {
	return s.beginConnect(ctx, userID, callbackURL)
}

// FinishWebConnect completes a connection begun by BeginWebConnect. Unlike the
// native return, the web callback runs inside a signed-in session, so the state
// must also belong to that session's person: a state issued to somebody else
// is refused (ErrForbidden) rather than connecting their Strava to this
// account. An unknown or expired state is ErrNotFound.
func (s *Service) FinishWebConnect(ctx context.Context, userID uuid.UUID, state, code string) error {
	if strings.TrimSpace(state) == "" {
		return apperr.ErrNotFound
	}
	owner, err := s.repo.TakeOAuthState(ctx, hashState(state))
	if err != nil {
		return err
	}
	if owner != userID {
		return apperr.ErrForbidden
	}
	return s.Connect(ctx, userID, code)
}

func (s *Service) beginConnect(ctx context.Context, userID uuid.UUID, redirectURI string) (string, error) {
	if !s.Configured() {
		return "", apperr.Wrap(apperr.ErrNotFound, "strava is not configured")
	}
	state, err := NewState()
	if err != nil {
		return "", err
	}
	if err := s.repo.SaveOAuthState(ctx, hashState(state), userID, time.Now().Add(nativeStateTTL)); err != nil {
		return "", err
	}
	return s.oauth.cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("redirect_uri", redirectURI),
		oauth2.SetAuthURLParam("approval_prompt", "auto"),
		oauth2.SetAuthURLParam("response_type", "code"),
	), nil
}

// FinishNativeConnect completes a connection begun by BeginNativeConnect.
func (s *Service) FinishNativeConnect(ctx context.Context, state, code string) error {
	if strings.TrimSpace(state) == "" {
		return apperr.ErrNotFound
	}
	userID, err := s.repo.TakeOAuthState(ctx, hashState(state))
	if err != nil {
		return err
	}
	return s.Connect(ctx, userID, code)
}

func hashState(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}
