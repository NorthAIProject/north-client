package mcpauth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	consent "github.com/NorthAIProject/north-client/web/mcpauth"
)

// requestCookie binds a parked authorization request to the browser that
// started it.
//
// Without it, a guessed request id lets somebody have a victim approve a
// request they started. Lax rather than Strict: the browser arrives here by a
// top-level redirect from the client, and Strict would withhold the cookie on
// exactly that navigation.
const requestCookie = "north_oauth_req"

// BrowserHandler serves the consent screen.
//
// Mounted inside the session and CSRF group, unlike everything in
// MachineHandler. The original plan put all of /oauth outside it; that is
// right for discovery and the token endpoint and wrong here, because this is a
// browser page that reads the session cookie, resolves a locale and renders a
// form with a CSRF token. Chi routes exact paths, so the two halves coexist.
type BrowserHandler struct {
	svc        *Service
	auth       *auth.Service
	authMW     *auth.Middleware
	onboarding *onboarding.Service
	log        *slog.Logger

	// funnel is nil-safe. Held here rather than reached through the service
	// because these two events belong to the screen, not to the flow.
	funnel *analytics.Funnel

	production bool
}

func NewBrowserHandler(
	svc *Service,
	authSvc *auth.Service,
	authMW *auth.Middleware,
	onboardingSvc *onboarding.Service,
	log *slog.Logger,
	production bool,
) *BrowserHandler {
	if log == nil {
		log = slog.Default()
	}
	return &BrowserHandler{
		svc:        svc,
		auth:       authSvc,
		authMW:     authMW,
		onboarding: onboardingSvc,
		log:        log,
		production: production,
	}
}

// WithFunnel attaches product analytics.
func (h *BrowserHandler) WithFunnel(f *analytics.Funnel) *BrowserHandler {
	h.funnel = f
	return h
}

func (h *BrowserHandler) Routes(r chi.Router) {
	r.Get("/oauth/authorize", h.frameGuard(h.authorize))
	r.Post("/oauth/authorize", h.frameGuard(h.decide))
	r.Post("/oauth/authorize/account", h.frameGuard(h.account))
	r.Get("/oauth/authorize/resume", h.frameGuard(h.resume))
}

// frameGuard stops the consent screen being framed.
//
// There is no CSP anywhere in this application yet, which means every page is
// framable — and a framable consent screen is a textbook clickjacking target:
// overlay an invisible iframe of this page under a "Play" button and a person
// approves a grant they never saw. The broader absence is out of scope; this
// page is the one that makes it exploitable, so it gets the headers.
func (h *BrowserHandler) frameGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		next(w, r)
	}
}

// authorize is where a client's browser redirect lands.
func (h *BrowserHandler) authorize(w http.ResponseWriter, r *http.Request) {
	// A request id means this is a re-render — the mode toggle, or a return
	// trip — rather than a fresh authorization request.
	if id := r.URL.Query().Get("request_id"); id != "" {
		h.renderExisting(w, r, id, r.URL.Query().Get("mode"))
		return
	}

	params := AuthorizeParams{
		ClientID:            r.URL.Query().Get("client_id"),
		RedirectURI:         r.URL.Query().Get("redirect_uri"),
		ResponseType:        r.URL.Query().Get("response_type"),
		State:               r.URL.Query().Get("state"),
		Scope:               r.URL.Query().Get("scope"),
		CodeChallenge:       r.URL.Query().Get("code_challenge"),
		CodeChallengeMethod: r.URL.Query().Get("code_challenge_method"),
		Resource:            r.URL.Query().Get("resource"),
	}

	_, signedIn := auth.UserFrom(r.Context())

	req, verifier, err := h.svc.BeginAuthorization(r.Context(), params, signedIn)
	if err != nil {
		h.writeAuthorizeError(w, r, err)
		return
	}

	h.setRequestCookie(w, verifier)
	h.render(w, r, req, "signup")
}

// resume re-renders the screen after Google or a passkey sent the browser away
// and back. Only the request id travels; every OAuth parameter stayed here.
func (h *BrowserHandler) resume(w http.ResponseWriter, r *http.Request) {
	h.renderExisting(w, r, r.URL.Query().Get("request_id"), "signup")
}

func (h *BrowserHandler) renderExisting(w http.ResponseWriter, r *http.Request, id, mode string) {
	req, ok := h.load(w, r, id)
	if !ok {
		return
	}
	h.render(w, r, req, mode)
}

// account creates or signs in to an account without leaving the screen.
//
// It delegates to internal/auth rather than reimplementing registration: the
// validation, the hashing, the session and the funnel event all already live
// there, and a second copy would drift.
func (h *BrowserHandler) account(w http.ResponseWriter, r *http.Request) {
	req, ok := h.load(w, r, r.URL.Query().Get("request_id"))
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderAccountError(w, r, req, "signup", "That form could not be read.", nil)
		return
	}

	mode := r.PostFormValue("mode")
	password := r.PostFormValue("password")

	var (
		user  users.User
		token string
		err   error
		fresh bool
	)

	if mode == "login" {
		user, token, err = h.auth.Login(r.Context(), auth.LoginInput{
			Email:    r.PostFormValue("email"),
			Password: password,
		}, h.authMW.RequestMetadata(r))
	} else {
		fresh = true
		user, token, err = h.auth.Signup(r.Context(), auth.SignupInput{
			Email:       r.PostFormValue("email"),
			DisplayName: r.PostFormValue("display_name"),
			Password:    password,

			// The screen asks for one password, not two. It is a conversion
			// surface with an agent waiting on the other side: a mistyped
			// password is recoverable by reset, and a mistyped email is not,
			// so the confirmation would be guarding the wrong field. Signup
			// requires the pair, so it gets the pair.
			PasswordConfirmation: password,

			Timezone: r.PostFormValue("timezone"),

			// The one number that decides whether this feature acquired
			// anybody or merely convenienced people who had already signed up.
			Via: analytics.ViaOAuthConsent,
		}, h.authMW.RequestMetadata(r))
	}
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if errors.As(err, &fieldErrs) {
			h.renderAccountError(w, r, req, mode, "", fieldErrs.Messages())
			return
		}
		h.renderAccountError(w, r, req, mode, err.Error(), nil)
		return
	}

	h.authMW.SetCookie(w, token, time.Now().Add(h.auth.Sessions().Lifetime()))

	if fresh {
		// The onboarding decision for an account made here: the smallest
		// honest profile, and onboarded. See onboarding.SeedForAgent for why
		// neither the wizard nor Skip would do.
		if _, seedErr := h.onboarding.SeedForAgent(r.Context(), user, req.ClientName); seedErr != nil {
			// Logged, not fatal. The account exists and the session is set;
			// refusing to continue would strand somebody mid-signup over a
			// memory that can be written later.
			h.log.Error("could not seed an account created in consent",
				slog.Any("error", seedErr), slog.String("user_id", user.ID.String()))
		}
		if markErr := h.svc.MarkAccountCreated(r.Context(), req.ID, h.verifier(r)); markErr != nil {
			h.log.Warn("could not record that consent created the account",
				slog.Any("error", markErr))
		}
		req.AccountCreated = true
	}

	// Re-rendered in its signed-in shape, with Approve still to press.
	// Deliberately not auto-submitted: a crafted authorize link plus an
	// auto-submit is a one-click grant, and this is the one place that could
	// happen silently.
	h.renderCard(w, r, req, user, "signup")
}

// decide is Approve or Cancel.
func (h *BrowserHandler) decide(w http.ResponseWriter, r *http.Request) {
	req, ok := h.load(w, r, r.URL.Query().Get("request_id"))
	if !ok {
		return
	}

	user, signedIn := auth.UserFrom(r.Context())
	if !signedIn {
		// Reachable if the session expired while the screen was open.
		h.render(w, r, req, "login")
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if r.PostFormValue("decision") == "deny" {
		redirect, err := h.svc.Deny(r.Context(), req.ID, h.verifier(r))
		if err != nil {
			h.renderExpired(w, r)
			return
		}
		// Counted separately from abandonment: a refusal is a decision, and a
		// screen people read and decline is a different problem from one they
		// close.
		h.funnel.MCPConsentDenied(r.Context(), user.ID, req.ClientName)
		h.clearRequestCookie(w)
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}

	// The read-only toggle may narrow the grant. Approve rejects anything that
	// would widen it, so a posted value cannot buy more than was requested.
	scope := ""
	if r.PostFormValue("read_only") != "" {
		scope = scopeRead()
	}

	redirect, err := h.svc.Approve(r.Context(), req.ID, h.verifier(r), user.ID, scope)
	if err != nil {
		h.renderExpired(w, r)
		return
	}

	h.log.Info("mcp oauth consent approved",
		slog.String("client_id", req.ClientID),
		slog.Bool("account_created", req.AccountCreated))

	// account_created is the verdict row of the scoreboard: whether this
	// feature acquires anybody, or only convenienced people who had already
	// signed up.
	granted := req.Scope
	if scope != "" {
		granted = scope
	}
	h.funnel.MCPConsentApproved(r.Context(), user.ID, req.ClientName, granted, req.AccountCreated)

	h.clearRequestCookie(w)
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// --- rendering -------------------------------------------------------------

func (h *BrowserHandler) render(w http.ResponseWriter, r *http.Request, req Request, mode string) {
	user, _ := auth.UserFrom(r.Context())
	h.renderPage(w, r, req, user, mode)
}

func (h *BrowserHandler) renderPage(w http.ResponseWriter, r *http.Request, req Request, user users.User, mode string) {
	form := h.form(req, user, mode)
	if err := consent.Page(form).Render(r.Context(), w); err != nil {
		h.log.Error("could not render the consent screen", slog.Any("error", err))
	}
}

// renderCard writes the swappable fragment, for an HTMX post.
func (h *BrowserHandler) renderCard(w http.ResponseWriter, r *http.Request, req Request, user users.User, mode string) {
	form := h.form(req, user, mode)
	if err := consent.Card(form).Render(r.Context(), w); err != nil {
		h.log.Error("could not render the consent card", slog.Any("error", err))
	}
}

func (h *BrowserHandler) renderAccountError(
	w http.ResponseWriter,
	r *http.Request,
	req Request,
	mode, message string,
	fields map[string]string,
) {
	user, _ := auth.UserFrom(r.Context())
	form := h.form(req, user, mode)
	form.Account.Error = message
	form.Account.FieldErrors = fields
	form.Account.Email = r.PostFormValue("email")
	form.Account.DisplayName = r.PostFormValue("display_name")

	w.WriteHeader(http.StatusUnprocessableEntity)
	if err := consent.Card(form).Render(r.Context(), w); err != nil {
		h.log.Error("could not render the consent card", slog.Any("error", err))
	}
}

func (h *BrowserHandler) form(req Request, user users.User, mode string) consent.ConsentForm {
	if mode != "login" {
		mode = "signup"
	}

	return consent.ConsentForm{
		RequestID:         req.ID.String(),
		ClientName:        req.ClientName,
		RedirectHost:      req.RedirectHost(),
		IsLoopback:        req.RedirectHost() == "this computer",
		Email:             user.Email,
		ReadOnlyRequested: req.Scope == scopeRead(),
		Account: consent.AccountForm{
			Mode:           mode,
			GoogleEnabled:  h.auth.GoogleEnabled(),
			PasskeyEnabled: h.auth.PasskeyEnabled(),
		},
	}
}

// writeAuthorizeError splits the two kinds of failure.
//
// A fatal one renders a page, because the destination is an unvalidated query
// parameter at that point and redirecting to it is the open-redirect bug this
// endpoint would otherwise be. A redirectable one goes to the callback the
// client actually registered, which is what lets it show a message.
func (h *BrowserHandler) writeAuthorizeError(w http.ResponseWriter, r *http.Request, err error) {
	var fatal FatalAuthorizeError
	if errors.As(err, &fatal) {
		w.WriteHeader(http.StatusBadRequest)
		if renderErr := consent.ErrorPage(fatal.Reason).Render(h.neutralPath(r), w); renderErr != nil {
			h.log.Error("could not render the consent error", slog.Any("error", renderErr))
		}
		return
	}

	var redirectable RedirectableError
	if errors.As(err, &redirectable) {
		http.Redirect(w, r, redirectable.URL(), http.StatusSeeOther)
		return
	}

	h.log.Error("mcp oauth authorize failed", slog.Any("error", err))
	http.Error(w, "server error", http.StatusInternalServerError)
}

func (h *BrowserHandler) renderExpired(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusBadRequest)
	reason := i18n.T(r.Context(), "oauth.error.expired")
	if err := consent.ErrorPage(reason).Render(h.neutralPath(r), w); err != nil {
		h.log.Error("could not render the consent error", slog.Any("error", err))
	}
}

// load resolves a request id and its cookie, rendering the expiry page when
// either is wrong.
//
// A wrong cookie and an unknown id are the same answer, because the cookie is
// a query predicate rather than a value compared afterwards.
func (h *BrowserHandler) load(w http.ResponseWriter, r *http.Request, rawID string) (Request, bool) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		h.renderExpired(w, r)
		return Request{}, false
	}

	req, err := h.svc.LoadRequest(r.Context(), id, h.verifier(r))
	if err != nil {
		h.renderExpired(w, r)
		return Request{}, false
	}
	return req, true
}

// neutralPath strips this request's own URL from the render context.
//
// The error page is rendered for a request whose query string carries an
// unvalidated redirect_uri, and the layout's language switcher posts back to
// whatever middleware.Path reports. That would put an attacker-supplied URI
// into a form action on our own page — escaped, same-origin, and not a
// redirect, but there is no reason to reflect it at all.
func (h *BrowserHandler) neutralPath(r *http.Request) context.Context {
	return middleware.WithPath(r.Context(), "/")
}

func (h *BrowserHandler) verifier(r *http.Request) string {
	cookie, err := r.Cookie(requestCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (h *BrowserHandler) setRequestCookie(w http.ResponseWriter, verifier string) {
	http.SetCookie(w, &http.Cookie{
		Name:     requestCookie,
		Value:    verifier,
		Path:     "/oauth",
		HttpOnly: true,
		Secure:   h.production,

		// Lax, not Strict. The browser arrives here by a top-level redirect
		// from the client, and Strict would withhold the cookie on exactly
		// that navigation.
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(RequestTTL.Seconds()),
	})
}

func (h *BrowserHandler) clearRequestCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     requestCookie,
		Value:    "",
		Path:     "/oauth",
		HttpOnly: true,
		Secure:   h.production,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
