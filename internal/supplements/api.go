package supplements

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/supplements/supplement"
)

// API is supplements for native clients.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/supplements", a.today)
	r.Post("/supplements", a.log)
	r.Delete("/supplements/{entryID}", a.undo)
}

type SupplementRequest struct {
	Preset    string   `json:"preset,omitempty"`
	Name      string   `json:"name,omitempty"`
	Count     int      `json:"count,omitempty"`
	Nutrients []string `json:"nutrients,omitempty"`
}

type SupplementEntryView struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Count     int       `json:"count"`
	Nutrients []string  `json:"nutrients"`
	LoggedAt  time.Time `json:"loggedAt"`
}

type SupplementPresetView struct {
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Nutrients []string `json:"nutrients"`
}

type SupplementsTodayView struct {
	Entries []SupplementEntryView  `json:"entries"`
	Presets []SupplementPresetView `json:"presets"`
	// Nutrients is the tracked set, in display order.
	Nutrients []string `json:"nutrients"`
}

func (a *API) today(w http.ResponseWriter, r *http.Request) { a.respond(w, r, http.StatusOK) }

func (a *API) log(w http.ResponseWriter, r *http.Request) {
	var req SupplementRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	in := LogInput{Preset: req.Preset, Name: req.Name, Count: req.Count, Nutrients: req.Nutrients}
	if _, err := a.svc.Log(r.Context(), auth.MustUser(r.Context()), in); err != nil {
		httpx.Error(w, err, "The supplement could not be logged.")
		return
	}
	a.respond(w, r, http.StatusCreated)
}

func (a *API) undo(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "entryID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "No such entry.")
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
		httpx.Error(w, err, "Today's supplements could not be loaded.")
		return
	}
	httpx.WriteJSON(w, status, ProjectToday(entries))
}

// ProjectToday is the today payload.
func ProjectToday(entries []Entry) SupplementsTodayView {
	out := SupplementsTodayView{Entries: make([]SupplementEntryView, len(entries)), Nutrients: supplement.Nutrients()}
	for i, e := range entries {
		out.Entries[i] = SupplementEntryView{ID: e.ID, Name: e.Name, Count: e.Count, Nutrients: append([]string{}, e.Nutrients...), LoggedAt: e.LoggedAt}
	}
	for _, p := range supplement.Presets() {
		out.Presets = append(out.Presets, SupplementPresetView{Key: p.Key, Name: p.Name, Nutrients: append([]string{}, p.Nutrients...)})
	}
	return out
}
