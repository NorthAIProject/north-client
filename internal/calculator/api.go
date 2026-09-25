package calculator

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator/macroplan"
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

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service, bio *biometrics.Service) *API { return &API{svc: svc, biometrics: bio} }

func (a *API) Routes(r chi.Router) {
	r.Get("/calculator", a.show)
	r.Put("/calculator/biometrics", a.recordBiometrics)
	r.Post("/calculator/plan", a.generate)
}

type BiometricsView struct {
	WeightKg float64 `json:"weightKg"`
	HeightCm float64 `json:"heightCm"`
	// DateOfBirth is a calendar day.
	DateOfBirth string `json:"dateOfBirth"`
	Sex         string `json:"sex"`
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

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	a.respond(w, r, http.StatusOK)
}

func (a *API) recordBiometrics(w http.ResponseWriter, r *http.Request) {
	var req BiometricsRequest
	if !readJSON(w, r, &req) {
		return
	}
	dob, err := time.Parse("2006-01-02", req.DateOfBirth)
	if err != nil {
		httpx.Error(w, apperr.FieldErrors{}.Add("dateOfBirth", "Use a date like 1990-05-01."), "Use a date like 1990-05-01.")
		return
	}
	if _, err := a.biometrics.Record(r.Context(), auth.MustUser(r.Context()).ID, biometrics.Input{
		WeightKg: req.WeightKg, HeightCm: req.HeightCm, DateOfBirth: dob, Sex: req.Sex,
	}); err != nil {
		httpx.Error(w, err, "The measurements could not be saved.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) generate(w http.ResponseWriter, r *http.Request) {
	var req GoalRequest
	if !readJSON(w, r, &req) {
		return
	}
	if _, err := a.svc.Generate(r.Context(), auth.MustUser(r.Context()).ID, Input(req)); err != nil {
		httpx.Error(w, err, "The goal could not be worked out.")
		return
	}
	a.respond(w, r, http.StatusCreated)
}

func (a *API) respond(w http.ResponseWriter, r *http.Request, status int) {
	userID := auth.MustUser(r.Context()).ID
	out := CalculatorView{Options: CalculatorOptions{
		ActivityLevels: macroplan.ActivityLevels, Goals: macroplan.Goals, MacroSplits: macroplan.Splits,
	}}
	bio, err := a.biometrics.Current(r.Context(), userID)
	switch {
	case err == nil:
		out.Biometrics = &BiometricsView{
			WeightKg: bio.WeightKg, HeightCm: bio.HeightCm,
			DateOfBirth: bio.DateOfBirth.Format("2006-01-02"), Sex: bio.Sex,
		}
	case !apperr.Is(err, apperr.ErrNotFound):
		httpx.Error(w, err, "The calculator could not be loaded.")
		return
	}
	plan, err := a.svc.Current(r.Context(), userID)
	switch {
	case err == nil:
		out.Goal = &MacroGoalView{
			ActivityLevel: plan.ActivityLevel, Goal: plan.Goal, MacroSplit: plan.MacroSplit,
			BMR: plan.BMR, TDEE: plan.TDEE, CalorieGoal: plan.CalorieGoal, ProteinG: plan.ProteinG, FatG: plan.FatG,
			CarbG: plan.CarbG, CreatedAt: plan.CreatedAt,
		}
	case !apperr.Is(err, apperr.ErrNotFound):
		httpx.Error(w, err, "The calculator could not be loaded.")
		return
	}
	httpx.WriteJSON(w, status, out)
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
