package exercises

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The catalog's vocabulary (muscles, type, level, equipment) is translated;
// the exercise's own name is not, because the catalog only has it in English.
func TestDetailNamesTheCatalogVocabularyInTheChosenLanguage(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), string(users.LocaleES))
	v := DetailView{
		Exercise: illustrated(),
		PlanID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Days:     []PlanDayOption{{Index: 0, Weekday: "Monday", Focus: "Upper", Includes: true}, {Index: 1, Weekday: "Thursday"}},
	}
	var b strings.Builder
	if err := Detail(users.User{DisplayName: "Ana", Locale: users.LocaleES}, v).Render(ctx, &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := b.String()
	for _, want := range []string{"Pecho", "Tríceps", "Fuerza", "Intermedio", "Barra", "Principales", "Ya está en tu plan: Lunes.", "+ Jueves", "Barbell Bench Press - Medium Grip"} {
		if !strings.Contains(html, want) {
			t.Errorf("detail does not contain %q", want)
		}
	}
	// Not "Primary": the shared muscle viewer prints its own legend, which is
	// web/shared's to translate.
	for _, leak := range []string{">Chest<", "Strength", "Intermediate", "Already in your plan", "Thursday"} {
		if strings.Contains(html, leak) {
			t.Errorf("English %q is still on the detail page", leak)
		}
	}
}

// A value the catalogue has no entry for — the filter echoes the URL — reads
// humanized rather than as a raw key.
func TestUnknownVocabularyFallsBackToHumanize(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), string(users.LocalePTBR))
	if got := equipmentLabel(ctx, "medicine ball"); got != "Medicine ball" {
		t.Errorf("equipmentLabel(unknown) = %q", got)
	}
	if got := equipmentLabel(ctx, "pull-up bar"); got != "Barra fixa" {
		t.Errorf("equipmentLabel(pull-up bar) = %q", got)
	}
}
