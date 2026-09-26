package soreness

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/soreness/sore"
)

// API is today's soreness for native clients.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/soreness", a.today)
	r.Put("/soreness/{region}", a.set)
	r.Delete("/soreness/{region}", a.clear)
}

type SorenessRequest struct {
	// Severity is 1 (stiff) to 3 (painful).
	Severity int    `json:"severity"`
	Note     string `json:"note,omitempty"`
}

type SorenessEntryView struct {
	Region   string `json:"region"`
	Severity int    `json:"severity"`
	Note     string `json:"note,omitempty"`
}

type SorenessTodayView struct {
	Entries []SorenessEntryView `json:"entries"`
	// Regions is every region, head to foot.
	Regions []string `json:"regions"`
}

func (a *API) today(w http.ResponseWriter, r *http.Request) { a.respond(w, r, http.StatusOK) }

func (a *API) set(w http.ResponseWriter, r *http.Request) {
	var req SorenessRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	if _, err := a.svc.Set(r.Context(), auth.MustUser(r.Context()), chi.URLParam(r, "region"), req.Severity, req.Note); err != nil {
		httpx.Error(w, err, "The soreness could not be saved.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) clear(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Clear(r.Context(), auth.MustUser(r.Context()), chi.URLParam(r, "region")); err != nil {
		httpx.Error(w, err, "The soreness could not be cleared.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) respond(w http.ResponseWriter, r *http.Request, status int) {
	entries, err := a.svc.OnDate(r.Context(), auth.MustUser(r.Context()), time.Now())
	if err != nil {
		httpx.Error(w, err, "Today's soreness could not be loaded.")
		return
	}
	httpx.WriteJSON(w, status, ProjectToday(entries))
}

// ProjectToday is the today payload.
func ProjectToday(entries []Entry) SorenessTodayView {
	out := SorenessTodayView{Entries: make([]SorenessEntryView, len(entries)), Regions: sore.Regions()}
	for i, e := range entries {
		out.Entries[i] = SorenessEntryView{Region: e.Region, Severity: e.Severity, Note: e.Note}
	}
	return out
}
