package auth

import (
	"log/slog"
	"net/http"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// The routes a web front end needs to sign people in without serving the
// pages from this binary: which methods are switched on, Google's redirect
// flow split into its two halves, and the reset link's landing. Each reuses
// the Service method the Go pages call, so both front ends apply the same
// rules. None of them sets a cookie; the front end owns its session.

// AuthOptionsResponse says which optional sign-in methods this deployment has
// configured, so a sign-in page only offers what works.
type AuthOptionsResponse struct {
	Google        bool `json:"google"`
	Passkey       bool `json:"passkey"`
	PasswordReset bool `json:"passwordReset"`
}

// GoogleStartRequest names the callback the front end serves. Empty means
// this server's own /auth/google/callback.
type GoogleStartRequest struct {
	RedirectURI string `json:"redirectUri"`
}

// GoogleStartResponse is where to send the browser, and the state the front
// end must keep (in its own short-lived cookie) and compare on the callback.
type GoogleStartResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

// GoogleCodeRequest is the callback's code, with the redirect URI it was
// issued for: Google refuses an exchange whose redirect URI differs.
type GoogleCodeRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirectUri"`
}

// ResetPasswordRequest is the set-new-password form from the emailed link.
type ResetPasswordRequest struct {
	Token                string `json:"token"`
	Password             string `json:"password"`
	PasswordConfirmation string `json:"passwordConfirmation"`
}

func (a *API) options(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, AuthOptionsResponse{
		Google:        a.service.GoogleEnabled(),
		Passkey:       a.service.PasskeyEnabled(),
		PasswordReset: a.service.PasswordResetEnabled(),
	})
}

func (a *API) googleStart(w http.ResponseWriter, r *http.Request) {
	if !a.service.GoogleEnabled() {
		httpx.Error(w, apperr.ErrNotFound, i18n.T(r.Context(), "auth.err.google.unavailable"))
		return
	}
	var req GoogleStartRequest
	if r.ContentLength != 0 {
		if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
			httpx.Error(w, err, i18n.T(r.Context(), "auth.api.body.google"))
			return
		}
	}
	state, err := NewGoogleState()
	if err != nil {
		middleware.FromContext(r.Context()).Error("google oauth state", slog.Any("error", err))
		httpx.Error(w, err, i18n.T(r.Context(), "auth.err.google.start"))
		return
	}
	url, err := a.service.GoogleAuthCodeURLFor(state, req.RedirectURI)
	if err != nil {
		httpx.Error(w, err, i18n.T(r.Context(), "auth.err.google.start"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, GoogleStartResponse{URL: url, State: state})
}

func (a *API) googleCode(w http.ResponseWriter, r *http.Request) {
	if !a.service.GoogleEnabled() {
		httpx.Error(w, apperr.ErrNotFound, i18n.T(r.Context(), "auth.err.google.unavailable"))
		return
	}
	var req GoogleCodeRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, i18n.T(r.Context(), "auth.api.body.google"))
		return
	}
	user, token, expiresAt, err := a.service.CompleteGoogleOAuthFor(r.Context(), req.Code, req.RedirectURI, a.mw.RequestMetadata(r))
	if err != nil {
		var fields apperr.FieldErrors
		if apperr.As(err, &fields) {
			httpx.Error(w, err, i18n.T(r.Context(), "auth.api.fields"))
			return
		}
		// A refused exchange is Google saying no, not this server failing:
		// 401, the way the Go page answered with its "could not sign in" card.
		middleware.FromContext(r.Context()).Error("google oauth failed", slog.Any("error", err))
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: i18n.T(r.Context(), "auth.err.google.failed")}})
		return
	}
	middleware.FromContext(r.Context()).Info("signed in with google", slog.String("user_id", user.ID.String()))
	httpx.WriteJSON(w, http.StatusOK, AuthResponse{Token: token, ExpiresAt: expiresAt, User: ProjectUser(user)})
}

func (a *API) resetPassword(w http.ResponseWriter, r *http.Request) {
	// The same switch the reset page obeys: with no mailer that can deliver,
	// no link was ever sent, and a reset route would only be an oracle.
	if !a.service.PasswordResetEnabled() {
		httpx.Error(w, apperr.ErrNotFound, i18n.T(r.Context(), "auth.err.reset.invalid"))
		return
	}
	var req ResetPasswordRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, i18n.T(r.Context(), "auth.err.form"))
		return
	}
	user, token, err := a.service.ResetPassword(r.Context(), ResetPasswordInput(req), a.mw.RequestMetadata(r))
	if err != nil {
		if apperr.Is(err, ErrInvalidResetToken) {
			httpx.WriteJSON(w, http.StatusUnauthorized, httpx.ErrorBody{Error: httpx.ErrorDetail{
				Message: i18n.T(r.Context(), "auth.err.reset.invalid"),
				Code:    "invalid_reset_token",
			}})
			return
		}
		a.writeAuthError(w, err, i18n.T(r.Context(), "auth.err.reset.failed"))
		return
	}
	middleware.FromContext(r.Context()).Info("password reset", slog.String("user_id", user.ID.String()))
	a.writeSession(w, r, http.StatusOK, user.ID.String(), token)
}
