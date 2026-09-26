package fasting

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/fasting/fast"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is fasting for native clients. Every change answers with the current
// state, so the phone never works out a phase itself.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/fasting", a.show)
	r.Post("/fasting/start", a.start)
	r.Post("/fasting/stop", a.stop)
}

type FastStartRequest struct {
	// TargetHours defaults to 16.
	TargetHours int        `json:"targetHours,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
}

type FastView struct {
	ID             uuid.UUID  `json:"id"`
	StartedAt      time.Time  `json:"startedAt"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
	TargetHours    int        `json:"targetHours"`
	ElapsedMinutes int        `json:"elapsedMinutes"`
	// Phase is fed, fasting, fat_burning or ketosis.
	Phase string `json:"phase"`
}

type FastingView struct {
	Current *FastView `json:"current,omitempty"`
}

func (a *API) show(w http.ResponseWriter, r *http.Request) { a.respond(w, r, http.StatusOK) }

func (a *API) start(w http.ResponseWriter, r *http.Request) {
	var req FastStartRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	if _, err := a.svc.Start(r.Context(), auth.MustUser(r.Context()), req.TargetHours, req.StartedAt); err != nil {
		httpx.Error(w, err, "The fast could not be started.")
		return
	}
	a.respond(w, r, http.StatusCreated)
}

func (a *API) stop(w http.ResponseWriter, r *http.Request) {
	if _, err := a.svc.Stop(r.Context(), auth.MustUser(r.Context())); err != nil {
		httpx.Error(w, err, "There is no fast to end.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) respond(w http.ResponseWriter, r *http.Request, status int) {
	current, ok, err := a.svc.Current(r.Context(), auth.MustUser(r.Context()))
	if err != nil {
		httpx.Error(w, err, "Fasting could not be loaded.")
		return
	}
	out := FastingView{}
	if ok {
		v := ProjectFast(current, time.Now())
		out.Current = &v
	}
	httpx.WriteJSON(w, status, out)
}

// ProjectFast is one fast's JSON shape.
func ProjectFast(s Session, now time.Time) FastView {
	elapsed := s.Elapsed(now)
	return FastView{
		ID: s.ID, StartedAt: s.StartedAt, EndedAt: s.EndedAt, TargetHours: s.TargetHours,
		ElapsedMinutes: int(elapsed.Minutes()), Phase: string(fast.PhaseAt(elapsed)),
	}
}
