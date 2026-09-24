package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/users"
)

// SessionResolver is the small session boundary needed by bearer API routes.
// Keeping it narrow lets the JSON contract be tested without a database.
type SessionResolver interface {
	Resolve(context.Context, string) (Session, error)
}

// API exposes authentication-adjacent JSON projections. Credential mutations
// are added in the next slice; /me is the tracer bullet for the native client.
type API struct {
	sessions SessionResolver
	service  *Service
	mw       *Middleware
}

func NewAPI(sessions SessionResolver) *API {
	return &API{sessions: sessions}
}

// WithAuthService enables credential routes on the API while keeping the
// bearer projection independently testable.
func (a *API) WithAuthService(service *Service, mw *Middleware) *API {
	a.service = service
	a.mw = mw
	return a
}

// Routes mounts routes relative to /api/v1.
func (a *API) Routes(r chi.Router) {
	r.Get("/me", a.me)
	if a.service == nil || a.mw == nil {
		return
	}
	r.Post("/auth/signup", a.signup)
	r.Post("/auth/login", a.login)
	r.Post("/auth/google", a.google)
	r.Post("/auth/apple", a.apple)
	r.Post("/auth/logout", a.logout)
	r.Post("/auth/forgot-password", a.forgotPassword)
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type SignupRequest struct {
	Email                string `json:"email"`
	Password             string `json:"password"`
	PasswordConfirmation string `json:"passwordConfirmation"`
	DisplayName          string `json:"displayName"`
	Timezone             string `json:"timezone"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

type GoogleRequest struct {
	IDToken string `json:"idToken"`
}

type AppleRequest struct {
	IdentityToken     string `json:"identityToken"`
	AuthorizationCode string `json:"authorizationCode"`
	Nonce             string `json:"nonce"`
	FullName          string `json:"fullName"`
	Email             string `json:"email"`
}

type AuthResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	User      APIUser   `json:"user"`
}

// APIUser is the public account projection. Sensitive persistence fields are
// intentionally not part of this type.
type APIUser struct {
	ID              uuid.UUID `json:"id"`
	Email           string    `json:"email"`
	DisplayName     string    `json:"displayName"`
	Timezone        string    `json:"timezone"`
	NeedsOnboarding bool      `json:"needsOnboarding"`
}

type MeResponse struct {
	User APIUser `json:"user"`
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		httpx.Error(w, apperr.ErrUnauthenticated, "A bearer token is required.")
		return
	}

	session, err := a.sessions.Resolve(r.Context(), token)
	if err != nil {
		if apperr.Is(err, apperr.ErrUnauthenticated) || apperr.Is(err, apperr.ErrNotFound) {
			httpx.Error(w, apperr.ErrUnauthenticated, "That token is not valid.")
			return
		}
		httpx.Error(w, apperr.ErrUnavailable, "Something went wrong.")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, MeResponse{User: ProjectUser(session.User)})
}

func (a *API) signup(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 64 << 10}); err != nil {
		httpx.Error(w, err, "The request body must be a valid signup request.")
		return
	}

	user, token, err := a.service.Signup(r.Context(), SignupInput{
		Email:                req.Email,
		Password:             req.Password,
		PasswordConfirmation: req.PasswordConfirmation,
		DisplayName:          req.DisplayName,
		Timezone:             req.Timezone,
	}, a.mw.RequestMetadata(r))
	if err != nil {
		a.writeAuthError(w, err, "That account could not be created.")
		return
	}

	// The API returns the token; it does not set the browser cookie.
	session, err := a.sessions.Resolve(r.Context(), token)
	if err != nil {
		a.writeAuthError(w, err, "Something went wrong.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, AuthResponse{Token: token, ExpiresAt: session.ExpiresAt, User: ProjectUser(user)})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 64 << 10}); err != nil {
		httpx.Error(w, err, "The request body must be a valid login request.")
		return
	}

	user, token, err := a.service.Login(r.Context(), LoginInput(req), a.mw.RequestMetadata(r))
	if err != nil {
		a.writeAuthError(w, err, "Email or password is incorrect.")
		return
	}

	session, err := a.sessions.Resolve(r.Context(), token)
	if err != nil {
		a.writeAuthError(w, err, "Something went wrong.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, AuthResponse{Token: token, ExpiresAt: session.ExpiresAt, User: ProjectUser(user)})
}

func (a *API) google(w http.ResponseWriter, r *http.Request) {
	var req GoogleRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body must contain a Google ID token.")
		return
	}
	user, token, expiresAt, err := a.service.CompleteGoogleIDToken(r.Context(), req.IDToken, a.mw.RequestMetadata(r))
	if err != nil {
		a.writeAuthError(w, err, "Google sign-in could not be completed.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, AuthResponse{Token: token, ExpiresAt: expiresAt, User: ProjectUser(user)})
}

func (a *API) apple(w http.ResponseWriter, r *http.Request) {
	var req AppleRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 32 << 10}); err != nil {
		httpx.Error(w, err, "The request body must contain an Apple identity token.")
		return
	}
	user, token, expiresAt, err := a.service.CompleteAppleSignIn(r.Context(), AppleSignInInput{
		IdentityToken: req.IdentityToken,
		Nonce:         req.Nonce,
		FullName:      req.FullName,
		Email:         req.Email,
	}, a.mw.RequestMetadata(r))
	if err != nil {
		a.writeAuthError(w, err, "Apple sign-in could not be completed.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, AuthResponse{Token: token, ExpiresAt: expiresAt, User: ProjectUser(user)})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		httpx.Error(w, apperr.ErrUnauthenticated, "A bearer token is required.")
		return
	}
	if err := a.service.Logout(r.Context(), token); err != nil {
		a.writeAuthError(w, err, "Something went wrong signing you out.")
		return
	}
	httpx.WriteJSON(w, http.StatusNoContent, nil)
}

func (a *API) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body must contain an email address.")
		return
	}
	if err := a.service.RequestPasswordReset(r.Context(), req.Email); err != nil {
		a.writeAuthError(w, err, "The password reset request could not be processed.")
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, nil)
}

func (a *API) writeAuthError(w http.ResponseWriter, err error, message string) {
	if apperr.Is(err, ErrInvalidCredentials) {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: message}})
		return
	}
	httpx.Error(w, err, message)
}

func ProjectUser(user users.User) APIUser {
	return APIUser{
		ID:              user.ID,
		Email:           user.Email,
		DisplayName:     user.DisplayName,
		Timezone:        user.Timezone,
		NeedsOnboarding: user.NeedsOnboarding(),
	}
}

func bearerToken(r *http.Request) (string, bool) {
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	return token, found && token != ""
}
