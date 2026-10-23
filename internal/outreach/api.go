package outreach

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the coach's unprompted message for native clients. Mount behind
// auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/outreach/today", a.today)
	r.Post("/outreach/{id}/actions", a.act)
}

// OutreachView is one day's message from the coach and its answers.
type OutreachView struct {
	ID  uuid.UUID `json:"id"`
	Day string    `json:"day"`
	// Trigger is low_recovery, missed_session or first_week, as an open string.
	Trigger string `json:"trigger"`
	Body    string `json:"body"`
	// Session is the plan session concerned.
	Session string `json:"session,omitempty"`
	// Actions are the answers still open: undo, lighter, keep, today,
	// tomorrow, let_go, done (open strings). Empty once answered.
	Actions []string `json:"actions"`
	// Action is the answer given, and Outcome what it did, in the person's
	// language.
	Action  string     `json:"action,omitempty"`
	Outcome string     `json:"outcome,omitempty"`
	ActedAt *time.Time `json:"actedAt,omitempty"`
}

// OutreachToday is today's message, absent when the coach has not spoken.
type OutreachToday struct {
	Outreach *OutreachView `json:"outreach,omitempty"`
}

// ActionRequest is an answer.
type ActionRequest struct {
	Action string `json:"action"`
}

func (a *API) today(w http.ResponseWriter, r *http.Request) {
	o, ok, err := a.svc.Today(r.Context(), auth.MustUser(r.Context()))
	if err != nil {
		httpx.Error(w, err, "Today's message could not be loaded.")
		return
	}
	var out OutreachToday
	if ok && o.Body != "" {
		v := Project(r, o)
		out.Outreach = &v
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// act answers an outreach. The id may be "today", which is what a
// notification button sends: the banner knows the day but not the row.
func (a *API) act(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	var req ActionRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 1 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	var o Outreach
	var err error
	if idText := chi.URLParam(r, "id"); idText == "today" {
		o, err = a.svc.ActToday(r.Context(), user, req.Action)
	} else if id, parseErr := uuid.Parse(idText); parseErr != nil {
		err = apperr.ErrNotFound
	} else {
		o, err = a.svc.Act(r.Context(), user, id, req.Action)
	}
	if err != nil {
		httpx.Error(w, err, "That answer could not be applied.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Project(r, o))
}

func Project(r *http.Request, o Outreach) OutreachView {
	v := OutreachView{
		ID: o.ID, Day: o.Day.Format(time.DateOnly), Trigger: o.Trigger, Body: o.Body,
		Session: o.Facts.Session, Actions: []string{}, Action: o.Action, ActedAt: o.ActedAt,
	}
	if o.Answered() {
		v.Outcome = Outcome(r.Context(), o.Action)
	} else {
		v.Actions = o.Actions()
	}
	return v
}
