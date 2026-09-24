package reports

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the coach's written reviews for native clients: the weekly reports
// and daily briefings the web /reports page lists, their text, archiving and
// the helpful rating the coach learns from.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/reports", a.list)
	r.Get("/reports/{reportID}", a.show)
	r.Post("/reports/{reportID}/archive", a.archive)
	r.Put("/reports/{reportID}/helpful", a.rate)
}

type ReportSummary struct {
	ID uuid.UUID `json:"id"`
	// Kind is weekly or daily.
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// PeriodStart and PeriodEnd are the calendar days it covers.
	PeriodStart string `json:"periodStart"`
	PeriodEnd   string `json:"periodEnd"`
	// Status is pending (being written), ready or failed.
	Status      string     `json:"status"`
	GeneratedAt *time.Time `json:"generatedAt,omitempty"`
	Archived    bool       `json:"archived"`
	// Helpful is the person's rating, absent until they give one.
	Helpful *bool `json:"helpful,omitempty"`
}

type ReportList struct {
	Reports []ReportSummary `json:"reports"`
}

type ReportDetail struct {
	ReportSummary
	// Body is Markdown.
	Body      string `json:"body"`
	LastError string `json:"lastError,omitempty"`
}

type HelpfulRequest struct {
	// Helpful is true, false, or null to clear the rating.
	Helpful *bool `json:"helpful"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	archived := r.URL.Query().Get("archived") == "true"
	list, err := a.svc.List(r.Context(), auth.MustUser(r.Context()).ID, archived)
	if err != nil {
		httpx.Error(w, err, "Reports could not be loaded.")
		return
	}
	out := ReportList{Reports: make([]ReportSummary, 0, len(list))}
	for _, rep := range list {
		out.Reports = append(out.Reports, summarize(rep))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	id, ok := reportID(w, r)
	if !ok {
		return
	}
	rep, err := a.svc.Get(r.Context(), id, auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The report could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail(rep))
}

func (a *API) archive(w http.ResponseWriter, r *http.Request) {
	id, ok := reportID(w, r)
	if !ok {
		return
	}
	if err := a.svc.Archive(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The report could not be archived.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) rate(w http.ResponseWriter, r *http.Request) {
	id, ok := reportID(w, r)
	if !ok {
		return
	}
	var req HelpfulRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 1 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	rep, err := a.svc.SetHelpful(r.Context(), id, auth.MustUser(r.Context()).ID, req.Helpful)
	if err != nil {
		httpx.Error(w, err, "The rating could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail(rep))
}

func summarize(rep Report) ReportSummary {
	return ReportSummary{
		ID: rep.ID, Kind: string(rep.Kind), Title: rep.Title,
		PeriodStart: rep.PeriodStart.Format("2006-01-02"), PeriodEnd: rep.PeriodEnd.Format("2006-01-02"),
		Status: string(rep.Status), GeneratedAt: rep.GeneratedAt, Archived: rep.Archived(), Helpful: rep.Helpful,
	}
}

func detail(rep Report) ReportDetail {
	return ReportDetail{ReportSummary: summarize(rep), Body: rep.Body, LastError: rep.LastError}
}

func reportID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "reportID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}
