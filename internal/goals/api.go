package goals

import (
	"net/http"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
)

// dateLayout is how a goal's or milestone's target date travels: a calendar
// day, with no time or zone, exactly as the web form sends it.
const dateLayout = "2006-01-02"

// API is goals for native clients: the web /goals pages' list, detail,
// create, edit, status, progress notes and milestones, over the same service.
type API struct {
	svc *Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/goals", a.list)
	g.POST("/goals", a.create)
	g.GET("/goals/:goalID", a.show)
	g.PUT("/goals/:goalID", a.update)
	g.PUT("/goals/:goalID/status", a.setStatus)
	g.DELETE("/goals/:goalID", a.destroy)
	g.POST("/goals/:goalID/updates", a.addUpdate)
	g.POST("/goals/:goalID/milestones", a.addMilestone)
	g.PUT("/goals/:goalID/milestones/:milestoneID", a.updateMilestone)
	g.PUT("/goals/:goalID/milestones/:milestoneID/status", a.setMilestoneStatus)
	g.DELETE("/goals/:goalID/milestones/:milestoneID", a.deleteMilestone)
}

type GoalUpdateView struct {
	ID   uuid.UUID `json:"id"`
	Note string    `json:"note"`
	// Progress is 0-100 when the note carried one.
	Progress  *int      `json:"progress,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type MilestoneView struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	// Status is open or completed.
	Status      string     `json:"status"`
	TargetDate  string     `json:"targetDate,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type GoalSummary struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	// Motivation is why the goal matters, as the web list shows under the
	// title. GoalDetail carries it too.
	Motivation string `json:"motivation,omitempty"`
	Category   string `json:"category"`
	// Status is active, achieved, paused or abandoned.
	Status         string          `json:"status"`
	TargetDate     string          `json:"targetDate,omitempty"`
	MilestoneTotal int             `json:"milestoneTotal"`
	MilestoneDone  int             `json:"milestoneDone"`
	LatestUpdate   *GoalUpdateView `json:"latestUpdate,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type GoalList struct {
	Goals []GoalSummary `json:"goals"`
	// Categories are the life areas a goal can belong to, in display order.
	Categories []string `json:"categories"`
	// OverdueMilestones counts open milestones past their date, across all
	// goals: the web list's "overdue" instrument.
	OverdueMilestones int `json:"overdueMilestones"`
}

type GoalDetail struct {
	GoalSummary
	Motivation string           `json:"motivation"`
	Success    string           `json:"success"`
	Milestones []MilestoneView  `json:"milestones"`
	Updates    []GoalUpdateView `json:"updates"`
}

type GoalRequest struct {
	Title      string `json:"title"`
	Motivation string `json:"motivation"`
	Success    string `json:"success"`
	Category   string `json:"category"`
	TargetDate string `json:"targetDate,omitempty"`
}

type StatusRequest struct {
	Status string `json:"status"`
}

type UpdateRequest struct {
	Note     string `json:"note"`
	Progress *int   `json:"progress,omitempty"`
}

type MilestoneRequest struct {
	Title      string `json:"title"`
	TargetDate string `json:"targetDate,omitempty"`
}

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	goals, err := a.svc.List(ctx, userID)
	if err != nil {
		return httpx.Fail(err, "Goals could not be loaded.")
	}
	overdue, err := a.svc.CountOverdueMilestones(ctx, userID)
	if err != nil {
		return httpx.Fail(err, "Goals could not be loaded.")
	}
	out := GoalList{Goals: make([]GoalSummary, 0, len(goals)), Categories: lifedomain.Domains, OverdueMilestones: overdue}
	for _, g := range goals {
		out.Goals = append(out.Goals, summarize(g))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) create(c *echo.Context) error {
	in, err := readGoal(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	goal, err := a.svc.Create(ctx, auth.MustUser(ctx).ID, in)
	if err != nil {
		return httpx.Fail(err, "The goal could not be saved.")
	}
	return a.writeDetail(c, http.StatusCreated, goal.ID)
}

func (a *API) show(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "goalID")
	if err != nil {
		return err
	}
	return a.writeDetail(c, http.StatusOK, id)
}

func (a *API) update(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "goalID")
	if err != nil {
		return err
	}
	in, err := readGoal(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err = a.svc.Update(ctx, id, auth.MustUser(ctx).ID, in); err != nil {
		return httpx.Fail(err, "The goal could not be saved.")
	}
	return a.writeDetail(c, http.StatusOK, id)
}

func (a *API) setStatus(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "goalID")
	if err != nil {
		return err
	}
	var req StatusRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err = a.svc.SetStatus(ctx, id, auth.MustUser(ctx).ID, req.Status); err != nil {
		return httpx.Fail(err, "The goal's status could not be changed.")
	}
	return a.writeDetail(c, http.StatusOK, id)
}

func (a *API) destroy(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "goalID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err = a.svc.Delete(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "The goal could not be deleted.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) addUpdate(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "goalID")
	if err != nil {
		return err
	}
	var req UpdateRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	update, err := a.svc.AddUpdate(ctx, id, auth.MustUser(ctx).ID, req.Note, req.Progress)
	if err != nil {
		return httpx.Fail(err, "The note could not be saved.")
	}
	return c.JSON(http.StatusCreated, projectUpdate(update))
}

func (a *API) addMilestone(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "goalID")
	if err != nil {
		return err
	}
	in, err := readMilestone(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	m, err := a.svc.AddMilestone(ctx, id, auth.MustUser(ctx).ID, in)
	if err != nil {
		return httpx.Fail(err, "The milestone could not be saved.")
	}
	return c.JSON(http.StatusCreated, projectMilestone(m))
}

func (a *API) updateMilestone(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "milestoneID")
	if err != nil {
		return err
	}
	in, err := readMilestone(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	m, err := a.svc.UpdateMilestone(ctx, id, auth.MustUser(ctx).ID, in)
	if err != nil {
		return httpx.Fail(err, "The milestone could not be saved.")
	}
	return c.JSON(http.StatusOK, projectMilestone(m))
}

func (a *API) setMilestoneStatus(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "milestoneID")
	if err != nil {
		return err
	}
	var req StatusRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	m, err := a.svc.SetMilestoneStatus(ctx, id, auth.MustUser(ctx).ID, req.Status)
	if err != nil {
		return httpx.Fail(err, "The milestone could not be changed.")
	}
	return c.JSON(http.StatusOK, projectMilestone(m))
}

func (a *API) deleteMilestone(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "milestoneID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err = a.svc.DeleteMilestone(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "The milestone could not be deleted.")
	}
	return c.NoContent(http.StatusNoContent)
}

// writeDetail answers with the goal as its detail page shows it: milestones
// and the recent progress notes, read fresh after whatever changed.
func (a *API) writeDetail(c *echo.Context, status int, id uuid.UUID) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	goal, err := a.svc.Get(ctx, id, userID)
	if err != nil {
		return httpx.Fail(err, "The goal could not be loaded.")
	}
	milestones, err := a.svc.Milestones(ctx, id, userID)
	if err != nil {
		return httpx.Fail(err, "The goal could not be loaded.")
	}
	// 50, as the Go detail page showed: the web app renders this list in full.
	updates, err := a.svc.Updates(ctx, id, userID, 50)
	if err != nil {
		return httpx.Fail(err, "The goal could not be loaded.")
	}
	goal = goal.WithMilestones(milestones)

	out := GoalDetail{
		GoalSummary: summarize(goal),
		Motivation:  goal.Motivation, Success: goal.Success,
		Milestones: make([]MilestoneView, 0, len(milestones)),
		Updates:    make([]GoalUpdateView, 0, len(updates)),
	}
	for _, m := range milestones {
		out.Milestones = append(out.Milestones, projectMilestone(m))
	}
	for _, u := range updates {
		out.Updates = append(out.Updates, projectUpdate(u))
	}
	if len(updates) > 0 {
		out.LatestUpdate = util.Ptr(projectUpdate(updates[0]))
	}
	return c.JSON(status, out)
}

func summarize(g Goal) GoalSummary {
	out := GoalSummary{
		ID: g.ID, Title: g.Title, Motivation: g.Motivation, Category: g.Category, Status: g.Status,
		TargetDate: formatDate(g.TargetDate), MilestoneTotal: g.MilestoneTotal, MilestoneDone: g.MilestoneDone,
		CreatedAt: g.CreatedAt,
	}
	if g.LatestUpdate != nil {
		out.LatestUpdate = util.Ptr(projectUpdate(*g.LatestUpdate))
	}
	return out
}

func projectUpdate(u Update) GoalUpdateView {
	return GoalUpdateView{ID: u.ID, Note: u.Note, Progress: u.Progress, CreatedAt: u.CreatedAt}
}

func projectMilestone(m Milestone) MilestoneView {
	return MilestoneView{ID: m.ID, Title: m.Title, Status: m.Status, TargetDate: formatDate(m.TargetDate), CompletedAt: m.CompletedAt}
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(dateLayout)
}

func readGoal(c *echo.Context) (Input, error) {
	var req GoalRequest
	if err := readJSON(c, &req); err != nil {
		return Input{}, err
	}
	target, err := parseDate(req.TargetDate)
	if err != nil {
		return Input{}, err
	}
	return Input{Title: req.Title, Motivation: req.Motivation, Success: req.Success, Category: req.Category, TargetDate: target}, nil
}

func readMilestone(c *echo.Context) (MilestoneInput, error) {
	var req MilestoneRequest
	if err := readJSON(c, &req); err != nil {
		return MilestoneInput{}, err
	}
	target, err := parseDate(req.TargetDate)
	if err != nil {
		return MilestoneInput{}, err
	}
	return MilestoneInput{Title: req.Title, TargetDate: target}, nil
}

// parseDate reads an optional calendar day. Unlike the web form, which drops
// a date it cannot read, the API says so: a client sending a malformed date
// has a bug worth hearing about.
func parseDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(dateLayout, raw)
	if err != nil {
		return time.Time{}, httpx.Fail(apperr.FieldErrors{}.Add("targetDate", "Use a date like 2026-12-31."), "Use a date like 2026-12-31.")
	}
	return t, nil
}

func readJSON(c *echo.Context, dst any) error {
	return echox.BindJSON(c, dst, httpx.ReadOptions{MaxBytes: 16 << 10}, "The request body could not be read.")
}
