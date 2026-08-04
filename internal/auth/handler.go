package auth

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	authpages "github.com/NorthAIProject/north-client/web/auth"
)

// Handler serves the signup, login, and logout routes.
//
// It parses, delegates, and renders. Every rule about what a valid password or
// a valid profile is lives in the service layer, so the same rules apply when
// Telegram or MCP creates a session later.
type Handler struct {
	svc  *Service
	mw   *Middleware
	home string
}

// NewHandler builds the auth handler. home is where a successful sign-in lands.
func NewHandler(svc *Service, mw *Middleware, home string) *Handler {
	return &Handler{svc: svc, mw: mw, home: home}
}

// Routes mounts the auth endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/signup", h.showSignup)
	r.Post("/signup", h.submitSignup)
	r.Get("/login", h.showLogin)
	r.Post("/login", h.submitLogin)
	r.Get("/forgot-password", h.showForgotPassword)
	r.Post("/forgot-password", h.submitForgotPassword)
	r.Get("/reset-password", h.showResetPassword)
	r.Post("/reset-password", h.submitResetPassword)
	r.Post("/logout", h.logout)

	r.Get("/auth/google", h.googleStart)
	r.Get("/auth/google/callback", h.googleCallback)

	r.Post("/auth/passkey/register/begin", h.passkeyRegisterBegin)
	r.Post("/auth/passkey/register/finish", h.passkeyRegisterFinish)
	r.Post("/auth/passkey/login/begin", h.passkeyLoginBegin)
	r.Post("/auth/passkey/login/finish", h.passkeyLoginFinish)
}

func (h *Handler) formOpts() authpages.AuthOptions {
	return authpages.AuthOptions{
		GoogleEnabled:  h.svc.GoogleEnabled(),
		PasskeyEnabled: h.svc.PasskeyEnabled(),
	}
}

func (h *Handler) showSignup(w http.ResponseWriter, r *http.Request) {
	// Already signed in: there is nothing useful on a signup form.
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, h.home, http.StatusSeeOther)
		return
	}
	render(w, r, http.StatusOK, authpages.SignupPage(authpages.SignupForm{Options: h.formOpts()}))
}

func (h *Handler) submitSignup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, r, http.StatusBadRequest, authpages.SignupPage(authpages.SignupForm{
			Error:   "That form could not be read. Please try again.",
			Options: h.formOpts(),
		}))
		return
	}

	form := authpages.SignupForm{
		Email:       r.PostFormValue("email"),
		DisplayName: r.PostFormValue("display_name"),
		Timezone:    r.PostFormValue("timezone"),
		Options:     h.formOpts(),
	}

	user, token, err := h.svc.Signup(r.Context(), SignupInput{
		Email:                r.PostFormValue("email"),
		DisplayName:          r.PostFormValue("display_name"),
		Password:             r.PostFormValue("password"),
		PasswordConfirmation: r.PostFormValue("password_confirmation"),
		Timezone:             r.PostFormValue("timezone"),
	}, RequestMetadata(r))
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			render(w, r, http.StatusUnprocessableEntity, authpages.SignupPage(form))
			return
		}

		middleware.FromContext(r.Context()).Error("signup failed", slog.Any("error", err))
		form.Error = "Something went wrong creating your account. Please try again."
		render(w, r, http.StatusInternalServerError, authpages.SignupPage(form))
		return
	}

	h.mw.SetCookie(w, token, tokenExpiry(h.svc))
	middleware.FromContext(r.Context()).Info("account created", slog.String("user_id", user.ID.String()))

	http.Redirect(w, r, h.home, http.StatusSeeOther)
}

func (h *Handler) showLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, h.home, http.StatusSeeOther)
		return
	}

	form := authpages.LoginForm{Options: h.formOpts()}
	if next := r.URL.Query().Get("next"); SafeRedirect(next) {
		form.Next = next
	}
	render(w, r, http.StatusOK, authpages.LoginPage(form))
}

func (h *Handler) submitLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, r, http.StatusBadRequest, authpages.LoginPage(authpages.LoginForm{
			Error:   "That form could not be read. Please try again.",
			Options: h.formOpts(),
		}))
		return
	}

	form := authpages.LoginForm{Email: r.PostFormValue("email"), Options: h.formOpts()}
	if next := r.PostFormValue("next"); SafeRedirect(next) {
		form.Next = next
	}

	user, token, err := h.svc.Login(r.Context(), LoginInput{
		Email:    r.PostFormValue("email"),
		Password: r.PostFormValue("password"),
	}, RequestMetadata(r))
	if err != nil {
		if apperr.Is(err, ErrInvalidCredentials) {
			// 401 rather than 422: the credentials were readable and wrong.
			form.Error = "Email or password is incorrect."
			render(w, r, http.StatusUnauthorized, authpages.LoginPage(form))
			return
		}

		middleware.FromContext(r.Context()).Error("login failed", slog.Any("error", err))
		form.Error = "Something went wrong signing you in. Please try again."
		render(w, r, http.StatusInternalServerError, authpages.LoginPage(form))
		return
	}

	h.mw.SetCookie(w, token, tokenExpiry(h.svc))
	middleware.FromContext(r.Context()).Info("signed in", slog.String("user_id", user.ID.String()))

	destination := h.home
	if form.Next != "" {
		destination = form.Next
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		if err := h.svc.Logout(r.Context(), cookie.Value); err != nil {
			// The cookie is cleared regardless: the user asked to sign out, and
			// leaving them signed in because of a database hiccup is worse than
			// an orphaned row that expires on its own.
			middleware.FromContext(r.Context()).Error("revoke session failed", slog.Any("error", err))
		}
	}

	h.mw.ClearCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Google OAuth
// ---------------------------------------------------------------------------

func (h *Handler) googleStart(w http.ResponseWriter, r *http.Request) {
	if !h.svc.GoogleEnabled() {
		http.NotFound(w, r)
		return
	}
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, h.home, http.StatusSeeOther)
		return
	}

	state, err := newOpaqueToken(googleStateBytes)
	if err != nil {
		middleware.FromContext(r.Context()).Error("google oauth state", slog.Any("error", err))
		http.Error(w, "Could not start Google sign-in.", http.StatusInternalServerError)
		return
	}

	next := ""
	if n := r.URL.Query().Get("next"); SafeRedirect(n) {
		next = n
	}

	// State is random; next is stashed alongside it in the cookie value so the
	// callback cannot be tricked into an open redirect with a forged state alone.
	cookieVal := state
	if next != "" {
		cookieVal = state + "|" + next
	}
	http.SetCookie(w, &http.Cookie{
		Name:     googleStateCookie,
		Value:    cookieVal,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.mw.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(googleStateTTL / time.Second),
	})

	url, err := h.svc.GoogleAuthCodeURL(state)
	if err != nil {
		http.Error(w, "Google sign-in is unavailable.", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *Handler) googleCallback(w http.ResponseWriter, r *http.Request) {
	if !h.svc.GoogleEnabled() {
		http.NotFound(w, r)
		return
	}

	clearGoogleStateCookie(w, h.mw.secure)

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		render(w, r, http.StatusBadRequest, authpages.LoginPage(authpages.LoginForm{
			Error:   "Google sign-in was cancelled or denied.",
			Options: h.formOpts(),
		}))
		return
	}

	cookie, err := r.Cookie(googleStateCookie)
	if err != nil || cookie.Value == "" {
		render(w, r, http.StatusBadRequest, authpages.LoginPage(authpages.LoginForm{
			Error:   "Google sign-in expired. Please try again.",
			Options: h.formOpts(),
		}))
		return
	}

	storedState, next := splitStateCookie(cookie.Value)
	if subtle.ConstantTimeCompare([]byte(storedState), []byte(r.URL.Query().Get("state"))) != 1 {
		render(w, r, http.StatusBadRequest, authpages.LoginPage(authpages.LoginForm{
			Error:   "Google sign-in could not be verified. Please try again.",
			Options: h.formOpts(),
		}))
		return
	}

	user, token, err := h.svc.CompleteGoogleOAuth(r.Context(), r.URL.Query().Get("code"), RequestMetadata(r))
	if err != nil {
		middleware.FromContext(r.Context()).Error("google oauth failed", slog.Any("error", err))
		render(w, r, http.StatusBadGateway, authpages.LoginPage(authpages.LoginForm{
			Error:   "Google sign-in failed. Please try again.",
			Options: h.formOpts(),
		}))
		return
	}

	h.mw.SetCookie(w, token, tokenExpiry(h.svc))
	middleware.FromContext(r.Context()).Info("signed in with google", slog.String("user_id", user.ID.String()))

	destination := h.home
	if next != "" && SafeRedirect(next) {
		destination = next
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func clearGoogleStateCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     googleStateCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func splitStateCookie(v string) (state, next string) {
	state, next, _ = strings.Cut(v, "|")
	return state, next
}

// ---------------------------------------------------------------------------
// Passkeys (WebAuthn JSON API)
// ---------------------------------------------------------------------------

func (h *Handler) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !h.svc.PasskeyEnabled() {
		http.NotFound(w, r)
		return
	}
	var in PasskeyRegisterBeginInput
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body."})
		return
	}
	ceremony, err := h.svc.PasskeyRegisterBegin(r.Context(), in)
	if err != nil {
		writePasskeyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ceremony)
}

func (h *Handler) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !h.svc.PasskeyEnabled() {
		http.NotFound(w, r)
		return
	}
	var in PasskeyRegisterFinishInput
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body."})
		return
	}
	user, token, err := h.svc.PasskeyRegisterFinish(r.Context(), in, RequestMetadata(r))
	if err != nil {
		writePasskeyError(w, r, err)
		return
	}
	h.mw.SetCookie(w, token, tokenExpiry(h.svc))
	middleware.FromContext(r.Context()).Info("account created with passkey", slog.String("user_id", user.ID.String()))
	writeJSON(w, http.StatusOK, map[string]string{"redirect": h.home})
}

func (h *Handler) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !h.svc.PasskeyEnabled() {
		http.NotFound(w, r)
		return
	}
	var in PasskeyLoginBeginInput
	// Empty body is fine (discoverable login).
	_ = decodeJSON(r, &in)
	ceremony, err := h.svc.PasskeyLoginBegin(r.Context(), in)
	if err != nil {
		writePasskeyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ceremony)
}

func (h *Handler) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !h.svc.PasskeyEnabled() {
		http.NotFound(w, r)
		return
	}
	var in PasskeyLoginFinishInput
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body."})
		return
	}
	user, token, err := h.svc.PasskeyLoginFinish(r.Context(), in, RequestMetadata(r))
	if err != nil {
		writePasskeyError(w, r, err)
		return
	}
	h.mw.SetCookie(w, token, tokenExpiry(h.svc))
	middleware.FromContext(r.Context()).Info("signed in with passkey", slog.String("user_id", user.ID.String()))

	redirect := h.home
	if next := r.URL.Query().Get("next"); SafeRedirect(next) {
		redirect = next
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect": redirect})
}

func writePasskeyError(w http.ResponseWriter, r *http.Request, err error) {
	var fieldErrs apperr.FieldErrors
	if apperr.As(err, &fieldErrs) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  fieldErrs.Error(),
			"fields": fieldErrs.Messages(),
		})
		return
	}
	if apperr.Is(err, ErrInvalidCredentials) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Passkey sign-in failed."})
		return
	}
	msg := err.Error()
	// Client-facing ceremony errors (expired challenge, parse failures).
	if msg == "invalid or expired passkey challenge" || msg == "invalid passkey ceremony" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "That passkey step expired. Please try again."})
		return
	}
	middleware.FromContext(r.Context()).Error("passkey failed", slog.Any("error", err))
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Something went wrong with the passkey. Please try again."})
}

func decodeJSON(r *http.Request, dst any) error {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) showForgotPassword(w http.ResponseWriter, r *http.Request) {
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, h.home, http.StatusSeeOther)
		return
	}
	render(w, r, http.StatusOK, authpages.ForgotPasswordPage(authpages.ForgotPasswordForm{}))
}

func (h *Handler) submitForgotPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, r, http.StatusBadRequest, authpages.ForgotPasswordPage(authpages.ForgotPasswordForm{
			Error: "That form could not be read. Please try again.",
		}))
		return
	}

	form := authpages.ForgotPasswordForm{Email: r.PostFormValue("email")}
	if err := h.svc.RequestPasswordReset(r.Context(), form.Email); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			render(w, r, http.StatusUnprocessableEntity, authpages.ForgotPasswordPage(form))
			return
		}

		middleware.FromContext(r.Context()).Error("password reset request failed", slog.Any("error", err))
		form.Error = "Something went wrong. Please try again."
		render(w, r, http.StatusInternalServerError, authpages.ForgotPasswordPage(form))
		return
	}

	// Same success copy whether or not the address is registered.
	form.Submitted = true
	render(w, r, http.StatusOK, authpages.ForgotPasswordPage(form))
}

func (h *Handler) showResetPassword(w http.ResponseWriter, r *http.Request) {
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, h.home, http.StatusSeeOther)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		render(w, r, http.StatusBadRequest, authpages.ResetPasswordPage(authpages.ResetPasswordForm{
			Error: "This reset link is missing or incomplete. Request a new one from the sign-in page.",
		}))
		return
	}

	render(w, r, http.StatusOK, authpages.ResetPasswordPage(authpages.ResetPasswordForm{Token: token}))
}

func (h *Handler) submitResetPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, r, http.StatusBadRequest, authpages.ResetPasswordPage(authpages.ResetPasswordForm{
			Error: "That form could not be read. Please try again.",
		}))
		return
	}

	form := authpages.ResetPasswordForm{Token: r.PostFormValue("token")}
	user, token, err := h.svc.ResetPassword(r.Context(), ResetPasswordInput{
		Token:                form.Token,
		Password:             r.PostFormValue("password"),
		PasswordConfirmation: r.PostFormValue("password_confirmation"),
	}, RequestMetadata(r))
	if err != nil {
		if apperr.Is(err, ErrInvalidResetToken) {
			form.Error = "This reset link is invalid or has expired. Request a new one."
			render(w, r, http.StatusUnauthorized, authpages.ResetPasswordPage(form))
			return
		}

		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			render(w, r, http.StatusUnprocessableEntity, authpages.ResetPasswordPage(form))
			return
		}

		middleware.FromContext(r.Context()).Error("password reset failed", slog.Any("error", err))
		form.Error = "Something went wrong resetting your password. Please try again."
		render(w, r, http.StatusInternalServerError, authpages.ResetPasswordPage(form))
		return
	}

	h.mw.SetCookie(w, token, tokenExpiry(h.svc))
	middleware.FromContext(r.Context()).Info("password reset", slog.String("user_id", user.ID.String()))
	http.Redirect(w, r, h.home, http.StatusSeeOther)
}
