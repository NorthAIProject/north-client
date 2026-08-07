package calculator_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/calculator/macroplan"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// fakeBiometrics lets service tests control what "current biometrics" returns
// without needing a real biometrics.Service in every test.
type fakeBiometrics struct {
	bio biometrics.Biometric
	err error
}

func (f fakeBiometrics) Current(context.Context, uuid.UUID) (biometrics.Biometric, error) {
	return f.bio, f.err
}

func newService(t *testing.T, lookup calculator.BiometricsLookup) (*calculator.Service, users.User) {
	t.Helper()

	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))

	user, err := userSvc.Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	return calculator.NewService(calculator.NewRepository(pool), lookup), user
}

func TestBMRMifflinStJeor(t *testing.T) {
	t.Parallel()

	// Hand-computed against the published Mifflin-St Jeor formula, not
	// against any ported implementation:
	// male:   10*80 + 6.25*180 - 5*30 + 5   = 800 + 1125 - 150 + 5   = 1780
	// female: 10*80 + 6.25*180 - 5*30 - 161 = 800 + 1125 - 150 - 161 = 1614
	if got, want := macroplan.BMR(80, 180, 30, macroplan.SexMale), 1780.0; got != want {
		t.Fatalf("male BMR = %v, want %v", got, want)
	}
	if got, want := macroplan.BMR(80, 180, 30, macroplan.SexFemale), 1614.0; got != want {
		t.Fatalf("female BMR = %v, want %v", got, want)
	}
}

func TestTDEEAppliesActivityMultiplier(t *testing.T) {
	t.Parallel()

	if got, want := macroplan.TDEE(2000, macroplan.ActivitySedentary), 2400.0; got != want {
		t.Fatalf("sedentary TDEE = %v, want %v", got, want)
	}
	if got, want := macroplan.TDEE(2000, macroplan.ActivityExtraHeavy), 3800.0; got != want {
		t.Fatalf("extra-heavy TDEE = %v, want %v", got, want)
	}
}

func TestCalorieGoalAppliesFlatOffset(t *testing.T) {
	t.Parallel()

	if got, want := macroplan.CalorieGoalFor(2500, macroplan.GoalCutting), 2050.0; got != want {
		t.Fatalf("cutting goal = %v, want %v", got, want)
	}
	if got, want := macroplan.CalorieGoalFor(2500, macroplan.GoalBulking), 2850.0; got != want {
		t.Fatalf("bulking goal = %v, want %v", got, want)
	}
	if got, want := macroplan.CalorieGoalFor(2500, macroplan.GoalMaintenance), 2500.0; got != want {
		t.Fatalf("maintenance goal = %v, want %v", got, want)
	}
}

func TestMacrosSplitByPreset(t *testing.T) {
	t.Parallel()

	// High-carb: 30% protein, 20% fat, 50% carb of 2000 kcal.
	protein, fat, carb := macroplan.Macros(2000, macroplan.SplitHighCarb)
	if protein != 150 { // 0.3*2000/4
		t.Fatalf("protein = %v, want 150", protein)
	}
	if !within(fat, 44.44, 0.01) { // 0.2*2000/9
		t.Fatalf("fat = %v, want ~44.44", fat)
	}
	if carb != 250 { // 0.5*2000/4
		t.Fatalf("carb = %v, want 250", carb)
	}
}

func within(got, want, tolerance float64) bool {
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	return diff < tolerance
}

func TestValidationDefaultsEmptyFields(t *testing.T) {
	t.Parallel()

	clean, err := calculator.Validate(calculator.Input{})
	if err != nil {
		t.Fatalf("empty input should default rather than fail: %v", err)
	}
	if clean.ActivityLevel != calculator.ActivityModerate {
		t.Fatalf("activity_level = %q, want moderate default", clean.ActivityLevel)
	}
	if clean.Goal != calculator.GoalMaintenance {
		t.Fatalf("goal = %q, want maintenance default", clean.Goal)
	}
	if clean.MacroSplit != calculator.SplitModerateCarb {
		t.Fatalf("macro_split = %q, want moderate_carb default", clean.MacroSplit)
	}
}

func TestValidationRejectsUnknownEnums(t *testing.T) {
	t.Parallel()

	if _, err := calculator.Validate(calculator.Input{ActivityLevel: "supersonic"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestGenerateRequiresBiometrics(t *testing.T) {
	svc, user := newService(t, fakeBiometrics{err: apperr.ErrNotFound})

	if _, err := svc.Generate(context.Background(), user.ID, calculator.Input{}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected a friendly validation error, got %v", err)
	}
}

func TestGenerateComputesAndPersistsAsCurrent(t *testing.T) {
	lookup := fakeBiometrics{bio: biometrics.Biometric{
		WeightKg:    80,
		HeightCm:    180,
		DateOfBirth: fixedBirthdate(30),
		Sex:         biometrics.SexMale,
	}}
	svc, user := newService(t, lookup)
	ctx := context.Background()

	plan, err := svc.Generate(ctx, user.ID, calculator.Input{
		ActivityLevel: calculator.ActivitySedentary,
		Goal:          calculator.GoalCutting,
		MacroSplit:    calculator.SplitHighCarb,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !plan.IsCurrent {
		t.Fatal("a newly generated plan should be current")
	}
	if plan.BMR != 1780 {
		t.Fatalf("BMR = %v, want 1780", plan.BMR)
	}

	current, err := svc.Current(ctx, user.ID)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.CalorieGoal != plan.CalorieGoal {
		t.Fatalf("current calorie goal = %v, want %v", current.CalorieGoal, plan.CalorieGoal)
	}
}

func TestGenerateRetiresThePreviousPlan(t *testing.T) {
	lookup := fakeBiometrics{bio: biometrics.Biometric{
		WeightKg:    80,
		HeightCm:    180,
		DateOfBirth: fixedBirthdate(30),
		Sex:         biometrics.SexMale,
	}}
	svc, user := newService(t, lookup)
	ctx := context.Background()

	if _, err := svc.Generate(ctx, user.ID, calculator.Input{Goal: calculator.GoalMaintenance}); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if _, err := svc.Generate(ctx, user.ID, calculator.Input{Goal: calculator.GoalBulking}); err != nil {
		t.Fatalf("second generate: %v", err)
	}

	history, err := svc.History(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected both plans in history, got %d", len(history))
	}

	current, err := svc.Current(ctx, user.ID)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.Goal != calculator.GoalBulking {
		t.Fatalf("current goal = %q, want the second generation's goal", current.Goal)
	}
}

// fixedBirthdate returns a date of birth years ago from now, so a test's
// expected age holds regardless of what day it happens to run.
func fixedBirthdate(years int) time.Time {
	return time.Now().AddDate(-years, 0, 0)
}
