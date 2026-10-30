package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Passkeys for native clients. The ceremony is the one the browser runs,
// against the same Service methods and the same stored challenges; only the
// ends differ. Requests and responses use the API's camelCase, errors use
// ErrorBody, and finishing returns a session token instead of setting a
// cookie.

// maxCeremonyBytes bounds a passkey ceremony body.
//
// The bound is new: this decoder used to read an unbounded body, which the
// shared reader made visible by asking for the cap explicitly. A WebAuthn
// ceremony is a few kilobytes of base64url, so 64 KiB is generous.
const maxCeremonyBytes = 64 << 10

// PasskeyRegisterBeginRequest starts creating an account with a passkey.
type PasskeyRegisterBeginRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Timezone    string `json:"timezone"`
}

// PasskeyLoginBeginRequest optionally names the account. Without an email the
// ceremony is discoverable: the device offers whichever passkeys it holds.
type PasskeyLoginBeginRequest struct {
	Email string `json:"email"`
}

// PasskeyCeremonyResponse carries the WebAuthn options for the device and the
// ID that finishing must quote back.
type PasskeyCeremonyResponse struct {
	ChallengeID string `json:"challengeId"`
	PublicKey   any    `json:"publicKey"`
}

// PasskeyFinishRequest returns the device's WebAuthn credential, encoded as
// the browser's PublicKeyCredential JSON (base64url fields).
type PasskeyFinishRequest struct {
	ChallengeID string          `json:"challengeId"`
	Credential  json.RawMessage `json:"credential"`
}

func (a *API) passkeyRegisterBegin(c *echo.Context) error {
	ctx := c.Request().Context()
	if !a.service.PasskeyEnabled() {
		return httpx.Fail(apperr.ErrNotFound, i18n.T(ctx, "auth.api.passkeys"))
	}
	var req PasskeyRegisterBeginRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: maxCeremonyBytes}, i18n.T(ctx, "auth.api.body.passkey")); err != nil {
		return err
	}
	ceremony, err := a.service.PasskeyRegisterBegin(ctx, PasskeyRegisterBeginInput(req))
	if err != nil {
		return apiPasskeyError(c, err)
	}
	return c.JSON(http.StatusOK, PasskeyCeremonyResponse(ceremony))
}

func (a *API) passkeyRegisterFinish(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	if !a.service.PasskeyEnabled() {
		return httpx.Fail(apperr.ErrNotFound, i18n.T(ctx, "auth.api.passkeys"))
	}
	var req PasskeyFinishRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: maxCeremonyBytes}, i18n.T(ctx, "auth.api.body.credential")); err != nil {
		return err
	}
	user, token, err := a.service.PasskeyRegisterFinish(ctx, PasskeyRegisterFinishInput(req), a.mw.RequestMetadata(r))
	if err != nil {
		return apiPasskeyError(c, err)
	}
	return a.writeSession(c, http.StatusCreated, user.ID.String(), token)
}

func (a *API) passkeyLoginBegin(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	if !a.service.PasskeyEnabled() {
		return httpx.Fail(apperr.ErrNotFound, i18n.T(ctx, "auth.api.passkeys"))
	}
	var req PasskeyLoginBeginRequest
	// An empty body is a discoverable login.
	if r.ContentLength != 0 {
		if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: maxCeremonyBytes}, i18n.T(ctx, "auth.api.body.passkey")); err != nil {
			return err
		}
	}
	ceremony, err := a.service.PasskeyLoginBegin(ctx, PasskeyLoginBeginInput(req))
	if err != nil {
		return apiPasskeyError(c, err)
	}
	return c.JSON(http.StatusOK, PasskeyCeremonyResponse(ceremony))
}

func (a *API) passkeyLoginFinish(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	if !a.service.PasskeyEnabled() {
		return httpx.Fail(apperr.ErrNotFound, i18n.T(ctx, "auth.api.passkeys"))
	}
	var req PasskeyFinishRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: maxCeremonyBytes}, i18n.T(ctx, "auth.api.body.credential")); err != nil {
		return err
	}
	user, token, err := a.service.PasskeyLoginFinish(ctx, PasskeyLoginFinishInput(req), a.mw.RequestMetadata(r))
	if err != nil {
		return apiPasskeyError(c, err)
	}
	return a.writeSession(c, http.StatusOK, user.ID.String(), token)
}

// writeSession answers a finished sign-in with the token, its expiry and the
// user, the same AuthResponse every other sign-in route returns.
func (a *API) writeSession(c *echo.Context, status int, userID, token string) error {
	ctx := c.Request().Context()
	session, err := a.sessions.Resolve(ctx, token)
	if err != nil {
		middleware.FromContext(ctx).Error("resolve new passkey session", slog.String("user_id", userID), slog.Any("error", err))
		return httpx.Fail(apperr.ErrUnavailable, i18n.T(ctx, "auth.api.generic"))
	}
	return c.JSON(status, AuthResponse{Token: token, ExpiresAt: session.ExpiresAt, User: ProjectUser(session.User)})
}

// apiPasskeyError maps ceremony failures to the API's error shape. The
// messages match the web ceremony's so both clients say the same thing.
func apiPasskeyError(c *echo.Context, err error) error {
	ctx := c.Request().Context()
	var fields apperr.FieldErrors
	switch {
	case apperr.As(err, &fields):
		return httpx.Fail(err, i18n.T(ctx, "auth.api.fields"))
	case apperr.Is(err, ErrInvalidCredentials):
		return c.JSON(http.StatusUnauthorized, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: i18n.T(ctx, "auth.err.passkey.failed")}})
	case err.Error() == "invalid or expired passkey challenge" || err.Error() == "invalid passkey ceremony":
		return c.JSON(http.StatusBadRequest, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: i18n.T(ctx, "auth.err.passkey.expired")}})
	default:
		middleware.FromContext(ctx).Error("passkey failed", slog.Any("error", err))
		return httpx.Fail(apperr.ErrUnavailable, i18n.T(ctx, "auth.err.passkey.generic"))
	}
}
