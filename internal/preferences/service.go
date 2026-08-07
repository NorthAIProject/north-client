package preferences

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/calculator"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// Input is preferences as submitted. Empty fields default to the same
// middle-of-the-road values as the column defaults, so a first-time user
// does not have to know these enums yet.
type Input struct {
	UnitsSystem       string
	DefaultGoal       string
	DefaultMacroSplit string
}

func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	in.UnitsSystem = strings.TrimSpace(in.UnitsSystem)
	switch {
	case in.UnitsSystem == "":
		in.UnitsSystem = UnitsMetric
	case !slices.Contains(UnitsSystems, in.UnitsSystem):
		errs = errs.Add("units_system", "Choose one of the listed units.")
	}

	in.DefaultGoal = strings.TrimSpace(in.DefaultGoal)
	switch {
	case in.DefaultGoal == "":
		in.DefaultGoal = calculator.GoalMaintenance
	case !slices.Contains(calculator.Goals, in.DefaultGoal):
		errs = errs.Add("default_goal", "Choose one of the listed goals.")
	}

	in.DefaultMacroSplit = strings.TrimSpace(in.DefaultMacroSplit)
	switch {
	case in.DefaultMacroSplit == "":
		in.DefaultMacroSplit = calculator.SplitModerateCarb
	case !slices.Contains(calculator.Splits, in.DefaultMacroSplit):
		errs = errs.Add("default_macro_split", "Choose one of the listed macro splits.")
	}

	return in, errs.OrNil()
}

// defaults is what a brand-new account is treated as having, before they
// have ever saved a preference.
func defaults() Preferences {
	return Preferences{
		UnitsSystem: UnitsMetric, DefaultGoal: calculator.GoalMaintenance, DefaultMacroSplit: calculator.SplitModerateCarb,
	}
}

// Get returns the user's saved preferences, or sane defaults if they have
// never saved any — never apperr.ErrNotFound, since a missing row is not a
// failure here, just an unconfigured account.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (Preferences, error) {
	p, err := s.repo.Get(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			d := defaults()
			d.UserID = userID
			return d, nil
		}
		return Preferences{}, err
	}
	return p, nil
}

// UnitsSystem returns just the units the person works in.
//
// Narrower than Get because that is all its caller needs, and because the
// caller is internal/calculator: this package imports calculator to validate
// the stored defaults against its enums, so calculator cannot import this one
// back. A method returning a plain string is something calculator can depend
// on through an interface of its own without a cycle.
func (s *Service) UnitsSystem(ctx context.Context, userID uuid.UUID) (string, error) {
	p, err := s.Get(ctx, userID)
	if err != nil {
		return "", err
	}
	return p.UnitsSystem, nil
}

func (s *Service) Upsert(ctx context.Context, userID uuid.UUID, in Input) (Preferences, error) {
	clean, err := Validate(in)
	if err != nil {
		return Preferences{}, err
	}
	return s.repo.Upsert(ctx, userID, clean)
}
