package mind

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the journal for native clients: the web /mind page's entries, the
// mood trend beside them, and writing one.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/mind/journal", a.list)
	r.Post("/mind/journal", a.create)
}

type JournalEntryView struct {
	ID      uuid.UUID `json:"id"`
	Content string    `json:"content"`
	// Mood is 1-5 when given.
	Mood      *int      `json:"mood,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type MoodTrendView struct {
	AverageMood   float64 `json:"averageMood"`
	AverageEnergy float64 `json:"averageEnergy"`
	// Count is how many check-ins the averages come from.
	Count int `json:"count"`
}

type Journal struct {
	Entries []JournalEntryView `json:"entries"`
	Trend   MoodTrendView      `json:"trend"`
}

type JournalRequest struct {
	Content string `json:"content"`
	Mood    *int   `json:"mood,omitempty"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUser(r.Context()).ID
	entries, err := a.svc.Recent(r.Context(), userID, 50)
	if err != nil {
		httpx.Error(w, err, "The journal could not be loaded.")
		return
	}
	trend, err := a.svc.RecentMoodTrend(r.Context(), userID, 14)
	if err != nil {
		httpx.Error(w, err, "The journal could not be loaded.")
		return
	}
	out := Journal{
		Entries: make([]JournalEntryView, 0, len(entries)),
		Trend:   MoodTrendView(trend),
	}
	for _, e := range entries {
		out.Entries = append(out.Entries, project(e))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var req JournalRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 64 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	entry, err := a.svc.Create(r.Context(), auth.MustUser(r.Context()).ID, Input(req))
	if err != nil {
		httpx.Error(w, err, "The entry could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, project(entry))
}

func project(e JournalEntry) JournalEntryView {
	out := JournalEntryView{ID: e.ID, Content: e.Content, CreatedAt: e.CreatedAt}
	if e.Mood != nil {
		m := int(*e.Mood)
		out.Mood = &m
	}
	return out
}
