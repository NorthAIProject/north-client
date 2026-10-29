package calculator

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator/macroplan"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the web /calculator page for native clients: body measurements and
// the macro goal generated from them. Everything is metric; a client converts
// at its own edge, as the web form does.
type API struct {
	svc        *Service
	biometrics *biometrics.Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service, bio *biometrics.Service) *API { return &API{svc: svc, biometrics: bio} }

func (a *API) Register(g *echo.Group) {
	g.GET("/calculator", a.show)
	g.PUT("/calculator/biometrics", a.recordBiometrics)
	g.POST("/calculator/plan", a.generate)
}

type BiometricsView struct {
	WeightKg float64 `json:"weightKg"`
	HeightCm float64 `json:"heightCm"`
	// DateOfBirth is a calendar day.
	DateOfBirth string `json:"dateOfBirth"`
	Sex         string `json:"sex"`
	// AgeYears is the age the measurements are worked out at, today.
	AgeYears int `json:"ageYears"`
}

// GoalCalories is what one objective would cost at the goal's TDEE.
type GoalCalories struct {
	Goal        string  `json:"goal"`
	CalorieGoal float64 `json:"calorieGoal"`
}

type MacroGoalView struct {
	ActivityLevel string    `json:"activityLevel"`
	Goal          string    `json:"goal"`
	MacroSplit    string    `json:"macroSplit"`
	BMR           float64   `json:"bmr"`
	TDEE          float64   `json:"tdee"`
	CalorieGoal   float64   `json:"calorieGoal"`
	ProteinG      float64   `json:"proteinG"`
	FatG          float64   `json:"fatG"`
	CarbG         float64   `json:"carbG"`
	CreatedAt     time.Time `json:"createdAt"`
	// Comparison is every objective's daily calories at this TDEE, in the
	// order Options.Goals lists them.
	Comparison []GoalCalories `json:"comparison"`
}

type CalculatorOptions struct {
	ActivityLevels []string `json:"activityLevels"`
	Goals          []string `json:"goals"`
	MacroSplits    []string `json:"macroSplits"`
}

type CalculatorView struct {
	// Biometrics is absent until measurements are recorded; workout calories
	// and the macro goal both need them.
	Biometrics *BiometricsView   `json:"biometrics,omitempty"`
	Goal       *MacroGoalView    `json:"goal,omitempty"`
	Options    CalculatorOptions `json:"options"`
}

type BiometricsRequest struct {
	WeightKg    float64 `json:"weightKg"`
	HeightCm    float64 `json:"heightCm"`
	DateOfBirth string  `json:"dateOfBirth"`
	Sex         string  `json:"sex"`
}

type GoalRequest struct {
	ActivityLevel string `json:"activityLevel"`
	Goal          string `json:"goal"`
	MacroSplit    string `json:"macroSplit"`
}

func (a *API) show(c *echo.Context) error {
	return a.respond(c, http.StatusOK)
}

func (a *API) recordBiometrics(c *echo.Context) error {
	var req BiometricsRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	dob, err := time.Parse("2006-01-02", req.DateOfBirth)
	if err != nil {
		return httpx.Fail(apperr.FieldErrors{}.Add("dateOfBirth", "Use a date like 1990-05-01."), "Use a date like 1990-05-01.")
	}
	ctx := c.Request().Context()
	if _, err := a.biometrics.Record(ctx, auth.MustUser(ctx).ID, biometrics.Input{
		WeightKg: req.WeightKg, HeightCm: req.HeightCm, DateOfBirth: dob, Sex: req.Sex,
	}); err != nil {
		return httpx.Fail(err, "The measurements could not be saved.")
	}
	return a.respond(c, http.StatusOK)
}

func (a *API) generate(c *echo.Context) error {
	var req GoalRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := a.svc.Generate(ctx, auth.MustUser(ctx).ID, Input(req)); err != nil {
		return httpx.Fail(err, "The goal could not be worked out.")
	}
	return a.respond(c, http.StatusCreated)
}

func (a *API) respond(c *echo.Context, status int) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	out := CalculatorView{Options: CalculatorOptions{
		ActivityLevels: macroplan.ActivityLevels, Goals: macroplan.Goals, MacroSplits: macroplan.Splits,
	}}
	bio, err := a.biometrics.Current(ctx, userID)
	switch {
	case err == nil:
		out.Biometrics = &BiometricsView{
			WeightKg: bio.WeightKg, HeightCm: bio.HeightCm,
			DateOfBirth: bio.DateOfBirth.Format("2006-01-02"), Sex: bio.Sex,
			AgeYears: bio.AgeYears(time.Now()),
		}
	case !apperr.Is(err, apperr.ErrNotFound):
		return httpx.Fail(err, "The calculator could not be loaded.")
	}
	plan, err := a.svc.Current(ctx, userID)
	switch {
	case err == nil:
		out.Goal = &MacroGoalView{
			ActivityLevel: plan.ActivityLevel, Goal: plan.Goal, MacroSplit: plan.MacroSplit,
			BMR: plan.BMR, TDEE: plan.TDEE, CalorieGoal: plan.CalorieGoal, ProteinG: plan.ProteinG, FatG: plan.FatG,
			CarbG: plan.CarbG, CreatedAt: plan.CreatedAt,
			Comparison: make([]GoalCalories, 0, len(macroplan.Goals)),
		}
		all := macroplan.AllGoals(plan.TDEE)
		for _, g := range macroplan.Goals {
			out.Goal.Comparison = append(out.Goal.Comparison, GoalCalories{Goal: g, CalorieGoal: all[g]})
		}
	case !apperr.Is(err, apperr.ErrNotFound):
		return httpx.Fail(err, "The calculator could not be loaded.")
	}
	return c.JSON(status, out)
}

func readJSON(c *echo.Context, dst any) error {
	return echox.BindJSON(c, dst, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read.")
}
