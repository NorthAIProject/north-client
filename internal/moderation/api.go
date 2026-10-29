package moderation

import (
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the app's side of the terms: whether the current version is
// accepted, and accepting it.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/moderation/terms", a.terms)
	g.POST("/moderation/terms", a.accept)
	// The operator queue, for the people named in OPERATOR_USER_IDS; anyone
	// else is not found, as on the web.
	g.GET("/ops/moderation", a.queue)
	g.POST("/ops/moderation/users/:userID/suspend", a.suspend)
	g.POST("/ops/moderation/:kind/:subjectID/:action", a.decide)
}

// ReportGroupView is one reported subject in the operator queue.
type ReportGroupView struct {
	// Kind is the subject's kind, such as crew_message. Open-ended.
	Kind      string     `json:"kind"`
	SubjectID uuid.UUID  `json:"subjectId"`
	AuthorID  *uuid.UUID `json:"authorId,omitempty"`
	// Reasons counts the open reports by reason, sorted by reason.
	Reasons []ReasonCount `json:"reasons"`
	Count   int           `json:"count"`
	Hidden  bool          `json:"hidden"`
	Excerpt string        `json:"excerpt"`
	FirstAt time.Time     `json:"firstAt"`
	// Actions are the decisions offered, in order: hide and dismiss, or
	// unhide and hide for a subject already hidden.
	Actions []string `json:"actions"`
}

type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// QueueView is every subject with open reports, oldest first.
type QueueView struct {
	Reports []ReportGroupView `json:"reports"`
}

// SuspendRequest is how long to stop someone posting; 0 or absent is until
// lifted.
type SuspendRequest struct {
	Hours int `json:"hours"`
}

func projectGroup(g ReportGroup) ReportGroupView {
	v := ReportGroupView{
		Kind: g.Kind, SubjectID: g.SubjectID, AuthorID: g.AuthorID, Count: g.Count,
		Hidden: g.Hidden, Excerpt: g.Excerpt, FirstAt: g.FirstAt, Reasons: make([]ReasonCount, 0, len(g.Reasons)),
	}
	for reason, n := range g.Reasons {
		v.Reasons = append(v.Reasons, ReasonCount{Reason: reason, Count: n})
	}
	sort.Slice(v.Reasons, func(i, j int) bool { return v.Reasons[i].Reason < v.Reasons[j].Reason })
	if g.Hidden {
		v.Actions = []string{DecideUnhide, DecideHide}
	} else {
		v.Actions = []string{DecideHide, DecideDismiss}
	}
	return v
}

func (a *API) queue(c *echo.Context) error {
	ctx := c.Request().Context()
	if !a.svc.IsOperator(auth.MustUser(ctx).ID) {
		return httpx.Fail(apperr.ErrNotFound, "Not found.")
	}
	groups, err := a.svc.OpenReports(ctx)
	if err != nil {
		return httpx.Fail(err, "The reports could not be loaded.")
	}
	out := QueueView{Reports: make([]ReportGroupView, 0, len(groups))}
	for _, g := range groups {
		out.Reports = append(out.Reports, projectGroup(g))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) decide(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "subjectID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.Decide(ctx, auth.MustUser(ctx).ID, c.Param("kind"), id, c.Param("action")); err != nil {
		return httpx.Fail(err, "That could not be saved.")
	}
	return c.NoContent(http.StatusNoContent)
}

// suspend stops a user posting for hours, or until lifted when hours is 0.
func (a *API) suspend(c *echo.Context) error {
	ctx := c.Request().Context()
	op := auth.MustUser(ctx).ID
	if !a.svc.IsOperator(op) {
		return httpx.Fail(apperr.ErrNotFound, "Not found.")
	}
	target, err := echox.UUIDParam(c, "userID")
	if err != nil {
		return err
	}
	var req SuspendRequest
	if c.Request().ContentLength != 0 {
		if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 1 << 10}, "That request could not be read."); err != nil {
			return err
		}
	}
	var until *time.Time
	if req.Hours > 0 {
		t := time.Now().Add(time.Duration(req.Hours) * time.Hour)
		until = &t
	}
	if err := a.svc.Suspend(ctx, &op, target, until, "operator"); err != nil {
		return httpx.Fail(err, "That could not be saved.")
	}
	return c.NoContent(http.StatusNoContent)
}

// TermsView is the terms state for the person asking.
type TermsView struct {
	Version  int  `json:"version"`
	Accepted bool `json:"accepted"`
}

func (a *API) terms(c *echo.Context) error {
	ctx := c.Request().Context()
	ok, err := a.svc.TermsAccepted(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The terms could not be loaded.")
	}
	return c.JSON(http.StatusOK, TermsView{Version: TermsVersion, Accepted: ok})
}

func (a *API) accept(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := a.svc.AcceptTerms(ctx, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "The terms could not be saved.")
	}
	return c.NoContent(http.StatusNoContent)
}
