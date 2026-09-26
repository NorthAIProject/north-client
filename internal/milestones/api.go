package milestones

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is "months since" trackers for native clients, at /trackers: goals
// already have milestones, and the two must not be confused on the wire. Every change answers
// with the whole list.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/trackers", a.list)
	r.Post("/trackers", a.create)
	r.Post("/trackers/{id}/done", a.done)
	r.Delete("/trackers/{id}", a.remove)
}

type MilestoneRequest struct {
	Name string `json:"name"`
	// LastDoneOn is YYYY-MM-DD; today when absent.
	LastDoneOn     string `json:"lastDoneOn,omitempty"`
	IntervalMonths *int   `json:"intervalMonths,omitempty"`
}

type MilestoneView struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	LastDoneOn     string    `json:"lastDoneOn"`
	IntervalMonths *int      `json:"intervalMonths,omitempty"`
	MonthsSince    int       `json:"monthsSince"`
	Due            bool      `json:"due"`
}

type MilestonesView struct {
	Trackers []MilestoneView `json:"trackers"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) { a.respond(w, r, http.StatusOK) }

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var req MilestoneRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	user := auth.MustUser(r.Context())
	var last *time.Time
	if req.LastDoneOn != "" {
		d, err := time.ParseInLocation("2006-01-02", req.LastDoneOn, user.Location())
		if err != nil {
			httpx.Error(w, apperr.FieldErrors{}.Add("lastDoneOn", "Use YYYY-MM-DD."), "Use YYYY-MM-DD.")
			return
		}
		last = &d
	}
	if _, err := a.svc.Create(r.Context(), user, req.Name, last, req.IntervalMonths); err != nil {
		httpx.Error(w, err, "The tracker could not be created.")
		return
	}
	a.respond(w, r, http.StatusCreated)
}

func (a *API) done(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "No such tracker.")
		return
	}
	if _, err := a.svc.Done(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		httpx.Error(w, err, "The tracker could not be updated.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) remove(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "No such tracker.")
		return
	}
	if err := a.svc.Delete(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		httpx.Error(w, err, "The tracker could not be removed.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) respond(w http.ResponseWriter, r *http.Request, status int) {
	user := auth.MustUser(r.Context())
	list, err := a.svc.List(r.Context(), user)
	if err != nil {
		httpx.Error(w, err, "The trackers could not be loaded.")
		return
	}
	httpx.WriteJSON(w, status, ProjectList(list, time.Now().In(user.Location())))
}

// ProjectList is the list payload.
func ProjectList(list []Tracker, now time.Time) MilestonesView {
	out := MilestonesView{Trackers: make([]MilestoneView, len(list))}
	for i, t := range list {
		out.Trackers[i] = MilestoneView{
			ID: t.ID, Name: t.Name, LastDoneOn: t.LastDoneOn.Format("2006-01-02"),
			IntervalMonths: t.IntervalMonths, MonthsSince: t.MonthsSince(now), Due: t.Due(now),
		}
	}
	return out
}
