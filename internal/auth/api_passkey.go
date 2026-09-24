package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Passkeys for native clients. The ceremony is the one the browser runs,
// against the same Service methods and the same stored challenges; only the
// ends differ. Requests and responses use the API's camelCase, errors use
// ErrorBody, and finishing returns a session token instead of setting a
// cookie.

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

func (a *API) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !a.service.PasskeyEnabled() {
		httpx.Error(w, apperr.ErrNotFound, "Passkeys are not available.")
		return
	}
	var req PasskeyRegisterBeginRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: maxCeremonyBytes}); err != nil {
		httpx.Error(w, err, "The request body must be a valid passkey request.")
		return
	}
	ceremony, err := a.service.PasskeyRegisterBegin(r.Context(), PasskeyRegisterBeginInput(req))
	if err != nil {
		writeAPIPasskeyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PasskeyCeremonyResponse(ceremony))
}

func (a *API) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !a.service.PasskeyEnabled() {
		httpx.Error(w, apperr.ErrNotFound, "Passkeys are not available.")
		return
	}
	var req PasskeyFinishRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: maxCeremonyBytes}); err != nil {
		httpx.Error(w, err, "The request body must be a valid passkey credential.")
		return
	}
	user, token, err := a.service.PasskeyRegisterFinish(r.Context(), PasskeyRegisterFinishInput(req), a.mw.RequestMetadata(r))
	if err != nil {
		writeAPIPasskeyError(w, r, err)
		return
	}
	a.writeSession(w, r, http.StatusCreated, user.ID.String(), token)
}

func (a *API) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !a.service.PasskeyEnabled() {
		httpx.Error(w, apperr.ErrNotFound, "Passkeys are not available.")
		return
	}
	var req PasskeyLoginBeginRequest
	// An empty body is a discoverable login.
	if r.ContentLength != 0 {
		if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: maxCeremonyBytes}); err != nil {
			httpx.Error(w, err, "The request body must be a valid passkey request.")
			return
		}
	}
	ceremony, err := a.service.PasskeyLoginBegin(r.Context(), PasskeyLoginBeginInput(req))
	if err != nil {
		writeAPIPasskeyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PasskeyCeremonyResponse(ceremony))
}

func (a *API) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !a.service.PasskeyEnabled() {
		httpx.Error(w, apperr.ErrNotFound, "Passkeys are not available.")
		return
	}
	var req PasskeyFinishRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: maxCeremonyBytes}); err != nil {
		httpx.Error(w, err, "The request body must be a valid passkey credential.")
		return
	}
	user, token, err := a.service.PasskeyLoginFinish(r.Context(), PasskeyLoginFinishInput(req), a.mw.RequestMetadata(r))
	if err != nil {
		writeAPIPasskeyError(w, r, err)
		return
	}
	a.writeSession(w, r, http.StatusOK, user.ID.String(), token)
}

// writeSession answers a finished sign-in with the token, its expiry and the
// user, the same AuthResponse every other sign-in route returns.
func (a *API) writeSession(w http.ResponseWriter, r *http.Request, status int, userID, token string) {
	session, err := a.sessions.Resolve(r.Context(), token)
	if err != nil {
		middleware.FromContext(r.Context()).Error("resolve new passkey session", slog.String("user_id", userID), slog.Any("error", err))
		httpx.Error(w, apperr.ErrUnavailable, "Something went wrong.")
		return
	}
	httpx.WriteJSON(w, status, AuthResponse{Token: token, ExpiresAt: session.ExpiresAt, User: ProjectUser(session.User)})
}

// writeAPIPasskeyError maps ceremony failures to the API's error shape. The
// messages match the web ceremony's so both clients say the same thing.
func writeAPIPasskeyError(w http.ResponseWriter, r *http.Request, err error) {
	var fields apperr.FieldErrors
	switch {
	case apperr.As(err, &fields):
		httpx.Error(w, err, "Please check the highlighted fields.")
	case apperr.Is(err, ErrInvalidCredentials):
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: "Passkey sign-in failed."}})
	case err.Error() == "invalid or expired passkey challenge" || err.Error() == "invalid passkey ceremony":
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: "That passkey step expired. Please try again."}})
	default:
		middleware.FromContext(r.Context()).Error("passkey failed", slog.Any("error", err))
		httpx.Error(w, apperr.ErrUnavailable, "Something went wrong with the passkey. Please try again.")
	}
}
