package workouts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderIn(t *testing.T, locale users.Locale, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(i18n.WithLocale(context.Background(), string(locale)), &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered nothing", locale)
	}
	return b.String()
}

func translatedPlanPage(t *testing.T, locale users.Locale) string {
	t.Helper()
	return renderIn(t, locale, PlanPage(users.User{DisplayName: "Ana", Locale: locale}, PlanView{
		ID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Plan:      planWith(gobletSquat()),
		CreatedAt: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
		Completed: map[int]bool{0: true},
	}))
}

// The plan page is read mid-session; a Spanish reader should not meet an
// English heading, button or weekday on it.
func TestPlanPageRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Why this plan", "Add exercise", "View muscles", "Monday", "Completed", "built 27 August 2026"},
		users.LocaleES:   {"Por qué este plan", "Añadir ejercicio", "Ver músculos", "Lunes", "Hecho", "creado el 27 de agosto de 2026"},
		users.LocalePTPT: {"Porquê este plano", "Adicionar exercício", "Ver músculos", "Segunda-feira", "Feito", "criado a 27 de agosto de 2026"},
		users.LocalePTBR: {"Por que este plano", "Adicionar exercício", "Ver músculos", "Segunda-feira", "Feito", "criado em 27 de agosto de 2026"},
	}
	for locale, wants := range cases {
		html := translatedPlanPage(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: plan page does not contain %q", locale, want)
			}
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedPlanPage(t *testing.T) {
	englishOnly := []string{"Why this plan", "Add exercise", "View muscles", ">Monday<", "Completed", "All plans", "Set a start time", "No equipment?"}
	for _, locale := range []users.Locale{users.LocaleES, users.LocalePTPT, users.LocalePTBR} {
		html := translatedPlanPage(t, locale)
		for _, leak := range englishOnly {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the plan page", locale, leak)
			}
		}
	}
}

// The week card says this week or next as one phrase, so both have to come
// from the catalogue rather than an English fragment glued to a translation.
func TestWeekCardIsTranslated(t *testing.T) {
	week := WeekView{
		NextWeek: true,
		Start:    time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Days: []WeekDayView{{
			Weekday: "Monday", Date: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), Focus: "Lower body",
		}},
	}
	html := renderIn(t, users.LocaleES, WeekCard(week))
	for _, want := range []string{"La semana que viene", "Cambiar la semana que viene", "1 días de entreno desde el lun 12 oct"} {
		if !strings.Contains(html, want) {
			t.Errorf("week card does not contain %q", want)
		}
	}
	for _, leak := range []string{"Next week", "Change next week", "training days from"} {
		if strings.Contains(html, leak) {
			t.Errorf("English %q is still on the week card", leak)
		}
	}
}

func TestIntakeOptionsAreTranslated(t *testing.T) {
	html := renderIn(t, users.LocalePTPT, IntakePage(users.User{DisplayName: "Ana", Locale: users.LocalePTPT}, IntakeForm{}))
	for _, want := range []string{"Cria o teu plano de treino", "Halteres", "Passadeira", "Criar o meu plano"} {
		if !strings.Contains(html, want) {
			t.Errorf("intake does not contain %q", want)
		}
	}
	// The posted values stay the validator's English vocabulary.
	if !strings.Contains(html, `value="dumbbell"`) {
		t.Error("the equipment value is no longer the validator's vocabulary")
	}
	for _, leak := range []string{"Build your training plan", "Dumbbells", "Treadmill"} {
		if strings.Contains(html, leak) {
			t.Errorf("English %q is still on the intake", leak)
		}
	}
}
