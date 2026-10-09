package lighterday

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/insights/score"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the lighter-day offer for native clients. Mount behind
// auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/today/lighter", a.show)
	r.Put("/today/lighter", a.choose)
}

type ReadinessView struct {
	// Low is true when today's recovery scores uneven or low against the
	// person's usual — the same recovery GET /insights/recovery reports.
	Low bool `json:"low"`
	// The readings, when Health has them: today's and the usual (the mean of
	// the 28 days before). Kept for builds that word the reason themselves.
	HRV         *float64 `json:"hrv,omitempty"`
	HRVBaseline *float64 `json:"hrvBaseline,omitempty"`
	RHR         *float64 `json:"restingHeartRate,omitempty"`
	RHRBaseline *float64 `json:"restingHeartRateBaseline,omitempty"`
	// Reason says which signals are off their usual, in the words the
	// Progress screen uses. Empty when none are.
	Reason string `json:"reason,omitempty"`
}

type LighterView struct {
	// Offered is true when the app should ask: a low morning, a session
	// still to do today, and no answer yet.
	Offered   bool          `json:"offered"`
	Readiness ReadinessView `json:"readiness"`
	// Session is today's plan session, when there is one.
	Session string `json:"session,omitempty"`
	// Choice is today's answer: lighter, keep, or absent.
	Choice string `json:"choice,omitempty"`
}

type ChoiceRequest struct {
	Choice string `json:"choice"`
}

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	t, err := a.svc.Today(r.Context(), auth.MustUser(r.Context()))
	if err != nil {
		httpx.Error(w, err, "Today could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project(t))
}

func (a *API) choose(w http.ResponseWriter, r *http.Request) {
	var req ChoiceRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 1 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	t, err := a.svc.Choose(r.Context(), auth.MustUser(r.Context()), req.Choice)
	if err != nil {
		httpx.Error(w, err, "That could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project(t))
}

func project(t Today) LighterView {
	r := t.Recovery
	v := LighterView{
		Offered: t.Offered(), Session: t.Session, Choice: t.Choice,
		Readiness: ReadinessView{Low: r.Low(), Reason: r.Why()},
	}
	if sig, ok := r.Signal(score.RecoveryHRV); ok {
		v.Readiness.HRV, v.Readiness.HRVBaseline = ptr(sig.Usual.Latest.Value), ptr(sig.Usual.Baseline.Mean)
	}
	if sig, ok := r.Signal(score.RecoveryRestingHR); ok {
		v.Readiness.RHR, v.Readiness.RHRBaseline = ptr(sig.Usual.Latest.Value), ptr(sig.Usual.Baseline.Mean)
	}
	return v
}

func ptr(f float64) *float64 { return &f }
