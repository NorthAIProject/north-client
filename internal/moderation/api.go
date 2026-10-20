package moderation

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the app's side of the terms: whether the current version is
// accepted, and accepting it.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/moderation/terms", a.terms)
	r.Post("/moderation/terms", a.accept)
}

// TermsView is the terms state for the person asking.
type TermsView struct {
	Version  int  `json:"version"`
	Accepted bool `json:"accepted"`
}

func (a *API) terms(w http.ResponseWriter, r *http.Request) {
	ok, err := a.svc.TermsAccepted(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The terms could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, TermsView{Version: TermsVersion, Accepted: ok})
}

func (a *API) accept(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.AcceptTerms(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The terms could not be saved.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
