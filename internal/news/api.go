package news

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the news ticker for native clients: the latest items from the feeds
// the person follows. Feeds are chosen on the web.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/news", a.ticker)
}

type NewsItemView struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"publishedAt"`
}

type NewsTicker struct {
	// Enabled is false when the person turned the ticker off.
	Enabled bool           `json:"enabled"`
	Items   []NewsItemView `json:"items"`
}

func (a *API) ticker(w http.ResponseWriter, r *http.Request) {
	// A deployment without feeds configured has no news service.
	if a.svc == nil {
		httpx.WriteJSON(w, http.StatusOK, NewsTicker{Items: []NewsItemView{}})
		return
	}
	t, err := a.svc.Ticker(r.Context(), auth.MustUser(r.Context()), 20)
	if err != nil {
		httpx.Error(w, err, "The news could not be loaded.")
		return
	}
	out := NewsTicker{Enabled: t.Enabled, Items: make([]NewsItemView, 0, len(t.Items))}
	for _, it := range t.Items {
		out.Items = append(out.Items, NewsItemView{Title: it.Title, URL: it.URL, Source: it.Source, PublishedAt: it.PublishedAt})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
