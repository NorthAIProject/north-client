package planimport

import (
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// API is plan import for native clients: upload a file, get a draft back,
// preview edits, commit. The same service, rules and quota as the web pages.
type API struct {
	svc    *Service
	quotas *quota.Service
}

// NewAPI builds the routes. Register goes in the JSON group; RegisterUploads
// in the upload group, whose body cap fits a file.
func NewAPI(svc *Service, quotas *quota.Service) *API { return &API{svc: svc, quotas: quotas} }

func (a *API) Register(g *echo.Group) {
	g.POST("/training/import", a.commitWorkout)
	g.POST("/nutrition/import/preview", a.previewMeal)
	g.POST("/nutrition/import", a.commitMeal)
	g.GET("/nutrition/plans/intake", a.mealIntake)
	g.POST("/nutrition/plans/generate", a.generateMeal, a.quotas.GuardAPI(quota.MealPlanGenerate))
}

func (a *API) RegisterUploads(g *echo.Group) {
	g.POST("/training/import/parse", a.parseWorkout, a.quotas.GuardAPI(quota.PlanImport))
	g.POST("/nutrition/import/parse", a.parseMeal, a.quotas.GuardAPI(quota.PlanImport))
}

// ImportedPlan answers a commit with the new plan's id; the client loads it
// through the ordinary plan endpoints, the same as a plan made by hand.
type ImportedPlan struct {
	PlanID uuid.UUID `json:"planId"`
}

// MealCommitRequest is a reviewed meal draft and the person's answer to any
// overage it has.
type MealCommitRequest struct {
	Draft          MealDraft `json:"draft"`
	ConfirmOverage bool      `json:"confirmOverage"`
}

// MealCommitRefusal is the 422 for a meal commit: the usual error shape, plus
// the draft as the server previewed it, so the client can show each reason
// beside the line or day it belongs to. A day over its target is a 409
// MacroOverage instead, as everywhere else in meal plans.
type MealCommitRefusal struct {
	Error httpx.ErrorDetail `json:"error"`
	Draft MealDraft         `json:"draft"`
}

func (a *API) parseWorkout(c *echo.Context) error {
	name, data, err := readUpload(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	draft, err := a.svc.ParseWorkout(ctx, auth.MustUser(ctx), name, data, "")
	if err != nil {
		return failWith(c, err, i18n.T(ctx, "planimport.api.import"))
	}
	return c.JSON(http.StatusOK, draft)
}

func (a *API) parseMeal(c *echo.Context) error {
	name, data, err := readUpload(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	draft, err := a.svc.ParseMeal(ctx, auth.MustUser(ctx), name, data, "")
	if err != nil {
		return failWith(c, err, i18n.T(ctx, "planimport.api.import"))
	}
	return c.JSON(http.StatusOK, draft)
}

func (a *API) commitWorkout(c *echo.Context) error {
	ctx := c.Request().Context()
	var draft WorkoutDraft
	if err := echox.BindJSON(c, &draft, httpx.ReadOptions{}, i18n.T(ctx, "planimport.api.read")); err != nil {
		return err
	}
	stored, err := a.svc.CommitWorkout(ctx, auth.MustUser(ctx), draft)
	if err != nil {
		return failWith(c, err, i18n.T(ctx, "planimport.api.save"))
	}
	return c.JSON(http.StatusCreated, ImportedPlan{PlanID: stored.ID})
}

func (a *API) previewMeal(c *echo.Context) error {
	ctx := c.Request().Context()
	var draft MealDraft
	if err := echox.BindJSON(c, &draft, httpx.ReadOptions{}, i18n.T(ctx, "planimport.api.read")); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, a.svc.PreviewMeal(ctx, auth.MustUser(ctx).ID, draft))
}

func (a *API) commitMeal(c *echo.Context) error {
	ctx := c.Request().Context()
	var req MealCommitRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{}, i18n.T(ctx, "planimport.api.read")); err != nil {
		return err
	}

	saved, previewed, err := a.svc.CommitMeal(ctx, auth.MustUser(ctx), req.Draft, req.ConfirmOverage)
	if err == nil {
		return c.JSON(http.StatusCreated, ImportedPlan{PlanID: saved.ID})
	}

	var over *meals.OverageError
	var fields apperr.FieldErrors
	switch {
	case apperr.As(err, &over):
		// The same 409 every other change to a meal plan gets; canConfirm says
		// whether asking again with confirmOverage will save it.
		return c.JSON(http.StatusConflict, meals.ProjectOverage(ctx, over))
	case apperr.As(meals.Localize(ctx, err), &fields):
		return c.JSON(http.StatusUnprocessableEntity, MealCommitRefusal{
			Error: httpx.ErrorDetail{Message: i18n.T(ctx, "planimport.api.attention"), Fields: fields.Messages()},
			Draft: previewed,
		})
	default:
		return httpx.Fail(err, i18n.T(ctx, "planimport.api.save"))
	}
}

// readUpload reads the one file an import takes. The body is capped here as
// well as by the route group, whose cap is sized for video.
func readUpload(c *echo.Context) (string, []byte, error) {
	r := c.Request()
	r.Body = http.MaxBytesReader(c.Response(), r.Body, MaxBytes+(1<<20))
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		msg := i18n.T(r.Context(), "planimport.err.upload")
		var tooBig *http.MaxBytesError
		if apperr.As(err, &tooBig) {
			msg = i18n.T(r.Context(), "planimport.err.toolarge")
		}
		return "", nil, httpx.Fail(apperr.FieldErrors{}.Add("file", msg), msg)
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		msg := i18n.T(r.Context(), "planimport.err.nofile")
		return "", nil, httpx.Fail(apperr.FieldErrors{}.Add("file", msg), msg)
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		msg := i18n.T(r.Context(), "planimport.err.upload")
		return "", nil, httpx.Fail(apperr.FieldErrors{}.Add("file", msg), msg)
	}
	return header.Filename, data, nil
}

// failWith answers with the refusal's own sentence when there is one: the
// person needs "this PDF is password-protected", not "could not be imported".
func failWith(c *echo.Context, err error, fallback string) error {
	var fe *FileError
	if apperr.As(err, &fe) {
		return httpx.Fail(err, fe.Message)
	}
	var fields apperr.FieldErrors
	if localized := meals.Localize(c.Request().Context(), err); apperr.As(localized, &fields) && len(fields) > 0 {
		return httpx.Fail(localized, fields[0].Message)
	}
	return httpx.Fail(err, fallback)
}
