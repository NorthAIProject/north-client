package checkins

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is check-ins for native clients: today's, the recent ones, the streak,
// and editing, over the same service the web /check-ins page uses.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/check-ins", a.list)
	r.Put("/check-ins/today", a.upsertToday)
	r.Put("/check-ins/{checkInID}", a.update)
	r.Delete("/check-ins/{checkInID}", a.destroy)
}

type CheckInView struct {
	ID uuid.UUID `json:"id"`
	// LocalDate is the day in the person's own time zone it belongs to.
	LocalDate  string `json:"localDate"`
	Mood       int    `json:"mood"`
	Energy     int    `json:"energy"`
	Wins       string `json:"wins"`
	Challenges string `json:"challenges"`
	Notes      string `json:"notes"`
	// RelatedGoalID and RelatedGoalTitle are set when it was about a goal.
	RelatedGoalID    *uuid.UUID `json:"relatedGoalId,omitempty"`
	RelatedGoalTitle string     `json:"relatedGoalTitle,omitempty"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type CheckInList struct {
	// Today is absent until today's check-in exists.
	Today  *CheckInView  `json:"today,omitempty"`
	Recent []CheckInView `json:"recent"`
	// Streak is consecutive days with a check-in, ending today or yesterday.
	Streak int `json:"streak"`
}

type CheckInRequest struct {
	Mood          int        `json:"mood"`
	Energy        int        `json:"energy"`
	Wins          string     `json:"wins"`
	Challenges    string     `json:"challenges"`
	Notes         string     `json:"notes"`
	RelatedGoalID *uuid.UUID `json:"relatedGoalId,omitempty"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	out := CheckInList{Recent: []CheckInView{}}

	today, err := a.svc.Today(r.Context(), user)
	switch {
	case err == nil:
		view := project(today)
		out.Today = &view
	case !apperr.Is(err, apperr.ErrNotFound):
		httpx.Error(w, err, "Check-ins could not be loaded.")
		return
	}

	recent, err := a.svc.List(r.Context(), user.ID, 30)
	if err != nil {
		httpx.Error(w, err, "Check-ins could not be loaded.")
		return
	}
	for _, c := range recent {
		out.Recent = append(out.Recent, project(c))
	}
	if out.Streak, err = a.svc.Streak(r.Context(), user); err != nil {
		httpx.Error(w, err, "Check-ins could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// upsertToday is the one check-in a day: filing it again replaces it, as the
// web form does.
func (a *API) upsertToday(w http.ResponseWriter, r *http.Request) {
	var req CheckInRequest
	if !readJSON(w, r, &req) {
		return
	}
	saved, err := a.svc.UpsertToday(r.Context(), auth.MustUser(r.Context()), Input(req))
	if err != nil {
		httpx.Error(w, err, "The check-in could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project(saved))
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "checkInID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	var req CheckInRequest
	if !readJSON(w, r, &req) {
		return
	}
	saved, err := a.svc.Update(r.Context(), id, auth.MustUser(r.Context()).ID, Input(req))
	if err != nil {
		httpx.Error(w, err, "The check-in could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project(saved))
}

func (a *API) destroy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "checkInID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	if err := a.svc.Delete(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The check-in could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func project(c CheckIn) CheckInView {
	return CheckInView{
		ID: c.ID, LocalDate: c.LocalDate.Format("2006-01-02"), Mood: c.Mood, Energy: c.Energy,
		Wins: c.Wins, Challenges: c.Challenges, Notes: c.Notes,
		RelatedGoalID: c.RelatedGoalID, RelatedGoalTitle: c.RelatedGoalTitle, UpdatedAt: c.UpdatedAt,
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
