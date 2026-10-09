package checkins

import (
	"net/http"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
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
	CreatedAt        time.Time  `json:"createdAt"`
	// Source is where the check-in was first created: web, ios, siri, coach,
	// mcp, capture, or unknown for entries from before it was recorded. Edits
	// do not change it. Open-ended; clients must tolerate values they do not
	// know.
	Source string `json:"source"`
	// Stress and SleepQuality are 1–5, absent when not given.
	Stress       *int `json:"stress,omitempty"`
	SleepQuality *int `json:"sleepQuality,omitempty"`
	// Tags are lowercase labels; empty, never absent.
	Tags []string `json:"tags"`
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
	// Stress, SleepQuality and Tags are optional. A save is a full overwrite
	// of the content fields, so leaving one out clears it.
	Stress       *int     `json:"stress,omitempty"`
	SleepQuality *int     `json:"sleepQuality,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	// Source is "ios" or "siri"; anything else is recorded as "ios". It is
	// stored only when the request creates the day's check-in.
	Source string `json:"source,omitempty"`
}

// input turns a request into a service input, recording which native surface
// sent it.
func (req CheckInRequest) input() Input {
	source := checkin.SourceIOS
	if checkin.Source(req.Source) == checkin.SourceSiri {
		source = checkin.SourceSiri
	}
	return Input{
		Mood: req.Mood, Energy: req.Energy,
		Wins: req.Wins, Challenges: req.Challenges, Notes: req.Notes,
		RelatedGoalID: req.RelatedGoalID,
		Stress:        req.Stress, SleepQuality: req.SleepQuality, Tags: req.Tags,
		Source: source,
	}
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	out := CheckInList{Recent: []CheckInView{}}

	today, err := a.svc.Today(r.Context(), user)
	switch {
	case err == nil:
		out.Today = util.Ptr(project(today))
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

// upsertToday is the one check-in a day: filing it again replaces its content,
// as the web form does, and keeps the source it was first created from.
func (a *API) upsertToday(w http.ResponseWriter, r *http.Request) {
	var req CheckInRequest
	if !readJSON(w, r, &req) {
		return
	}
	saved, err := a.svc.UpsertToday(r.Context(), auth.MustUser(r.Context()), req.input())
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
	saved, err := a.svc.Update(r.Context(), id, auth.MustUser(r.Context()).ID, req.input())
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
		CreatedAt: c.CreatedAt, Source: sourceLabel(c.Source),
		Stress: c.Stress, SleepQuality: c.SleepQuality, Tags: tagsOrEmpty(c.Tags),
	}
}

func sourceLabel(s checkin.Source) string {
	if s == "" {
		return string(checkin.SourceUnknown)
	}
	return string(s)
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
