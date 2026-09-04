package push

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// A subscription serialises to a few hundred bytes. Anything near this is not
// one.
const maxRequestBytes = 8 << 10

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the subscribe and unsubscribe endpoints. Must be behind
// RequireAuth: a subscription belongs to the signed-in account.
//
// JSON rather than a form because the browser already holds the subscription
// as JSON (PushSubscription.toJSON) and the call is made from script after a
// permission prompt, not from a submit button. The CSRF token travels in the
// X-CSRF-Token header, which the CSRF middleware checks before the body.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/push/subscriptions", h.subscribe)
	r.Delete("/push/subscriptions", h.unsubscribe)
}

// subscriptionJSON is PushSubscription.toJSON() as the browser emits it.
type subscriptionJSON struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	if !h.svc.Enabled() {
		http.NotFound(w, r)
		return
	}
	user := auth.MustUser(r.Context())

	var in subscriptionJSON
	if !h.decode(w, r, &in) {
		return
	}

	_, err := h.svc.Subscribe(r.Context(), user.ID, Input{
		Endpoint:  in.Endpoint,
		P256dh:    in.Keys.P256dh,
		Auth:      in.Keys.Auth,
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	if in.Endpoint == "" {
		http.Error(w, "An endpoint is required.", http.StatusUnprocessableEntity)
		return
	}

	if err := h.svc.Unsubscribe(r.Context(), user.ID, in.Endpoint); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decode reads a small JSON body into dst, answering the request itself when
// the body is not one.
//
// Unknown fields are allowed on purpose: PushSubscription.toJSON() carries
// expirationTime today and whatever the spec adds tomorrow, and a decoder
// that refused them would refuse every real browser.
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{
		MaxBytes: maxRequestBytes,

		// See the note above: the browser's own shape, not ours.
		AllowUnknownFields: true,
	})
	switch {
	case err == nil:
		return true
	case errors.Is(err, httpx.ErrTooLarge):
		http.Error(w, "That request is too large.", http.StatusRequestEntityTooLarge)
	default:
		http.Error(w, "The request body must be a JSON subscription.", http.StatusBadRequest)
	}
	return false
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That is not a valid push subscription.", http.StatusUnprocessableEntity)
	case apperr.Is(err, apperr.ErrUnavailable):
		http.NotFound(w, r)
	default:
		middleware.FromContext(r.Context()).Error("push request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
