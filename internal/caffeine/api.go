package caffeine

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/caffeine/caffeine"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is caffeine for native clients. Every change answers with today's
// drinks, so a client never adds anything up itself.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/caffeine", a.today)
	r.Post("/caffeine", a.log)
	r.Delete("/caffeine/{entryID}", a.undo)
}

type CaffeineRequest struct {
	// Preset is espresso, coffee, tea, energy_drink or cola; it fills mg and
	// label when they are left out.
	Preset   string     `json:"preset,omitempty"`
	MG       int        `json:"mg,omitempty"`
	Label    string     `json:"label,omitempty"`
	LoggedAt *time.Time `json:"loggedAt,omitempty"`
}

type CaffeineEntryView struct {
	ID       uuid.UUID `json:"id"`
	MG       int       `json:"mg"`
	Label    string    `json:"label"`
	LoggedAt time.Time `json:"loggedAt"`
}

type CaffeineTodayView struct {
	TotalMG  int                 `json:"totalMg"`
	ActiveMG int                 `json:"activeMg"`
	LimitMG  int                 `json:"limitMg"`
	Entries  []CaffeineEntryView `json:"entries"`
}

func (a *API) today(w http.ResponseWriter, r *http.Request) { a.respond(w, r, http.StatusOK) }

func (a *API) log(w http.ResponseWriter, r *http.Request) {
	var req CaffeineRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	in := LogInput{Preset: req.Preset, MG: req.MG, Label: req.Label, At: req.LoggedAt}
	if _, err := a.svc.Log(r.Context(), auth.MustUser(r.Context()), in); err != nil {
		httpx.Error(w, err, "The caffeine could not be logged.")
		return
	}
	a.respond(w, r, http.StatusCreated)
}

func (a *API) undo(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "entryID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "No such drink.")
		return
	}
	if err := a.svc.Undo(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		httpx.Error(w, err, "That could not be undone.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) respond(w http.ResponseWriter, r *http.Request, status int) {
	entries, err := a.svc.Today(r.Context(), auth.MustUser(r.Context()))
	if err != nil {
		httpx.Error(w, err, "Today's caffeine could not be loaded.")
		return
	}
	httpx.WriteJSON(w, status, ProjectToday(entries, time.Now()))
}

// ProjectToday is the today payload.
func ProjectToday(entries []Entry, now time.Time) CaffeineTodayView {
	out := CaffeineTodayView{
		TotalMG:  caffeine.Total(entries),
		ActiveMG: int(caffeine.Active(entries, now)),
		LimitMG:  caffeine.DailyLimitMG,
		Entries:  make([]CaffeineEntryView, len(entries)),
	}
	for i, e := range entries {
		out.Entries[i] = CaffeineEntryView{ID: e.ID, MG: e.MG, Label: e.Label, LoggedAt: e.LoggedAt}
	}
	return out
}
