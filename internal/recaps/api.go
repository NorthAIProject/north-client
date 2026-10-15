package recaps

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/recaps/recap"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the recap for native clients. Mount behind auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/recap", a.get)
	r.Post("/recap/share", a.share)
}

type TotalsView struct {
	ActiveDays int `json:"activeDays"`
	Workouts   int `json:"workouts"`
	Sets       int `json:"sets"`
	FoodLogs   int `json:"foodLogs"`
	CheckIns   int `json:"checkIns"`
}

type DayView struct {
	// Date is YYYY-MM-DD.
	Date     string `json:"date"`
	Workouts int    `json:"workouts"`
	Sets     int    `json:"sets"`
	FoodLogs int    `json:"foodLogs"`
	CheckIns int    `json:"checkIns"`
}

type HighlightView struct {
	// Kind is an achievement kind; an open set, so clients must not switch
	// on it exhaustively.
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Detail     string    `json:"detail"`
	OccurredAt time.Time `json:"occurredAt"`
}

type RecapView struct {
	// Range is week or month.
	Range string `json:"range"`
	// Since and Until are the first and last day, inclusive, YYYY-MM-DD.
	Since         string          `json:"since"`
	Until         string          `json:"until"`
	Totals        TotalsView      `json:"totals"`
	Prior         TotalsView      `json:"prior"`
	CheckInStreak int             `json:"checkInStreak"`
	Days          []DayView       `json:"days"`
	Highlights    []HighlightView `json:"highlights"`
}

type ShareRequest struct {
	Range string `json:"range"`
	// ShareTraining also turns on training sharing, so followers can see
	// the post. Absent leaves sharing as it is.
	ShareTraining bool `json:"shareTraining"`
}

type ShareView struct {
	ID uuid.UUID `json:"id"`
	// TrainingShared false means the post exists but followers cannot see
	// it until the person shares training.
	TrainingShared bool `json:"trainingShared"`
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("range")
	if key == "" {
		key = recap.KeyWeek
	}
	rc, err := a.svc.Load(r.Context(), auth.MustUser(r.Context()), key)
	if err != nil {
		httpx.Error(w, err, "The recap could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectRecap(rc))
}

func (a *API) share(w http.ResponseWriter, r *http.Request) {
	var req ShareRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 1 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	shared, err := a.svc.Share(r.Context(), auth.MustUser(r.Context()), req.Range, req.ShareTraining)
	if err != nil {
		httpx.Error(w, err, "The recap could not be shared.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ShareView{ID: shared.ID, TrainingShared: shared.TrainingShared})
}

func projectRecap(rc recap.Recap) RecapView {
	out := RecapView{
		Range:         rc.Key,
		Since:         rc.Since.Format(time.DateOnly),
		Until:         rc.Until.Format(time.DateOnly),
		Totals:        projectTotals(rc.Totals),
		Prior:         projectTotals(rc.Prior),
		CheckInStreak: rc.CheckInStreak,
		Days:          make([]DayView, 0, len(rc.Days)),
		Highlights:    make([]HighlightView, 0, len(rc.Highlights)),
	}
	for _, d := range rc.Days {
		out.Days = append(out.Days, DayView{
			Date: d.Date.Format(time.DateOnly), Workouts: d.Workouts, Sets: d.Sets, FoodLogs: d.FoodLogs, CheckIns: d.CheckIns,
		})
	}
	for _, h := range rc.Highlights {
		out.Highlights = append(out.Highlights, HighlightView{Kind: h.Kind, Title: h.Title, Detail: h.Detail, OccurredAt: h.OccurredAt})
	}
	return out
}

func projectTotals(t recap.Totals) TotalsView {
	return TotalsView{ActiveDays: t.ActiveDays, Workouts: t.Workouts, Sets: t.Sets, FoodLogs: t.FoodLogs, CheckIns: t.CheckIns}
}
