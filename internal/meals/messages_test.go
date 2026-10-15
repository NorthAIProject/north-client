package meals

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/i18n/fieldmsg"
)

// The services write these messages in English; the table is how a handler
// finds their translation, so the two must agree word for word.
func TestFieldMessagesMatchTheCatalogue(t *testing.T) {
	t.Parallel()
	for _, m := range fieldmsg.Mismatches(fieldMessages) {
		t.Error(m)
	}
}

func fieldsIn(t *testing.T, locale string, err error) map[string]string {
	t.Helper()
	ctx := context.Background()
	if locale != "" {
		ctx = i18n.WithLocale(ctx, locale)
	}
	var fields apperr.FieldErrors
	if !apperr.As(localize(ctx, err), &fields) {
		t.Fatalf("%v: not field errors", err)
	}
	return fields.Messages()
}

func TestPlanErrorsReadTheRequestLanguage(t *testing.T) {
	t.Parallel()

	_, err := ValidateMealPlan(MealPlanInput{Settings: meal.PlanSettings{Mode: meal.Easy, Type: meal.MidCarb}, DayCount: 9})
	es := fieldsIn(t, "es", err)
	if es["name"] != "Ponle un nombre al plan." || es["day_count"] != "Elige entre 1 y 7 días." {
		t.Fatalf("es = %v", es)
	}
	// Without a locale, and so for the coach, the service's English stands.
	if en := fieldsIn(t, "", err); en["name"] != "Give the plan a name." {
		t.Fatalf("en = %v", en)
	}
}

// checkGrams carries the person's own target, so its message is matched by
// shape rather than looked up whole.
func TestGramsErrorCarriesTheTargetIntoTheTranslation(t *testing.T) {
	t.Parallel()

	err := ValidateDayOverride(meal.DayOverride{ProteinG: util.Ptr(400.0)}, Macros{ProteinG: 160, CarbG: 200, FatG: 70})
	if got := fieldsIn(t, "pt-BR", err)["protein_g"]; got != "Mantenha a proteína entre 0 e sua meta de 160 g." {
		t.Fatalf("pt-BR = %q", got)
	}
}

func TestOverageMessageInEachLanguage(t *testing.T) {
	t.Parallel()

	over := &OverageError{Verdict: meal.Verdict{Over: []meal.DayOverage{
		{Weekday: time.Monday, Status: meal.DayStatus{Over: meal.Macros{CarbG: 14, FatG: 3}}},
		{Weekday: time.Tuesday, Status: meal.DayStatus{Over: meal.Macros{ProteinG: 20}}},
	}}}
	// The coach reads Error, and it reads as it always has.
	if want := "Monday would be 14 g over on carbs and 3 g over on fat; Tuesday would be 20 g over on protein."; over.Error() != want {
		t.Fatalf("Error() = %q, want %q", over.Error(), want)
	}
	es := over.MessageIn(i18n.WithLocale(context.Background(), "es"))
	if want := "Lunes: te pasarías 14 g de carbohidratos y 3 g de grasa; martes: te pasarías 20 g de proteína."; es != want {
		t.Fatalf("es = %q, want %q", es, want)
	}
}

func TestBlankOptionLabelIsInTheAddersLanguage(t *testing.T) {
	t.Parallel()

	slot := Meal{OptionLabel: "Opción 2"}
	if got := freeOptionLabel("es", slot); got != "Opción 3" {
		t.Fatalf("es label = %q, want the first unused Spanish one", got)
	}
	if got := freeOptionLabel("en", slot); got != "Option 2" {
		t.Fatalf("en label = %q", got)
	}
}

func TestProgressAndRecommendationReadTheRequestLanguage(t *testing.T) {
	t.Parallel()

	p := Progress{
		Date:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Logged: Macros{Calories: 1800, ProteinG: 120, FatG: 60, CarbG: 180},
		Goal:   Macros{Calories: 2200}, DeltaCalories: -400,
	}
	ctx := i18n.WithLocale(context.Background(), "pt-BR")
	if got := p.SummaryIn(ctx); !strings.HasPrefix(got, "2 jan: 1800/2200 kcal registradas (400 kcal abaixo da meta)") {
		t.Fatalf("pt-BR summary = %q", got)
	}
	if got := p.SummaryIn(context.Background()); got != p.Summary() {
		t.Fatalf("English SummaryIn = %q, Summary = %q", got, p.Summary())
	}
	for _, key := range []string{"nutrition.rec.inconsistent", "nutrition.rec.cut", "nutrition.rec.bulk", "nutrition.rec.drift", "nutrition.rec.ok"} {
		if got := i18n.T(ctx, key); got == key || got == i18n.Translate("en", key) {
			t.Errorf("%s is not translated: %q", key, got)
		}
	}
}
