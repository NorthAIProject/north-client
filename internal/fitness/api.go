package fitness

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// NativeReturnURL is where the Strava callback sends the app's sign-in sheet
// when it is done. The sheet closes on the khepri:// scheme and the app reads
// the result from the query.
const NativeReturnURL = "khepri://fitness/strava"

// API is Strava for native clients: status, connect, sync and disconnect, the
// same operations as the web fitness hub's Strava card.
type API struct {
	strava  *strava.Service
	baseURL string
}

// NewAPI builds the routes. Routes go behind auth.RequireBearer; PublicRoutes
// is the OAuth callback, which Strava calls with no session at all.
func NewAPI(svc *strava.Service, baseURL string) *API {
	return &API{strava: svc, baseURL: baseURL}
}

func (a *API) Routes(r chi.Router) {
	r.Get("/fitness/strava", a.status)
	r.Post("/fitness/strava/connect", a.connect)
	r.Post("/fitness/strava/sync", a.sync)
	r.Delete("/fitness/strava", a.disconnect)
}

func (a *API) PublicRoutes(r chi.Router) {
	r.Get("/fitness/strava/callback", a.callback)
}

type StravaStatus struct {
	// Configured is false when this deployment has no Strava credentials; the
	// app then hides the connection instead of offering one that cannot work.
	Configured          bool       `json:"configured"`
	Connected           bool       `json:"connected"`
	LastSyncedAt        *time.Time `json:"lastSyncedAt,omitempty"`
	LastSyncAttemptedAt *time.Time `json:"lastSyncAttemptedAt,omitempty"`
	// LastSyncError is empty after a successful sync.
	LastSyncError string `json:"lastSyncError,omitempty"`
	// SyncPending means a sync is queued or running.
	SyncPending bool `json:"syncPending"`
}

type StravaConnect struct {
	// AuthorizeURL opens in the app's sign-in sheet; Strava returns to
	// NativeReturnURL with result=connected, cancelled, expired or failed.
	AuthorizeURL string `json:"authorizeUrl"`
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	s, err := a.strava.Status(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Strava's status could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StravaStatus{
		Configured: s.Configured, Connected: s.Connected,
		LastSyncedAt: s.LastSyncedAt, LastSyncAttemptedAt: s.LastSyncAttemptedAt,
		LastSyncError: s.LastSyncError, SyncPending: s.SyncPending,
	})
}

func (a *API) connect(w http.ResponseWriter, r *http.Request) {
	consent, err := a.strava.BeginNativeConnect(r.Context(), auth.MustUser(r.Context()).ID, a.baseURL)
	if err != nil {
		httpx.Error(w, err, "Strava is not available right now.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StravaConnect{AuthorizeURL: consent})
}

func (a *API) sync(w http.ResponseWriter, r *http.Request) {
	if err := a.strava.RequestSync(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "Connect Strava first.")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) disconnect(w http.ResponseWriter, r *http.Request) {
	if err := a.strava.Disconnect(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "Strava could not be disconnected.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// callback finishes a connection begun by connect and hands the result back to
// the app. It always redirects to the app, never renders: the sign-in sheet
// shows nothing of its own, so the app is the only place to say what happened.
func (a *API) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result := "connected"
	switch {
	case q.Get("error") != "":
		result = "cancelled"
	default:
		err := a.strava.FinishNativeConnect(r.Context(), q.Get("state"), q.Get("code"))
		switch {
		case apperr.Is(err, apperr.ErrNotFound):
			result = "expired"
		case err != nil:
			middleware.FromContext(r.Context()).Error("native strava connect failed", slog.Any("error", err))
			result = "failed"
		}
	}
	http.Redirect(w, r, NativeReturnURL+"?"+url.Values{"result": {result}}.Encode(), http.StatusFound)
}
