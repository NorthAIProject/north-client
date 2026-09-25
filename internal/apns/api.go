package apns

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API lets the app register the device token iOS handed it, and forget it
// on sign-out. Mount behind auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Put("/devices/apns", a.register)
	r.Delete("/devices/apns/{token}", a.unregister)
}

type RegisterRequest struct {
	Token       string `json:"token"`
	Topic       string `json:"topic"`
	Environment string `json:"environment"`
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	in := Input(req)
	if _, err := a.svc.Register(r.Context(), auth.MustUser(r.Context()).ID, in); err != nil {
		httpx.Error(w, err, "This device could not be registered for notifications.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) unregister(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Unregister(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "token")); err != nil {
		httpx.Error(w, err, "This device could not be removed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
