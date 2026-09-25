package decisions

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the decision log for native clients: what was decided, between
// what, why, and how it turned out, as the web /decisions page keeps it.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/decisions", a.list)
	r.Post("/decisions", a.create)
	r.Get("/decisions/{decisionID}", a.show)
	r.Put("/decisions/{decisionID}", a.update)
	r.Delete("/decisions/{decisionID}", a.destroy)
}

type DecisionView struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Options   string    `json:"options"`
	Rationale string    `json:"rationale"`
	// Outcome is filled in later, once it is known how it went.
	Outcome   string    `json:"outcome"`
	DecidedAt time.Time `json:"decidedAt"`
}

type DecisionList struct {
	Decisions []DecisionView `json:"decisions"`
}

type DecisionRequest struct {
	Title     string `json:"title"`
	Options   string `json:"options"`
	Rationale string `json:"rationale"`
	Outcome   string `json:"outcome"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.svc.List(r.Context(), auth.MustUser(r.Context()).ID, 100)
	if err != nil {
		httpx.Error(w, err, "Decisions could not be loaded.")
		return
	}
	out := DecisionList{Decisions: make([]DecisionView, 0, len(list))}
	for _, d := range list {
		out.Decisions = append(out.Decisions, project(d))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var req DecisionRequest
	if !readJSON(w, r, &req) {
		return
	}
	d, err := a.svc.Create(r.Context(), auth.MustUser(r.Context()).ID, Input(req))
	if err != nil {
		httpx.Error(w, err, "The decision could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, project(d))
}

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	id, ok := decisionID(w, r)
	if !ok {
		return
	}
	d, err := a.svc.Get(r.Context(), id, auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The decision could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project(d))
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, ok := decisionID(w, r)
	if !ok {
		return
	}
	var req DecisionRequest
	if !readJSON(w, r, &req) {
		return
	}
	d, err := a.svc.Update(r.Context(), id, auth.MustUser(r.Context()).ID, Input(req))
	if err != nil {
		httpx.Error(w, err, "The decision could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project(d))
}

func (a *API) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := decisionID(w, r)
	if !ok {
		return
	}
	if err := a.svc.Delete(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The decision could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func project(d Decision) DecisionView {
	return DecisionView{ID: d.ID, Title: d.Title, Options: d.Options, Rationale: d.Rationale, Outcome: d.Outcome, DecidedAt: d.DecidedAt}
}

func decisionID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "decisionID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 64 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
