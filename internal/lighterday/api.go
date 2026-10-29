package lighterday

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/insights/score"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/users"
)

// API is the lighter-day offer for native clients. Register behind
// auth.Bearer.
type API struct {
	svc *Service
	// coveredBy is the web handler's: a day the coach's own message already
	// speaks about recovery, which the response reports as covered.
	coveredBy func(context.Context, users.User) bool
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

// WithCoveredBy reports, as covered, days the coach's message already covers.
func (a *API) WithCoveredBy(covered func(context.Context, users.User) bool) *API {
	a.coveredBy = covered
	return a
}

func (a *API) Register(g *echo.Group) {
	g.GET("/today/lighter", a.show)
	g.PUT("/today/lighter", a.choose)
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
	// Source is who chose: person, or coach when the coach lightened a low
	// morning itself. Absent with no choice. An open string. A lighter day
	// is undone with PUT choice=keep.
	Source string `json:"source,omitempty"`
	// Covered is true when today's coach message already speaks about
	// recovery with the same answers; the web card then stays out of the way
	// rather than offering a second Undo.
	Covered bool `json:"covered,omitempty"`
}

type ChoiceRequest struct {
	Choice string `json:"choice"`
}

func (a *API) show(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	t, err := a.svc.Today(ctx, user)
	if err != nil {
		return httpx.Fail(err, "Today could not be loaded.")
	}
	v := project(ctx, t)
	v.Covered = a.coveredBy != nil && a.coveredBy(ctx, user)
	return c.JSON(http.StatusOK, v)
}

func (a *API) choose(c *echo.Context) error {
	var req ChoiceRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 1 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	t, err := a.svc.Choose(ctx, auth.MustUser(ctx), req.Choice)
	if err != nil {
		return httpx.Fail(err, "That could not be saved.")
	}
	return c.JSON(http.StatusOK, project(ctx, t))
}

func project(ctx context.Context, t Today) LighterView {
	r := t.Recovery
	v := LighterView{
		Offered: t.Offered(), Session: t.Session, Choice: t.Choice, Source: t.Source,
		Readiness: ReadinessView{Low: r.Low(), Reason: r.WhyIn(ctx)},
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
