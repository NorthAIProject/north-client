package calculator

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator/macroplan"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// BiometricsLookup is the calculator's view of the biometrics package: just
// enough to read the current measurement, so this package need not depend on
// biometrics.Service's concrete type.
type BiometricsLookup interface {
	Current(ctx context.Context, userID uuid.UUID) (biometrics.Biometric, error)
}

type Service struct {
	repo       *Repository
	biometrics BiometricsLookup
}

func NewService(repo *Repository, lookup BiometricsLookup) *Service {
	return &Service{repo: repo, biometrics: lookup}
}

// Input is the choices a person makes when generating a plan; the biometric
// numbers themselves come from BiometricsLookup, not from the caller.
type Input struct {
	ActivityLevel string
	Goal          string
	MacroSplit    string
}

// Validate checks the choices before a plan is generated. Empty fields
// default to the middle-of-the-road option rather than failing, since a
// first-time user has no reason to know these enums yet.
func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	in.ActivityLevel = strings.TrimSpace(in.ActivityLevel)
	switch {
	case in.ActivityLevel == "":
		in.ActivityLevel = ActivityModerate
	case !slices.Contains(ActivityLevels, in.ActivityLevel):
		errs = errs.Add("activity_level", "Choose one of the listed activity levels.")
	}

	in.Goal = strings.TrimSpace(in.Goal)
	switch {
	case in.Goal == "":
		in.Goal = GoalMaintenance
	case !slices.Contains(Goals, in.Goal):
		errs = errs.Add("goal", "Choose one of the listed goals.")
	}

	in.MacroSplit = strings.TrimSpace(in.MacroSplit)
	switch {
	case in.MacroSplit == "":
		in.MacroSplit = SplitModerateCarb
	case !slices.Contains(Splits, in.MacroSplit):
		errs = errs.Add("macro_split", "Choose one of the listed macro splits.")
	}

	return in, errs.OrNil()
}

// Generate loads the user's current biometrics, computes a new plan, and
// persists it as the current one.
func (s *Service) Generate(ctx context.Context, userID uuid.UUID, in Input) (MacroPlan, error) {
	clean, err := Validate(in)
	if err != nil {
		return MacroPlan{}, err
	}

	bio, err := s.biometrics.Current(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return MacroPlan{}, apperr.Wrap(apperr.ErrValidation, "record your biometrics before generating a plan")
		}
		return MacroPlan{}, err
	}

	age := bio.AgeYears(time.Now())
	plan := macroplan.Generate(bio.WeightKg, bio.HeightCm, age, bio.Sex, clean.ActivityLevel, clean.Goal, clean.MacroSplit)

	return s.repo.RecordCurrent(ctx, userID, plan)
}

// Current returns the most recent plan. apperr.ErrNotFound until the user has
// generated one.
func (s *Service) Current(ctx context.Context, userID uuid.UUID) (MacroPlan, error) {
	return s.repo.Current(ctx, userID)
}

func (s *Service) History(ctx context.Context, userID uuid.UUID, limit int) ([]MacroPlan, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.History(ctx, userID, limit)
}
