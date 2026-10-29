package push

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is Web Push opt-in for web clients over /api/v1: the same subscribe and
// unsubscribe the cookie-session handler serves, plus the VAPID key the
// browser subscribes with. Mount behind auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/push/config", a.config)
	r.Post("/push/subscriptions", a.subscribe)
	r.Delete("/push/subscriptions", a.unsubscribe)
}

// PushConfig says whether this deployment sends Web Push, with what key, and
// how many of the caller's browsers already listen.
type PushConfig struct {
	Enabled bool `json:"enabled"`
	// PublicKey is the VAPID applicationServerKey, base64url; empty when not enabled.
	PublicKey string `json:"publicKey"`
	Devices   int    `json:"devices"`
}

// apiSubscription is PushSubscription.toJSON(), plus the browser's user agent
// for a client calling through a proxy that does not forward it.
type apiSubscription struct {
	subscriptionJSON
	UserAgent string `json:"userAgent"`
}

func (a *API) config(w http.ResponseWriter, r *http.Request) {
	devices, err := a.svc.Count(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Notifications could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PushConfig{Enabled: a.svc.Enabled(), PublicKey: a.svc.PublicKey(), Devices: devices})
}

func (a *API) subscribe(w http.ResponseWriter, r *http.Request) {
	if !a.svc.Enabled() {
		httpx.Error(w, apperr.ErrNotFound, "Notifications are not available here.")
		return
	}
	var in apiSubscription
	if !readSubscription(w, r, &in) {
		return
	}
	ua := in.UserAgent
	if ua == "" {
		ua = r.UserAgent()
	}
	_, err := a.svc.Subscribe(r.Context(), auth.MustUser(r.Context()).ID, Input{
		Endpoint: in.Endpoint, P256dh: in.Keys.P256dh, Auth: in.Keys.Auth, UserAgent: ua,
	})
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case apperr.Is(err, apperr.ErrValidation):
		httpx.Error(w, err, "That is not a valid push subscription.")
	case apperr.Is(err, apperr.ErrUnavailable):
		httpx.Error(w, apperr.ErrNotFound, "Notifications are not available here.")
	default:
		httpx.Error(w, err, "Something went wrong.")
	}
}

func (a *API) unsubscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if !readSubscription(w, r, &in) {
		return
	}
	if in.Endpoint == "" {
		httpx.Error(w, apperr.ErrValidation, "An endpoint is required.")
		return
	}
	if err := a.svc.Unsubscribe(r.Context(), auth.MustUser(r.Context()).ID, in.Endpoint); err != nil {
		httpx.Error(w, err, "Something went wrong.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readSubscription decodes a small JSON body, answering the request itself
// when it is not one. Unknown fields are allowed for the reason the cookie
// handler gives: the browser's shape, not ours.
func readSubscription(w http.ResponseWriter, r *http.Request, dst any) bool {
	err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: maxRequestBytes, AllowUnknownFields: true})
	switch {
	case err == nil:
		return true
	case errors.Is(err, httpx.ErrTooLarge):
		httpx.WriteJSON(w, http.StatusRequestEntityTooLarge, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: "That request is too large."}})
	default:
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: "The request body must be a JSON subscription."}})
	}
	return false
}
