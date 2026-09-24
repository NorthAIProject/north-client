package activity

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the activity timer for native clients: the web /activity page's
// start, pause, resume, stop, cancel and manual log. A phone workout runs one
// of these sessions, so the time and calories land where the web and the
// coach already read them.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/activity", a.overview)
	r.Post("/activity/start", a.start)
	r.Post("/activity/log", a.log)
	r.Post("/activity/{sessionID}/pause", a.pause)
	r.Post("/activity/{sessionID}/resume", a.resume)
	r.Post("/activity/{sessionID}/stop", a.stop)
	r.Delete("/activity/{sessionID}", a.cancel)
}

type SessionView struct {
	ID           uuid.UUID `json:"id"`
	ActivityCode string    `json:"activityCode"`
	ActivityName string    `json:"activityName"`
	// Source is manual or strava.
	Source string `json:"source"`
	// Status is active, paused, completed or cancelled.
	Status             string     `json:"status"`
	StartedAt          time.Time  `json:"startedAt"`
	PausedAt           *time.Time `json:"pausedAt,omitempty"`
	TotalPausedSeconds int        `json:"totalPausedSeconds"`
	EndedAt            *time.Time `json:"endedAt,omitempty"`
	// ElapsedSeconds is moving time as of this response; a client ticks it
	// forward from StartedAt and the paused total rather than polling.
	ElapsedSeconds int      `json:"elapsedSeconds"`
	CaloriesBurned *float64 `json:"caloriesBurned,omitempty"`
	DistanceM      *float64 `json:"distanceM,omitempty"`
}

type ActivityKind struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

type Overview struct {
	Active *SessionView   `json:"active,omitempty"`
	Recent []SessionView  `json:"recent"`
	Kinds  []ActivityKind `json:"kinds"`
}

type StartRequest struct {
	ActivityCode string `json:"activityCode"`
}

type LogRequest struct {
	ActivityCode string `json:"activityCode"`
	// StartedAt defaults to now minus the duration.
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	DurationMinutes int        `json:"durationMinutes"`
	DistanceKm      *float64   `json:"distanceKm,omitempty"`
}

func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUser(r.Context()).ID
	now := time.Now()

	out := Overview{Recent: []SessionView{}, Kinds: make([]ActivityKind, 0, len(METTable))}
	if active, ok, err := a.svc.Active(r.Context(), userID); err != nil {
		httpx.Error(w, err, "Activity could not be loaded.")
		return
	} else if ok {
		view := project(active, now)
		out.Active = &view
	}
	recent, err := a.svc.List(r.Context(), userID, 20)
	if err != nil {
		httpx.Error(w, err, "Activity could not be loaded.")
		return
	}
	for _, s := range recent {
		out.Recent = append(out.Recent, project(s, now))
	}
	for _, m := range METTable {
		out.Kinds = append(out.Kinds, ActivityKind{Code: m.Code, Name: m.Name, Category: m.Category})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) start(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if !readJSON(w, r, &req) {
		return
	}
	session, err := a.svc.Start(r.Context(), auth.MustUser(r.Context()).ID, req.ActivityCode)
	a.respond(w, http.StatusCreated, session, err)
}

func (a *API) log(w http.ResponseWriter, r *http.Request) {
	var req LogRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.DurationMinutes <= 0 {
		httpx.Error(w, apperr.FieldErrors{}.Add("durationMinutes", "Say how many minutes the session lasted."), "Say how many minutes the session lasted.")
		return
	}
	in := LogInput{ActivityCode: req.ActivityCode, Duration: time.Duration(req.DurationMinutes) * time.Minute}
	if req.StartedAt != nil {
		in.StartedAt = *req.StartedAt
	}
	if req.DistanceKm != nil {
		in.DistanceM = *req.DistanceKm * 1000
	}
	session, err := a.svc.Log(r.Context(), auth.MustUser(r.Context()).ID, in)
	a.respond(w, http.StatusCreated, session, err)
}

func (a *API) pause(w http.ResponseWriter, r *http.Request) {
	a.transition(w, r, a.svc.Pause)
}

func (a *API) resume(w http.ResponseWriter, r *http.Request) {
	a.transition(w, r, a.svc.Resume)
}

func (a *API) stop(w http.ResponseWriter, r *http.Request) {
	a.transition(w, r, a.svc.Stop)
}

func (a *API) cancel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	if err := a.svc.Cancel(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		a.respond(w, 0, Session{}, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) transition(w http.ResponseWriter, r *http.Request, step func(ctx context.Context, id, userID uuid.UUID) (Session, error)) {
	id, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	session, err := step(r.Context(), id, auth.MustUser(r.Context()).ID)
	a.respond(w, http.StatusOK, session, err)
}

// respond writes the session, or the service's own reason for refusing:
// "record your biometrics before tracking activity" is worth showing as is.
func (a *API) respond(w http.ResponseWriter, status int, session Session, err error) {
	switch {
	case err == nil:
		httpx.WriteJSON(w, status, project(session, time.Now()))
	case apperr.Is(err, apperr.ErrValidation):
		message := strings.TrimSuffix(err.Error(), ": "+apperr.ErrValidation.Error())
		httpx.Error(w, apperr.FieldErrors{}.Add("activity", message), message)
	case apperr.Is(err, apperr.ErrConflict):
		httpx.Error(w, err, strings.TrimSuffix(err.Error(), ": "+apperr.ErrConflict.Error()))
	default:
		httpx.Error(w, err, "That session could not be updated.")
	}
}

func project(s Session, now time.Time) SessionView {
	name := s.ActivityCode
	if met, ok := LookupMET(s.ActivityCode); ok {
		name = met.Name
	}
	return SessionView{
		ID: s.ID, ActivityCode: s.ActivityCode, ActivityName: name, Source: s.Source, Status: s.Status,
		StartedAt: s.StartedAt, PausedAt: s.PausedAt, TotalPausedSeconds: s.TotalPausedSeconds, EndedAt: s.EndedAt,
		ElapsedSeconds: int(s.Elapsed(now).Seconds()), CaloriesBurned: s.CaloriesBurned, DistanceM: s.DistanceM,
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
