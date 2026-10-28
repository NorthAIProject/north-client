package moderation

import (
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
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
	// The operator queue, for the people named in OPERATOR_USER_IDS; anyone
	// else is not found, as on the web.
	r.Get("/ops/moderation", a.queue)
	r.Post("/ops/moderation/users/{userID}/suspend", a.suspend)
	r.Post("/ops/moderation/{kind}/{subjectID}/{action}", a.decide)
}

// ReportGroupView is one reported subject in the operator queue.
type ReportGroupView struct {
	// Kind is the subject's kind, such as crew_message. Open-ended.
	Kind      string     `json:"kind"`
	SubjectID uuid.UUID  `json:"subjectId"`
	AuthorID  *uuid.UUID `json:"authorId,omitempty"`
	// Reasons counts the open reports by reason, sorted by reason.
	Reasons []ReasonCount `json:"reasons"`
	Count   int           `json:"count"`
	Hidden  bool          `json:"hidden"`
	Excerpt string        `json:"excerpt"`
	FirstAt time.Time     `json:"firstAt"`
	// Actions are the decisions offered, in order: hide and dismiss, or
	// unhide and hide for a subject already hidden.
	Actions []string `json:"actions"`
}

type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// QueueView is every subject with open reports, oldest first.
type QueueView struct {
	Reports []ReportGroupView `json:"reports"`
}

// SuspendRequest is how long to stop someone posting; 0 or absent is until
// lifted.
type SuspendRequest struct {
	Hours int `json:"hours"`
}

func projectGroup(g ReportGroup) ReportGroupView {
	v := ReportGroupView{
		Kind: g.Kind, SubjectID: g.SubjectID, AuthorID: g.AuthorID, Count: g.Count,
		Hidden: g.Hidden, Excerpt: g.Excerpt, FirstAt: g.FirstAt, Reasons: make([]ReasonCount, 0, len(g.Reasons)),
	}
	for reason, n := range g.Reasons {
		v.Reasons = append(v.Reasons, ReasonCount{Reason: reason, Count: n})
	}
	sort.Slice(v.Reasons, func(i, j int) bool { return v.Reasons[i].Reason < v.Reasons[j].Reason })
	if g.Hidden {
		v.Actions = []string{DecideUnhide, DecideHide}
	} else {
		v.Actions = []string{DecideHide, DecideDismiss}
	}
	return v
}

func (a *API) queue(w http.ResponseWriter, r *http.Request) {
	if !a.svc.IsOperator(auth.MustUser(r.Context()).ID) {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	groups, err := a.svc.OpenReports(r.Context())
	if err != nil {
		httpx.Error(w, err, "The reports could not be loaded.")
		return
	}
	out := QueueView{Reports: make([]ReportGroupView, 0, len(groups))}
	for _, g := range groups {
		out.Reports = append(out.Reports, projectGroup(g))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) decide(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "subjectID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	err = a.svc.Decide(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "kind"), id, chi.URLParam(r, "action"))
	if err != nil {
		httpx.Error(w, err, "That could not be saved.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// suspend stops a user posting for hours, or until lifted when hours is 0.
func (a *API) suspend(w http.ResponseWriter, r *http.Request) {
	op := auth.MustUser(r.Context()).ID
	if !a.svc.IsOperator(op) {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	target, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	var req SuspendRequest
	if r.ContentLength != 0 {
		if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 1 << 10}); err != nil {
			httpx.Error(w, err, "That request could not be read.")
			return
		}
	}
	var until *time.Time
	if req.Hours > 0 {
		t := time.Now().Add(time.Duration(req.Hours) * time.Hour)
		until = &t
	}
	if err := a.svc.Suspend(r.Context(), &op, target, until, "operator"); err != nil {
		httpx.Error(w, err, "That could not be saved.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
