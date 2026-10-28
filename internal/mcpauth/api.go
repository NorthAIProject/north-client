package mcpauth

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// API is the consent screen's server half, for a web front end that renders
// the screen itself (the SvelteKit app). It is BrowserHandler with the HTML
// taken out: the same Service calls, the same account delegation to
// internal/auth, the same funnel events.
//
// The front end owns the north_oauth_req cookie. It receives the verifier
// once, from begin, keeps it HttpOnly on /oauth, and quotes it back on every
// call: a wrong verifier and an unknown request are the same "expired"
// answer, exactly as on the Go page.
//
// The machine half (/oauth/token, /oauth/register, discovery) is not here: it
// is MachineHandler's, and a client talks to it directly.
type API struct {
	svc        *Service
	auth       *auth.Service
	authMW     *auth.Middleware
	onboarding *onboarding.Service
	log        *slog.Logger
	funnel     *analytics.Funnel
}

// NewAPI builds the consent routes. Every argument may be nil in a routing
// test, where no handler body runs.
func NewAPI(svc *Service, authSvc *auth.Service, authMW *auth.Middleware, onboardingSvc *onboarding.Service, log *slog.Logger) *API {
	if log == nil {
		log = slog.Default()
	}
	return &API{svc: svc, auth: authSvc, authMW: authMW, onboarding: onboardingSvc, log: log}
}

// WithFunnel attaches product analytics.
func (a *API) WithFunnel(f *analytics.Funnel) *API {
	a.funnel = f
	return a
}

// PublicRoutes run before anybody is signed in: parking the request, showing
// it, and creating or signing in to the account on the screen. Mount them
// outside RequireBearer.
func (a *API) PublicRoutes(r chi.Router) {
	r.Post("/auth/oauth/authorize", a.begin)
	r.Get("/auth/oauth/requests/{requestID}", a.show)
	r.Post("/auth/oauth/requests/{requestID}/account", a.account)
}

// Routes are the decision, which needs the person. Mount behind RequireBearer.
func (a *API) Routes(r chi.Router) {
	r.Post("/oauth/requests/{requestID}/approve", a.approve)
	r.Post("/oauth/requests/{requestID}/deny", a.deny)
}

// VerifierHeader carries the north_oauth_req cookie's value on a read.
const VerifierHeader = "X-OAuth-Request-Verifier"

// AuthorizeRequest is an authorization request as it arrived on the
// /oauth/authorize query string, plus whether the browser had a session.
type AuthorizeRequest struct {
	ClientID            string `json:"clientId"`
	RedirectURI         string `json:"redirectUri"`
	ResponseType        string `json:"responseType"`
	State               string `json:"state"`
	Scope               string `json:"scope"`
	CodeChallenge       string `json:"codeChallenge"`
	CodeChallengeMethod string `json:"codeChallengeMethod"`
	Resource            string `json:"resource"`
	// SignedIn is for the funnel only; it grants nothing.
	SignedIn bool `json:"signedIn"`
}

// AuthorizeResponse is either a parked request with its verifier, or — when
// the request was bad in a way the client should hear about — the client's
// callback to send the browser to. A request too bad to redirect anywhere is
// a 400 instead, with the reason to show.
type AuthorizeResponse struct {
	Request  *ConsentRequest `json:"request,omitempty"`
	Verifier string          `json:"verifier,omitempty"`
	Redirect string          `json:"redirect,omitempty"`
}

// ConsentRequest is what the consent screen renders from.
type ConsentRequest struct {
	ID string `json:"id"`
	// ClientName is chosen by whoever registered the client: show it escaped,
	// and always beside RedirectHost, which the client cannot choose.
	ClientName        string    `json:"clientName"`
	RedirectHost      string    `json:"redirectHost"`
	IsLoopback        bool      `json:"isLoopback"`
	ReadOnlyRequested bool      `json:"readOnlyRequested"`
	AccountCreated    bool      `json:"accountCreated"`
	ExpiresAt         time.Time `json:"expiresAt"`
}

// ConsentAccountRequest creates an account (mode signup) or signs in (mode
// login) without leaving the screen.
type ConsentAccountRequest struct {
	Verifier    string `json:"verifier"`
	Mode        string `json:"mode"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
	Timezone    string `json:"timezone"`
}

// ConsentDecisionRequest is Approve or Cancel. ReadOnly may narrow the grant
// to read access; nothing posted can widen it.
type ConsentDecisionRequest struct {
	Verifier string `json:"verifier"`
	ReadOnly bool   `json:"readOnly"`
}

// ConsentDecisionResponse is the client's callback, with the code or the
// refusal on it.
type ConsentDecisionResponse struct {
	Redirect string `json:"redirect"`
}

func projectRequest(req Request) *ConsentRequest {
	return &ConsentRequest{
		ID:                req.ID.String(),
		ClientName:        req.ClientName,
		RedirectHost:      req.RedirectHost(),
		IsLoopback:        req.RedirectHost() == "this computer",
		ReadOnlyRequested: req.Scope == scopeRead(),
		AccountCreated:    req.AccountCreated,
		ExpiresAt:         req.ExpiresAt,
	}
}

func (a *API) begin(w http.ResponseWriter, r *http.Request) {
	var in AuthorizeRequest
	if err := httpx.ReadJSON(w, r, &in, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, i18n.T(r.Context(), "auth.err.form"))
		return
	}
	req, verifier, err := a.svc.BeginAuthorization(r.Context(), AuthorizeParams{
		ClientID:            in.ClientID,
		RedirectURI:         in.RedirectURI,
		ResponseType:        in.ResponseType,
		State:               in.State,
		Scope:               in.Scope,
		CodeChallenge:       in.CodeChallenge,
		CodeChallengeMethod: in.CodeChallengeMethod,
		Resource:            in.Resource,
	}, in.SignedIn)
	if err != nil {
		var fatal FatalAuthorizeError
		if apperr.As(err, &fatal) {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: fatal.Reason, Code: "oauth_fatal"}})
			return
		}
		var redirectable RedirectableError
		if apperr.As(err, &redirectable) {
			httpx.WriteJSON(w, http.StatusOK, AuthorizeResponse{Redirect: redirectable.URL()})
			return
		}
		a.log.Error("mcp oauth authorize failed", slog.Any("error", err))
		httpx.Error(w, err, "The authorization request could not be started.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, AuthorizeResponse{Request: projectRequest(req), Verifier: verifier})
}

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	req, ok := a.load(w, r, r.Header.Get(VerifierHeader))
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectRequest(req))
}

// account delegates to internal/auth, as BrowserHandler.account does: the
// validation, hashing, session and signup funnel event live there.
func (a *API) account(w http.ResponseWriter, r *http.Request) {
	var in ConsentAccountRequest
	if err := httpx.ReadJSON(w, r, &in, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, i18n.T(r.Context(), "auth.err.form"))
		return
	}
	req, ok := a.load(w, r, in.Verifier)
	if !ok {
		return
	}

	var (
		user  users.User
		token string
		err   error
	)
	fresh := in.Mode != "login"
	meta := a.authMW.RequestMetadata(r)
	if fresh {
		user, token, err = a.auth.Signup(r.Context(), auth.SignupInput{
			Email:       in.Email,
			DisplayName: in.DisplayName,
			Password:    in.Password,
			// One password on this screen, not two; see BrowserHandler.account.
			PasswordConfirmation: in.Password,
			Timezone:             in.Timezone,
			Via:                  analytics.ViaOAuthConsent,
		}, meta)
	} else {
		user, token, err = a.auth.Login(r.Context(), auth.LoginInput{Email: in.Email, Password: in.Password}, meta)
	}
	if err != nil {
		var fields apperr.FieldErrors
		switch {
		case apperr.As(err, &fields):
			httpx.Error(w, err, i18n.T(r.Context(), "auth.api.fields"))
		case apperr.Is(err, auth.ErrInvalidCredentials):
			httpx.WriteJSON(w, http.StatusUnauthorized, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: i18n.T(r.Context(), "auth.err.credentials")}})
		default:
			httpx.Error(w, err, i18n.T(r.Context(), "auth.api.generic"))
		}
		return
	}

	if fresh {
		// The smallest honest profile, and onboarded: see
		// onboarding.SeedForAgent. Logged, not fatal, as on the Go page.
		if seeded, seedErr := a.onboarding.SeedForAgent(r.Context(), user, req.ClientName); seedErr != nil {
			a.log.Error("could not seed an account created in consent",
				slog.Any("error", seedErr), slog.String("user_id", user.ID.String()))
		} else {
			user = seeded
		}
		if markErr := a.svc.MarkAccountCreated(r.Context(), req.ID, in.Verifier); markErr != nil {
			a.log.Warn("could not record that consent created the account", slog.Any("error", markErr))
		}
	}

	session, err := a.auth.Sessions().Resolve(r.Context(), token)
	if err != nil {
		a.log.Error("resolve consent session", slog.Any("error", err))
		httpx.Error(w, apperr.ErrUnavailable, i18n.T(r.Context(), "auth.api.generic"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, auth.AuthResponse{Token: token, ExpiresAt: session.ExpiresAt, User: auth.ProjectUser(session.User)})
}

func (a *API) approve(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	var in ConsentDecisionRequest
	if err := httpx.ReadJSON(w, r, &in, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, i18n.T(r.Context(), "auth.err.form"))
		return
	}
	req, ok := a.load(w, r, in.Verifier)
	if !ok {
		return
	}

	scope := ""
	if in.ReadOnly {
		scope = scopeRead()
	}
	redirect, err := a.svc.Approve(r.Context(), req.ID, in.Verifier, user.ID, scope)
	if err != nil {
		a.writeExpired(w, r)
		return
	}

	a.log.Info("mcp oauth consent approved",
		slog.String("client_id", req.ClientID),
		slog.Bool("account_created", req.AccountCreated))
	granted := req.Scope
	if scope != "" {
		granted = scope
	}
	a.funnel.MCPConsentApproved(r.Context(), user.ID, req.ClientName, granted, req.AccountCreated)
	httpx.WriteJSON(w, http.StatusOK, ConsentDecisionResponse{Redirect: redirect})
}

func (a *API) deny(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	var in ConsentDecisionRequest
	if err := httpx.ReadJSON(w, r, &in, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, i18n.T(r.Context(), "auth.err.form"))
		return
	}
	req, ok := a.load(w, r, in.Verifier)
	if !ok {
		return
	}
	redirect, err := a.svc.Deny(r.Context(), req.ID, in.Verifier)
	if err != nil {
		a.writeExpired(w, r)
		return
	}
	a.funnel.MCPConsentDenied(r.Context(), user.ID, req.ClientName)
	httpx.WriteJSON(w, http.StatusOK, ConsentDecisionResponse{Redirect: redirect})
}

// load resolves the path's request id against the verifier. Any failure is
// the one "expired" answer, so the response cannot tell a guessed id from a
// wrong cookie.
func (a *API) load(w http.ResponseWriter, r *http.Request, verifier string) (Request, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "requestID"))
	if err != nil || strings.TrimSpace(verifier) == "" {
		a.writeExpired(w, r)
		return Request{}, false
	}
	req, err := a.svc.LoadRequest(r.Context(), id, verifier)
	if err != nil {
		a.writeExpired(w, r)
		return Request{}, false
	}
	return req, true
}

func (a *API) writeExpired(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody{Error: httpx.ErrorDetail{
		Message: i18n.T(r.Context(), "oauth.error.expired"),
		Code:    "oauth_request_expired",
	}})
}
