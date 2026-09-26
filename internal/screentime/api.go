package screentime

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API takes a day's screen time from the app or from a Shortcut.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Put("/screen-time", a.set)
}

type ScreenTimeRequest struct {
	// Date is YYYY-MM-DD; today when absent.
	Date    string `json:"date,omitempty"`
	Minutes int    `json:"minutes"`
	// Source is manual or shortcut.
	Source string `json:"source,omitempty"`
}

type ScreenTimeView struct {
	Date    string `json:"date"`
	Minutes int    `json:"minutes"`
	Source  string `json:"source"`
}

func (a *API) set(w http.ResponseWriter, r *http.Request) {
	var req ScreenTimeRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	user := auth.MustUser(r.Context())
	var date *time.Time
	if req.Date != "" {
		d, err := time.ParseInLocation("2006-01-02", req.Date, user.Location())
		if err != nil {
			httpx.Error(w, err, "Date must be YYYY-MM-DD.")
			return
		}
		date = &d
	}
	day, err := a.svc.Set(r.Context(), user, date, req.Minutes, req.Source)
	if err != nil {
		httpx.Error(w, err, "The screen time could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ScreenTimeView{Date: day.LocalDate.Format("2006-01-02"), Minutes: day.Minutes, Source: day.Source})
}
