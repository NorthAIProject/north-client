package activity

import (
	"context"
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
)

// API is the activity timer for native clients: the web /activity page's
// start, pause, resume, stop, cancel and manual log. A phone workout runs one
// of these sessions, so the time and calories land where the web and the
// coach already read them.
type API struct {
	svc *Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/activity", a.overview)
	g.POST("/activity/start", a.start)
	g.POST("/activity/log", a.log)
	g.POST("/activity/:sessionID/pause", a.pause)
	g.POST("/activity/:sessionID/resume", a.resume)
	g.POST("/activity/:sessionID/stop", a.stop)
	g.DELETE("/activity/:sessionID", a.cancel)
}

type SessionView struct {
	ID           uuid.UUID `json:"id"`
	ActivityCode string    `json:"activityCode"`
	ActivityName string    `json:"activityName"`
	// Source is manual or strava.
	Source string `json:"source"`
	// Status is active, paused, completed or cancelled.
	Status             string     `json:"status"`
	StartedAt          time.Time  `json:"startedAt"`
	PausedAt           *time.Time `json:"pausedAt,omitempty"`
	TotalPausedSeconds int        `json:"totalPausedSeconds"`
	EndedAt            *time.Time `json:"endedAt,omitempty"`
	// ElapsedSeconds is moving time as of this response; a client ticks it
	// forward from StartedAt and the paused total rather than polling.
	ElapsedSeconds int      `json:"elapsedSeconds"`
	CaloriesBurned *float64 `json:"caloriesBurned,omitempty"`
	DistanceM      *float64 `json:"distanceM,omitempty"`
}

type ActivityKind struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

type Overview struct {
	Active *SessionView   `json:"active,omitempty"`
	Recent []SessionView  `json:"recent"`
	Kinds  []ActivityKind `json:"kinds"`
	// TodayCalories is what completed sessions burned since local midnight,
	// the line the web page leads with.
	TodayCalories float64 `json:"todayCalories"`
}

type StartRequest struct {
	ActivityCode string `json:"activityCode"`
	// PlanWeekday is the plan day a guided workout was opened for. Finishing
	// the session marks that day completed for the week.
	PlanWeekday string `json:"planWeekday,omitempty"`
}

type LogRequest struct {
	ActivityCode string `json:"activityCode"`
	// StartedAt defaults to now minus the duration.
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	DurationMinutes int        `json:"durationMinutes"`
	DistanceKm      *float64   `json:"distanceKm,omitempty"`
}

func (a *API) overview(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	userID := user.ID
	now := time.Now()

	out := Overview{Recent: []SessionView{}, Kinds: make([]ActivityKind, 0, len(METTable))}
	if active, ok, err := a.svc.Active(ctx, userID); err != nil {
		return httpx.Fail(err, "Activity could not be loaded.")
	} else if ok {
		out.Active = util.Ptr(project(active, now))
	}
	recent, err := a.svc.List(ctx, userID, 20)
	if err != nil {
		return httpx.Fail(err, "Activity could not be loaded.")
	}
	for _, s := range recent {
		out.Recent = append(out.Recent, project(s, now))
	}
	for _, m := range METTable {
		out.Kinds = append(out.Kinds, ActivityKind{Code: m.Code, Name: m.Name, Category: m.Category})
	}
	local := now.In(user.Location())
	since := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, user.Location())
	if out.TodayCalories, err = a.svc.TotalCaloriesSince(ctx, userID, since); err != nil {
		return httpx.Fail(err, "Activity could not be loaded.")
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) start(c *echo.Context) error {
	var req StartRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	session, err := a.svc.Start(ctx, auth.MustUser(ctx).ID, req.ActivityCode, req.PlanWeekday)
	return a.respond(c, http.StatusCreated, session, err)
}

func (a *API) log(c *echo.Context) error {
	var req LogRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	if req.DurationMinutes <= 0 {
		return httpx.Fail(apperr.FieldErrors{}.Add("durationMinutes", "Say how many minutes the session lasted."), "Say how many minutes the session lasted.")
	}
	in := LogInput{ActivityCode: req.ActivityCode, Duration: time.Duration(req.DurationMinutes) * time.Minute}
	in.StartedAt = util.Val(req.StartedAt)
	if req.DistanceKm != nil {
		in.DistanceM = *req.DistanceKm * 1000
	}
	ctx := c.Request().Context()
	session, err := a.svc.Log(ctx, auth.MustUser(ctx).ID, in)
	return a.respond(c, http.StatusCreated, session, err)
}

func (a *API) pause(c *echo.Context) error {
	return a.transition(c, a.svc.Pause)
}

func (a *API) resume(c *echo.Context) error {
	return a.transition(c, a.svc.Resume)
}

func (a *API) stop(c *echo.Context) error {
	return a.transition(c, a.svc.Stop)
}

func (a *API) cancel(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "sessionID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.Cancel(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return a.respond(c, 0, Session{}, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) transition(c *echo.Context, step func(ctx context.Context, id, userID uuid.UUID) (Session, error)) error {
	id, err := echox.UUIDParam(c, "sessionID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	session, err := step(ctx, id, auth.MustUser(ctx).ID)
	return a.respond(c, http.StatusOK, session, err)
}

// respond writes the session, or the service's own reason for refusing:
// "record your biometrics before tracking activity" is worth showing as is.
func (a *API) respond(c *echo.Context, status int, session Session, err error) error {
	switch {
	case err == nil:
		return c.JSON(status, project(session, time.Now()))
	case apperr.Is(err, apperr.ErrValidation):
		message := strings.TrimSuffix(err.Error(), ": "+apperr.ErrValidation.Error())
		return httpx.Fail(apperr.FieldErrors{}.Add("activity", message), message)
	case apperr.Is(err, apperr.ErrConflict):
		return httpx.Fail(err, strings.TrimSuffix(err.Error(), ": "+apperr.ErrConflict.Error()))
	default:
		return httpx.Fail(err, "That session could not be updated.")
	}
}

func project(s Session, now time.Time) SessionView {
	name := s.ActivityCode
	if met, ok := LookupMET(s.ActivityCode); ok {
		name = met.Name
	}
	return SessionView{
		ID: s.ID, ActivityCode: s.ActivityCode, ActivityName: name, Source: s.Source, Status: s.Status,
		StartedAt: s.StartedAt, PausedAt: s.PausedAt, TotalPausedSeconds: s.TotalPausedSeconds, EndedAt: s.EndedAt,
		ElapsedSeconds: int(s.Elapsed(now).Seconds()), CaloriesBurned: s.CaloriesBurned, DistanceM: s.DistanceM,
	}
}

func readJSON(c *echo.Context, dst any) error {
	return echox.BindJSON(c, dst, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read.")
}
