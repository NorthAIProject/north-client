package fitness

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	fitnesspages "github.com/NorthAIProject/north-client/web/fitness"
)

const (
	stravaStateCookie = "north_strava_oauth"
	stravaStateTTL    = 10 * time.Minute
)

type Handler struct {
	svc    *Service
	strava *strava.Service

	// secure marks the state cookie Secure outside development, matching how
	// the auth middleware decides the same thing for the session cookie.
	secure bool
}

func NewHandler(opts Options, secure bool) *Handler {
	return &Handler{
		svc:    NewService(opts),
		strava: opts.Strava,
		secure: secure,
	}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/fitness", h.hub)

	r.Get("/fitness/activities", h.activities)
	r.Get("/fitness/activities/sessions", h.activitySessions)

	r.Get("/fitness/strava/connect", h.stravaConnect)
	r.Get("/fitness/strava/callback", h.stravaCallback)
	r.Post("/fitness/strava/sync", h.stravaSync)
	r.Post("/fitness/strava/disconnect", h.stravaDisconnect)
}

// activities renders the training chart and the session list.
func (h *Handler) activities(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	status, err := h.strava.Status(ctx, user.ID)
	if err != nil {
		middleware.FromContext(ctx).Error("read strava status", slog.Any("error", err))
		status = strava.Status{Configured: h.strava.Configured(), Unavailable: true}
	}

	var (
		trend    strava.Trend
		sessions strava.SessionPage
	)
	if status.Connected {
		trend, err = h.strava.Trend(ctx, user.ID, user.Location(), 0)
		if err != nil {
			middleware.FromContext(ctx).Error("build training trend", slog.Any("error", err))
			// Not worth failing the page, but an empty chart must not claim
			// nothing was ever imported. Unavailable is what makes the template
			// say the activities could not be read — which it now actually does.
			trend = strava.Trend{}
			status.Unavailable = true
		}

		sessions, err = h.strava.Sessions(ctx, user.ID, user.Location(), sessionPageParam(r), 0)
		if err != nil {
			middleware.FromContext(ctx).Error("read activity sessions", slog.Any("error", err))
			sessions = strava.SessionPage{}
			status.Unavailable = true
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := fitnesspages.ActivitiesPage(user, status, trend, sessions).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render activities", slog.Any("error", err))
	}
}

// activitySessions serves one page of the session list, as the markup the
// pager swaps in.
//
// A fragment rather than a whole page because turning a page must not rebuild
// the terrain: the scene is a WebGL context with weeks resident on the GPU,
// and throwing it away to read the next ten rows would be both slow and
// jarring. The pager's links carry real hrefs, so this endpoint is also what a
// reader without JavaScript lands on.
func (h *Handler) activitySessions(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	sessions, err := h.strava.Sessions(ctx, user.ID, user.Location(), sessionPageParam(r), 0)
	if err != nil {
		middleware.FromContext(ctx).Error("read activity sessions", slog.Any("error", err))
		http.Error(w, "Your activities could not be read just now.", httpx.Status(err))
		return
	}

	// Somebody's training history. Never a shared cache, never a disk copy.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := fitnesspages.ActivitySessions(sessions, user.Location()).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render activity sessions", slog.Any("error", err))
	}
}

// sessionPageParam reads ?page=. Anything that is not a positive number is
// page one: a pager is navigation, and navigation that answers a fat-fingered
// URL with a 422 is worse than one that answers it with the first page. The
// service clamps the upper end against how many pages there actually are.
func sessionPageParam(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

func (h *Handler) hub(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "")
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, notice string) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	snap, err := h.svc.Load(ctx, user)
	if err != nil {
		middleware.FromContext(ctx).Error("load fitness hub", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}

	data := fitnesspages.HubData{
		StravaStatus:      snap.StravaStatus,
		Instruments:       buildView(snap),
		Notice:            notice,
		DeviceReadings:    snap.DeviceReadings,
		HasDeviceReadings: snap.HasDeviceReadings,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := fitnesspages.Hub(user, data).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render fitness hub", slog.Any("error", err))
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
