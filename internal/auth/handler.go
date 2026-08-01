package auth

import (
	"log/slog"
	"net/http"

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
	r.Post("/logout", h.logout)
}

func (h *Handler) showSignup(w http.ResponseWriter, r *http.Request) {
	// Already signed in: there is nothing useful on a signup form.
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, h.home, http.StatusSeeOther)
		return
	}
	render(w, r, http.StatusOK, authpages.SignupPage(authpages.SignupForm{}))
}

func (h *Handler) submitSignup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, r, http.StatusBadRequest, authpages.SignupPage(authpages.SignupForm{
			Error: "That form could not be read. Please try again.",
		}))
		return
	}

	form := authpages.SignupForm{
		Email:       r.PostFormValue("email"),
		DisplayName: r.PostFormValue("display_name"),
		Timezone:    r.PostFormValue("timezone"),
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

	form := authpages.LoginForm{}
	if next := r.URL.Query().Get("next"); SafeRedirect(next) {
		form.Next = next
	}
	render(w, r, http.StatusOK, authpages.LoginPage(form))
}

func (h *Handler) submitLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, r, http.StatusBadRequest, authpages.LoginPage(authpages.LoginForm{
			Error: "That form could not be read. Please try again.",
		}))
		return
	}

	form := authpages.LoginForm{Email: r.PostFormValue("email")}
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
