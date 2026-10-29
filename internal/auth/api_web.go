package auth

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/shared/echox"
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

func (a *API) options(c *echo.Context) error {
	return c.JSON(http.StatusOK, AuthOptionsResponse{
		Google:        a.service.GoogleEnabled(),
		Passkey:       a.service.PasskeyEnabled(),
		PasswordReset: a.service.PasswordResetEnabled(),
	})
}

func (a *API) googleStart(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	if !a.service.GoogleEnabled() {
		return httpx.Fail(apperr.ErrNotFound, i18n.T(ctx, "auth.err.google.unavailable"))
	}
	var req GoogleStartRequest
	if r.ContentLength != 0 {
		if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, i18n.T(ctx, "auth.api.body.google")); err != nil {
			return err
		}
	}
	state, err := NewGoogleState()
	if err != nil {
		middleware.FromContext(ctx).Error("google oauth state", slog.Any("error", err))
		return httpx.Fail(err, i18n.T(ctx, "auth.err.google.start"))
	}
	url, err := a.service.GoogleAuthCodeURLFor(state, req.RedirectURI)
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "auth.err.google.start"))
	}
	return c.JSON(http.StatusOK, GoogleStartResponse{URL: url, State: state})
}

func (a *API) googleCode(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	if !a.service.GoogleEnabled() {
		return httpx.Fail(apperr.ErrNotFound, i18n.T(ctx, "auth.err.google.unavailable"))
	}
	var req GoogleCodeRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 16 << 10}, i18n.T(ctx, "auth.api.body.google")); err != nil {
		return err
	}
	user, token, expiresAt, err := a.service.CompleteGoogleOAuthFor(ctx, req.Code, req.RedirectURI, a.mw.RequestMetadata(r))
	if err != nil {
		var fields apperr.FieldErrors
		if apperr.As(err, &fields) {
			return httpx.Fail(err, i18n.T(ctx, "auth.api.fields"))
		}
		// A refused exchange is Google saying no, not this server failing:
		// 401, the way the Go page answered with its "could not sign in" card.
		middleware.FromContext(ctx).Error("google oauth failed", slog.Any("error", err))
		return c.JSON(http.StatusUnauthorized, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: i18n.T(ctx, "auth.err.google.failed")}})
	}
	middleware.FromContext(ctx).Info("signed in with google", slog.String("user_id", user.ID.String()))
	return c.JSON(http.StatusOK, AuthResponse{Token: token, ExpiresAt: expiresAt, User: ProjectUser(user)})
}

func (a *API) resetPassword(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	// The same switch the reset page obeys: with no mailer that can deliver,
	// no link was ever sent, and a reset route would only be an oracle.
	if !a.service.PasswordResetEnabled() {
		return httpx.Fail(apperr.ErrNotFound, i18n.T(ctx, "auth.err.reset.invalid"))
	}
	var req ResetPasswordRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 16 << 10}, i18n.T(ctx, "auth.err.form")); err != nil {
		return err
	}
	user, token, err := a.service.ResetPassword(ctx, ResetPasswordInput(req), a.mw.RequestMetadata(r))
	if err != nil {
		if apperr.Is(err, ErrInvalidResetToken) {
			return c.JSON(http.StatusUnauthorized, httpx.ErrorBody{Error: httpx.ErrorDetail{
				Message: i18n.T(ctx, "auth.err.reset.invalid"),
				Code:    "invalid_reset_token",
			}})
		}
		return authError(c, err, i18n.T(ctx, "auth.err.reset.failed"))
	}
	middleware.FromContext(ctx).Info("password reset", slog.String("user_id", user.ID.String()))
	return a.writeSession(c, http.StatusOK, user.ID.String(), token)
}
