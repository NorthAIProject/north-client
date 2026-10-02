package weekly

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/weekly/week"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// API is the weekly review for native clients. Mount behind auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/weekly/review", a.review)
	r.Put("/weekly/focus", a.setFocus)
}

type FocusView struct {
	// WeekStart is the Monday of the week, YYYY-MM-DD.
	WeekStart  string    `json:"weekStart"`
	Priorities []string  `json:"priorities"`
	Volume     string    `json:"volume"`
	ReviewedAt time.Time `json:"reviewedAt"`
}

type ReviewGoal struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Category string    `json:"category"`
	// Priority is the person's rank, 1 first; 0 is unranked.
	Priority int `json:"priority"`
}

type ReviewReport struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	// Body is markdown; empty while the report is being written.
	Body  string `json:"body"`
	Ready bool   `json:"ready"`
}

type ReviewView struct {
	// Reviewing is the Monday of the week that happened; planning is the
	// Monday of the week the new focus is for. Both YYYY-MM-DD.
	Reviewing string        `json:"reviewing"`
	Planning  string        `json:"planning"`
	Report    *ReviewReport `json:"report,omitempty"`
	Last      *FocusView    `json:"last,omitempty"`
	Current   *FocusView    `json:"current,omitempty"`
	Goals     []ReviewGoal  `json:"goals"`
}

type FocusRequest struct {
	Priorities []string    `json:"priorities"`
	GoalOrder  []uuid.UUID `json:"goalOrder"`
	Volume     string      `json:"volume"`
}

func (a *API) review(w http.ResponseWriter, r *http.Request) {
	rv, err := a.svc.Review(r.Context(), auth.MustUser(r.Context()))
	if err != nil {
		httpx.Error(w, err, "The weekly review could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectReview(rv))
}

func (a *API) setFocus(w http.ResponseWriter, r *http.Request) {
	var req FocusRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	f, err := a.svc.SetFocus(r.Context(), auth.MustUser(r.Context()), Input{
		Priorities: req.Priorities, GoalOrder: req.GoalOrder, Volume: plan.Volume(req.Volume),
	})
	if err != nil {
		httpx.Error(w, err, "The week's focus could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectFocus(f))
}

func projectReview(rv Review) ReviewView {
	out := ReviewView{
		Reviewing: rv.Reviewing.Format(time.DateOnly),
		Planning:  rv.Planning.Format(time.DateOnly),
		Goals:     make([]ReviewGoal, 0, len(rv.Goals)),
	}
	if rv.Report != nil {
		out.Report = &ReviewReport{ID: rv.Report.ID, Title: rv.Report.Title, Body: rv.Report.Body, Ready: rv.Report.Ready}
	}
	if rv.Last != nil {
		v := projectFocus(*rv.Last)
		out.Last = &v
	}
	if rv.Current != nil {
		v := projectFocus(*rv.Current)
		out.Current = &v
	}
	for _, g := range rv.Goals {
		out.Goals = append(out.Goals, ReviewGoal{ID: g.ID, Title: g.Title, Category: g.Category, Priority: g.Priority})
	}
	return out
}

func projectFocus(f week.Focus) FocusView {
	priorities := f.Priorities
	if priorities == nil {
		priorities = []string{}
	}
	return FocusView{WeekStart: f.WeekStart.Format(time.DateOnly), Priorities: priorities, Volume: string(f.Volume), ReviewedAt: f.ReviewedAt}
}
