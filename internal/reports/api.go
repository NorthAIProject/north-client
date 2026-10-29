package reports

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the coach's written reviews for native clients: the weekly reports
// and daily briefings the web /reports page lists, their text, archiving and
// the helpful rating the coach learns from.
type API struct {
	svc *Service
	// quotas bounds the routes that cost a model call, as the web handler's
	// do. Nil leaves them unguarded (tests).
	quotas *quota.Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

// WithQuotas guards generate, regenerate and briefing with the report quota.
func (a *API) WithQuotas(q *quota.Service) *API {
	a.quotas = q
	return a
}

func (a *API) Register(g *echo.Group) {
	g.GET("/reports", a.list)
	g.GET("/reports/:reportID", a.show)
	g.POST("/reports/:reportID/archive", a.archive)
	g.PUT("/reports/:reportID/helpful", a.rate)

	var generating []echo.MiddlewareFunc
	if a.quotas != nil {
		generating = append(generating, a.quotas.GuardAPI(quota.ReportGenerate))
	}
	g.POST("/reports/generate", a.generate, generating...)
	g.POST("/reports/briefing", a.briefing, generating...)
	g.POST("/reports/:reportID/regenerate", a.regenerate, generating...)
}

// generate asks for this week's review now. It answers at once with the
// pending report; the text is written in the background.
func (a *API) generate(c *echo.Context) error {
	ctx := c.Request().Context()
	rep, err := a.svc.RequestGenerate(ctx, auth.MustUser(ctx).ID, time.Time{})
	if err != nil {
		return refuse(c, err, "The review could not be requested.")
	}
	return c.JSON(http.StatusAccepted, detail(rep))
}

// briefing asks for today's briefing now, as the dashboard button does.
func (a *API) briefing(c *echo.Context) error {
	ctx := c.Request().Context()
	rep, err := a.svc.RequestBriefing(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return refuse(c, err, "The briefing could not be requested.")
	}
	return c.JSON(http.StatusAccepted, detail(rep))
}

// regenerate rewrites a report. The answer may be a different report: the
// one now being written in its place.
func (a *API) regenerate(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "reportID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	rep, err := a.svc.Regenerate(ctx, id, auth.MustUser(ctx).ID)
	if err != nil {
		return refuse(c, err, "The review could not be rewritten.")
	}
	return c.JSON(http.StatusAccepted, detail(rep))
}

// refuse answers a generation failure. A conflict carries a code, since the
// web shows a different line for each: archived (the report is archived) or
// cooldown (one was already generated this hour).
func refuse(c *echo.Context, err error, message string) error {
	if !apperr.Is(err, apperr.ErrConflict) {
		return httpx.Fail(err, message)
	}
	code := "cooldown"
	if errors.Is(err, ErrArchived) {
		code = "archived"
	}
	return c.JSON(http.StatusConflict, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: message, Code: code}})
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

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	archived := c.QueryParam("archived") == "true"
	list, err := a.svc.List(ctx, auth.MustUser(ctx).ID, archived)
	if err != nil {
		return httpx.Fail(err, "Reports could not be loaded.")
	}
	out := ReportList{Reports: make([]ReportSummary, 0, len(list))}
	for _, rep := range list {
		out.Reports = append(out.Reports, summarize(rep))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) show(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "reportID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	rep, err := a.svc.Get(ctx, id, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The report could not be loaded.")
	}
	return c.JSON(http.StatusOK, detail(rep))
}

func (a *API) archive(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "reportID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.Archive(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "The report could not be archived.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) rate(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "reportID")
	if err != nil {
		return err
	}
	var req HelpfulRequest
	if err = echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 1 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	rep, err := a.svc.SetHelpful(ctx, id, auth.MustUser(ctx).ID, req.Helpful)
	if err != nil {
		return httpx.Fail(err, "The rating could not be saved.")
	}
	return c.JSON(http.StatusOK, detail(rep))
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
