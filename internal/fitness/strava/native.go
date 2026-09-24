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
		oauth2.SetAuthURLParam("redirect_uri", strings.TrimRight(baseURL, "/")+NativeCallbackPath),
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
