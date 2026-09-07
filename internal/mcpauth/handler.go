package mcpauth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
)

// registrationsPerMinute bounds the one public write in this package.
//
// Open registration is what makes the flow one URL, and an unbounded public
// insert is what would make that a mistake. Generous enough that a person
// re-adding a connector never notices, small enough that farming rows costs
// somebody a rate limit rather than a table.
const registrationsPerMinute = 10

// maxRegistrationBody caps /oauth/register. A registration is a handful of
// short strings; anything larger is not a registration.
const maxRegistrationBody = 16 << 10

// MachineHandler serves the endpoints a client talks to without a person
// present: discovery, registration, token exchange and revocation.
//
// Separate from the browser handler because the two need opposite middleware.
// These carry no cookie and need permissive CORS so a browser-based client can
// call them from its own page; the consent screen needs the session, the
// locale and CSRF. Mounting them together would mean one of the two getting
// the wrong treatment.
type MachineHandler struct {
	svc   *Service
	log   *slog.Logger
	limit *ratelimit.Limiters

	trustedProxies middleware.TrustedProxies
}

func NewMachineHandler(svc *Service, log *slog.Logger, proxies middleware.TrustedProxies) *MachineHandler {
	if log == nil {
		log = slog.Default()
	}
	return &MachineHandler{
		svc:            svc,
		log:            log,
		limit:          ratelimit.New(registrationsPerMinute),
		trustedProxies: proxies,
	}
}

// Routes mounts the machine-facing endpoints.
//
// All of them are public, outside the session and CSRF group: a client
// fetching discovery has no session, and a token request authenticates with a
// code rather than a cookie.
func (h *MachineHandler) Routes(r chi.Router) {
	// RFC 9728 puts the resource's path component into the well-known path, so
	// a client resolving /mcp asks for the /mcp-suffixed one. The bare path is
	// served too, because several clients still ask for that.
	r.Get("/.well-known/oauth-protected-resource", h.cors(h.protectedResource))
	r.Get("/.well-known/oauth-protected-resource/mcp", h.cors(h.protectedResource))
	r.Get("/.well-known/oauth-authorization-server", h.cors(h.authorizationServer))

	r.Post("/oauth/register", h.cors(h.register))
	r.Post("/oauth/token", h.cors(h.token))
	r.Post("/oauth/revoke", h.cors(h.revoke))

	// A browser-based client preflights each of the above before calling it.
	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-authorization-server",
		"/oauth/register",
		"/oauth/token",
		"/oauth/revoke",
	} {
		r.Options(path, h.preflight)
	}
}

// cors makes an endpoint callable from a browser-based client's own page.
//
// Claude's web client performs discovery and the token exchange from the page
// rather than from a server, so without these headers it fails with nothing
// visible in the response. None of these endpoints reads a cookie, which is
// what makes a wildcard origin safe here — and why
// Access-Control-Allow-Credentials must never be set: it is illegal with "*"
// and would turn a public endpoint into one that acts as the visitor.
func (h *MachineHandler) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		next(w, r)
	}
}

func (h *MachineHandler) preflight(w http.ResponseWriter, _ *http.Request) {
	setCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
	w.Header().Set("Access-Control-Max-Age", "3600")
}

func (h *MachineHandler) protectedResource(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, h.svc.ProtectedResource())
}

func (h *MachineHandler) authorizationServer(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, h.svc.AuthorizationServer())
}

// registrationRequest is the RFC 7591 body.
//
// Unknown fields are allowed. Clients send metadata this server has no use for
// — client_uri, logo_uri, contacts, tos_uri — and refusing a registration for
// carrying a field the specification defines would fail every real client to
// no benefit.
type registrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	SoftwareID              string   `json:"software_id"`
	SoftwareVersion         string   `json:"software_version"`
}

type registrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

func (h *MachineHandler) register(w http.ResponseWriter, r *http.Request) {
	if !h.limit.Allow(middleware.ClientIP(r, h.trustedProxies)) {
		w.Header().Set("Retry-After", "60")
		h.writeOAuthError(w, http.StatusTooManyRequests,
			"temporarily_unavailable", "too many registrations from this address")
		return
	}

	var in registrationRequest
	if err := httpx.ReadJSON(w, r, &in, httpx.ReadOptions{
		MaxBytes:           maxRegistrationBody,
		AllowUnknownFields: true,
	}); err != nil {
		h.writeOAuthError(w, http.StatusBadRequest,
			"invalid_client_metadata", "the registration body could not be read")
		return
	}

	client, err := h.svc.RegisterClient(r.Context(), Registration{
		ClientName:              in.ClientName,
		RedirectURIs:            in.RedirectURIs,
		GrantTypes:              in.GrantTypes,
		SoftwareID:              in.SoftwareID,
		TokenEndpointAuthMethod: in.TokenEndpointAuthMethod,
	})
	if err != nil {
		// The message is the client's own metadata reflected back, never
		// server state, so it is safe to be specific: the caller is a program
		// that can act on "redirect_uri must use https".
		status := http.StatusBadRequest
		if apperr.Is(err, apperr.ErrUnavailable) {
			status = http.StatusTooManyRequests
		}
		h.writeOAuthError(w, status, "invalid_client_metadata", err.Error())
		return
	}

	h.log.Info("mcp oauth client registered",
		slog.String("client_id", client.ID),
		slog.String("client_name", client.Name))

	httpx.WriteJSON(w, http.StatusCreated, registrationResponse{
		ClientID:         client.ID,
		ClientIDIssuedAt: client.CreatedAt.Unix(),
		ClientName:       client.Name,
		RedirectURIs:     client.RedirectURIs,
		GrantTypes:       client.GrantTypes,
		ResponseTypes:    []string{"code"},

		// Echoed so a client can see it was registered as public. There is no
		// client_secret in this response, and that is not an omission.
		TokenEndpointAuthMethod: "none",
	})
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// token is the RFC 6749 token endpoint.
//
// Form-encoded, not JSON. Every client posts application/x-www-form-urlencoded
// here, and a JSON-only decoder would fail all of them.
func (h *MachineHandler) token(w http.ResponseWriter, r *http.Request) {
	// No-store on every reply, success or failure: the body carries a bearer
	// token, and a cache between here and the client holding a copy is exactly
	// what RFC 6749 section 5.1 forbids.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	if err := r.ParseForm(); err != nil {
		h.writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the form body could not be read")
		return
	}

	switch grant := r.PostFormValue("grant_type"); grant {
	case "authorization_code":
		h.exchangeCode(w, r)
	case "refresh_token":
		h.refresh(w, r)
	default:
		h.writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type",
			"only the authorization_code and refresh_token grants are supported")
	}
}

func (h *MachineHandler) exchangeCode(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.svc.ExchangeCode(r.Context(), CodeExchange{
		Code:         r.PostFormValue("code"),
		ClientID:     r.PostFormValue("client_id"),
		RedirectURI:  r.PostFormValue("redirect_uri"),
		CodeVerifier: r.PostFormValue("code_verifier"),
		Resource:     r.PostFormValue("resource"),
	})
	if err != nil {
		h.writeGrantError(w, r, err)
		return
	}
	h.writeTokens(w, tokens)
}

func (h *MachineHandler) refresh(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.svc.Refresh(r.Context(), RefreshExchange{
		RefreshToken: r.PostFormValue("refresh_token"),
		ClientID:     r.PostFormValue("client_id"),
		Scope:        r.PostFormValue("scope"),
	})
	if err != nil {
		h.writeGrantError(w, r, err)
		return
	}
	h.writeTokens(w, tokens)
}

func (h *MachineHandler) writeTokens(w http.ResponseWriter, tokens Tokens) {
	httpx.WriteJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  tokens.AccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    tokens.ExpiresIn,
		RefreshToken: tokens.RefreshToken,
		Scope:        tokens.Scope,
	})
}

// revoke is RFC 7009.
//
// Always 200, whether or not the token existed. The specification says so, and
// the reason is the same one behind the identical 401 on /mcp: a different
// answer for an unknown token confirms which tokens are real.
//
// Without this endpoint, removing a connector inside Claude would leave a live
// grant that only North's own settings page could turn off.
func (h *MachineHandler) revoke(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	token := strings.TrimSpace(r.PostFormValue("token"))
	if token != "" {
		if err := h.svc.RevokeToken(r.Context(), token); err != nil &&
			!apperr.Is(err, apperr.ErrNotFound) && !apperr.Is(err, apperr.ErrUnauthenticated) {
			// Logged, not reported. The caller gets 200 either way, and an
			// operator needs to know a revocation failed for a real reason.
			h.log.Error("mcp oauth revocation failed", slog.Any("error", err))
		}
	}

	w.WriteHeader(http.StatusOK)
}

// oauthError is the RFC 6749 section 5.2 error shape.
type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func (h *MachineHandler) writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, status, oauthError{Error: code, ErrorDescription: description})
}

// writeGrantError reports a token-endpoint refusal.
//
// Every refusal is invalid_grant with the same status, whatever the cause:
// unknown, expired, replayed, wrong client and wrong verifier are one answer,
// for the reason the 401 on /mcp is one answer. The specific description goes
// to the log instead, where an operator can see it and a caller cannot.
func (h *MachineHandler) writeGrantError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrInvalidTarget) {
		h.writeOAuthError(w, http.StatusBadRequest, "invalid_target",
			"this authorization server does not issue tokens for that resource")
		return
	}

	if apperr.Is(err, apperr.ErrUnauthenticated) {
		middleware.FromContext(r.Context()).Info("mcp oauth grant refused", slog.Any("error", err))
		h.writeOAuthError(w, http.StatusBadRequest, "invalid_grant",
			"the grant is not valid")
		return
	}

	h.log.Error("mcp oauth token endpoint failed", slog.Any("error", err))
	h.writeOAuthError(w, http.StatusInternalServerError, "server_error", "")
}
