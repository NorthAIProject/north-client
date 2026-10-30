package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
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
	throttle *Throttle
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

// WithThrottle bounds sign-in, sign-up and password-reset attempts against
// one account. Nil leaves them unbounded, which is the test wiring.
func (a *API) WithThrottle(t *Throttle) *API {
	a.throttle = t
	return a
}

// throttled answers an attempt the account's allowance has no room for.
func throttled(c *echo.Context) error {
	c.Response().Header().Set("Retry-After", "60")
	return httpx.Fail(httpx.ErrRateLimited, i18n.T(c.Request().Context(), "auth.err.throttled"))
}

// RegisterPublic registers the credential routes relative to /api/v1. They
// run before a session exists, so they sit outside Bearer.
func (a *API) RegisterPublic(g *echo.Group) {
	if a.service == nil || a.mw == nil {
		return
	}
	g.POST("/auth/signup", a.signup)
	g.POST("/auth/login", a.login)
	g.POST("/auth/google", a.google)
	g.POST("/auth/apple", a.apple)
	g.POST("/auth/logout", a.logout)
	g.POST("/auth/forgot-password", a.forgotPassword)
	g.POST("/auth/reset-password", a.resetPassword)
	g.GET("/auth/options", a.options)
	g.POST("/auth/google/start", a.googleStart)
	g.POST("/auth/google/code", a.googleCode)
	g.POST("/auth/passkey/register/begin", a.passkeyRegisterBegin)
	g.POST("/auth/passkey/register/finish", a.passkeyRegisterFinish)
	g.POST("/auth/passkey/login/begin", a.passkeyLoginBegin)
	g.POST("/auth/passkey/login/finish", a.passkeyLoginFinish)
}

// Register registers the signed-in routes relative to /api/v1, on a group
// behind Bearer.
func (a *API) Register(g *echo.Group) {
	g.GET("/me", a.me)
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
	// Locale and Appearance let a web client render its first byte in the
	// account's language and mode, as LoadUser does for the Go pages.
	Locale     string `json:"locale,omitempty"`
	Appearance string `json:"appearance,omitempty"`
}

type MeResponse struct {
	User APIUser `json:"user"`
}

func (a *API) me(c *echo.Context) error {
	return c.JSON(http.StatusOK, MeResponse{User: ProjectUser(MustUser(c.Request().Context()))})
}

func (a *API) signup(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	var req SignupRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 64 << 10}, i18n.T(ctx, "auth.api.body.signup")); err != nil {
		return err
	}
	if !a.throttle.AllowAccount(c.Request(), req.Email) {
		return throttled(c)
	}

	user, token, err := a.service.Signup(ctx, SignupInput{
		Email:                req.Email,
		Password:             req.Password,
		PasswordConfirmation: req.PasswordConfirmation,
		DisplayName:          req.DisplayName,
		Timezone:             req.Timezone,
	}, a.mw.RequestMetadata(r))
	if err != nil {
		return authError(c, err, i18n.T(ctx, "auth.api.signup"))
	}

	// The API returns the token; it does not set the browser cookie.
	session, err := a.sessions.Resolve(ctx, token)
	if err != nil {
		return authError(c, err, i18n.T(ctx, "auth.api.generic"))
	}
	return c.JSON(http.StatusCreated, AuthResponse{Token: token, ExpiresAt: session.ExpiresAt, User: ProjectUser(user)})
}

func (a *API) login(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	var req LoginRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 64 << 10}, i18n.T(ctx, "auth.api.body.login")); err != nil {
		return err
	}
	if !a.throttle.AllowAccount(c.Request(), req.Email) {
		return throttled(c)
	}

	user, token, err := a.service.Login(ctx, LoginInput(req), a.mw.RequestMetadata(r))
	if err != nil {
		return authError(c, err, i18n.T(ctx, "auth.err.credentials"))
	}

	session, err := a.sessions.Resolve(ctx, token)
	if err != nil {
		return authError(c, err, i18n.T(ctx, "auth.api.generic"))
	}
	return c.JSON(http.StatusOK, AuthResponse{Token: token, ExpiresAt: session.ExpiresAt, User: ProjectUser(user)})
}

func (a *API) google(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	var req GoogleRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 16 << 10}, i18n.T(ctx, "auth.api.body.google")); err != nil {
		return err
	}
	user, token, expiresAt, err := a.service.CompleteGoogleIDToken(ctx, req.IDToken, a.mw.RequestMetadata(r))
	if err != nil {
		return authError(c, err, i18n.T(ctx, "auth.api.google"))
	}
	return c.JSON(http.StatusOK, AuthResponse{Token: token, ExpiresAt: expiresAt, User: ProjectUser(user)})
}

func (a *API) apple(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	var req AppleRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 32 << 10}, i18n.T(ctx, "auth.api.body.apple")); err != nil {
		return err
	}
	user, token, expiresAt, err := a.service.CompleteAppleSignIn(ctx, AppleSignInInput{
		IdentityToken: req.IdentityToken,
		Nonce:         req.Nonce,
		FullName:      req.FullName,
		Email:         req.Email,
	}, a.mw.RequestMetadata(r))
	if err != nil {
		return authError(c, err, i18n.T(ctx, "auth.api.apple"))
	}
	return c.JSON(http.StatusOK, AuthResponse{Token: token, ExpiresAt: expiresAt, User: ProjectUser(user)})
}

func (a *API) logout(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	token, ok := bearerToken(r)
	if !ok {
		return httpx.Fail(apperr.ErrUnauthenticated, i18n.T(ctx, "auth.api.bearer"))
	}
	if err := a.service.Logout(ctx, token); err != nil {
		return authError(c, err, i18n.T(ctx, "auth.api.signout"))
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) forgotPassword(c *echo.Context) error {
	ctx := c.Request().Context()
	var req ForgotPasswordRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 16 << 10}, i18n.T(ctx, "auth.api.body.email")); err != nil {
		return err
	}
	if !a.throttle.AllowAccount(c.Request(), req.Email) {
		return throttled(c)
	}
	if err := a.service.RequestPasswordReset(ctx, req.Email); err != nil {
		return authError(c, err, i18n.T(ctx, "auth.api.reset"))
	}
	return c.JSON(http.StatusAccepted, nil)
}

// authError answers a credential failure. Wrong credentials are a 401 with
// the endpoint's message; anything else is mapped as usual.
func authError(c *echo.Context, err error, message string) error {
	if apperr.Is(err, ErrInvalidCredentials) {
		return c.JSON(http.StatusUnauthorized, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: message}})
	}
	return httpx.Fail(err, message)
}

func ProjectUser(user users.User) APIUser {
	return APIUser{
		ID:              user.ID,
		Email:           user.Email,
		DisplayName:     user.DisplayName,
		Timezone:        user.Timezone,
		NeedsOnboarding: user.NeedsOnboarding(),
		Locale:          string(user.Locale),
		Appearance:      string(user.Appearance),
	}
}
