package fitness

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	fitnesspages "github.com/NorthAIProject/north-client/web/fitness"
)

const (
	stravaStateCookie = "north_strava_oauth"
	stravaStateTTL    = 10 * time.Minute
)

type Handler struct {
	strava *strava.Service

	// secure marks the state cookie Secure outside development, matching how
	// the auth middleware decides the same thing for the session cookie.
	secure bool
}

func NewHandler(stravaSvc *strava.Service, secure bool) *Handler {
	return &Handler{strava: stravaSvc, secure: secure}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/fitness", h.hub)

	r.Get("/fitness/strava/connect", h.stravaConnect)
	r.Get("/fitness/strava/callback", h.stravaCallback)
	r.Post("/fitness/strava/sync", h.stravaSync)
	r.Post("/fitness/strava/disconnect", h.stravaDisconnect)
}

func (h *Handler) hub(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "")
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, notice string) {
	user := auth.MustUser(r.Context())

	status, err := h.strava.Status(r.Context(), user.ID)
	if err != nil {
		middleware.FromContext(r.Context()).Error("read strava status", slog.Any("error", err))
		// A hub page that cannot read the integration's status is still a
		// useful hub page; show it as disconnected rather than 500.
		status = strava.Status{Configured: h.strava.Configured()}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := fitnesspages.Hub(user, status, notice).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render fitness hub", slog.Any("error", err))
	}
}

// stravaConnect starts the OAuth flow. The state is random and stored in an
// HttpOnly cookie, so the callback can prove the response belongs to a flow
// this browser actually started — same arrangement as Google sign-in.
func (h *Handler) stravaConnect(w http.ResponseWriter, r *http.Request) {
	if !h.strava.Configured() {
		http.NotFound(w, r)
		return
	}

	state, err := strava.NewState()
	if err != nil {
		http.Error(w, "Could not start the Strava connection.", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     stravaStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(stravaStateTTL / time.Second),
	})

	url, err := h.strava.AuthCodeURL(state)
	if err != nil {
		http.Error(w, "Strava is unavailable.", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *Handler) stravaCallback(w http.ResponseWriter, r *http.Request) {
	if !h.strava.Configured() {
		http.NotFound(w, r)
		return
	}

	h.clearStateCookie(w)

	if r.URL.Query().Get("error") != "" {
		h.render(w, r, "Strava connection was cancelled.")
		return
	}

	cookie, err := r.Cookie(stravaStateCookie)
	if err != nil || cookie.Value == "" {
		h.render(w, r, "That Strava connection expired. Please try again.")
		return
	}
	if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.URL.Query().Get("state"))) != 1 {
		h.render(w, r, "That Strava connection could not be verified. Please try again.")
		return
	}

	if err := h.strava.Connect(r.Context(), auth.MustUser(r.Context()).ID, r.URL.Query().Get("code")); err != nil {
		middleware.FromContext(r.Context()).Error("strava connect failed", slog.Any("error", err))
		h.render(w, r, "Connecting to Strava failed. Please try again.")
		return
	}

	h.render(w, r, "Strava connected. Your recent activities are importing now.")
}

func (h *Handler) stravaSync(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := h.strava.RequestSync(r.Context(), user.ID); err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			h.render(w, r, "Connect Strava first.")
			return
		}
		middleware.FromContext(r.Context()).Error("strava sync request failed", slog.Any("error", err))
		h.render(w, r, "Could not start the sync. Please try again.")
		return
	}

	h.render(w, r, "Syncing. New activities will appear shortly.")
}

func (h *Handler) stravaDisconnect(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := h.strava.Disconnect(r.Context(), user.ID); err != nil {
		middleware.FromContext(r.Context()).Error("strava disconnect failed", slog.Any("error", err))
		h.render(w, r, "Could not disconnect. Please try again.")
		return
	}

	// Already-imported sessions stay: they are a record of training that
	// happened, and deleting someone's history because they unlinked an
	// account would be a surprise.
	h.render(w, r, "Strava disconnected. Activities already imported have been kept.")
}

func (h *Handler) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     stravaStateCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
}
