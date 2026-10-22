package onboarding

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderFormIn(t *testing.T, locale string) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), locale)
	if err := FormPage(Form{}).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	return b.String()
}

// Onboarding is the first page after sign-up: somebody who signed up in
// Portuguese should not be asked their goals in English.
func TestOnboardingRendersInTheChosenLanguage(t *testing.T) {
	cases := map[string][]string{
		"en":    {"Tell Khepri where to start", "Supportive", "Skip for now", "Health"},
		"es":    {"Dile a Khepri por dónde empezar", "Cercano", "Saltar por ahora", "Salud"},
		"pt-PT": {"Diz ao Khepri por onde começar", "Próximo", "Saltar por agora", "Saúde"},
		"pt-BR": {"Diga ao Khepri por onde começar", "Acolhedor", "Pular por enquanto", "Saúde"},
	}
	for locale, wants := range cases {
		html := renderFormIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedOnboarding(t *testing.T) {
	englishOnly := []string{"Tell Khepri where to start", "Pick one or more.", "Skip pep talks", "Skip for now", ">Learning<"}
	for _, locale := range []string{"es", "pt-PT", "pt-BR"} {
		html := renderFormIn(t, locale)
		for _, leak := range englishOnly {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the page", locale, leak)
			}
		}
	}
}

func TestDonePageGreetsByNameInTheChosenLanguage(t *testing.T) {
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), "pt-PT")
	if err := DonePage(users.User{DisplayName: "Ana Silva"}, "", false).Render(ctx, &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := b.String()
	if !strings.Contains(html, "O Khepri já sabe por onde começar, Ana") {
		t.Errorf("greeting not translated:\n%s", html)
	}
	if strings.Contains(html, "Go to dashboard") {
		t.Error("English button on the Portuguese page")
	}
}

// The pick only shows when the server starts first weeks; with the switch
// off the form is exactly what it was.
func TestTheFirstWeekPickShowsOnlyWhenWeeksStart(t *testing.T) {
	var off, on strings.Builder
	ctx := i18n.WithLocale(context.Background(), "es")
	if err := FormPage(Form{}).Render(ctx, &off); err != nil {
		t.Fatal(err)
	}
	if err := FormPage(Form{FirstWeek: true}).Render(ctx, &on); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(off.String(), "first_week_template") {
		t.Error("the pick is on the form with first weeks off")
	}
	html := on.String()
	for _, want := range []string{"¿Qué quieres cambiar en los próximos 7 días?", "Dormir mejor", `value="journal"`} {
		if !strings.Contains(html, want) {
			t.Errorf("form is missing %q", want)
		}
	}
	// Custom is chosen until the person picks: a submit without touching the
	// pick runs the week on the goal they typed.
	if !strings.Contains(html, `value="custom" checked`) && !strings.Contains(html, `checked value="custom"`) {
		t.Errorf("custom is not the default pick")
	}
}

func TestTheDonePageOffersOneSourceWhenAWeekStarted(t *testing.T) {
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), "pt-BR")
	if err := DonePage(users.User{DisplayName: "Ana"}, "", true).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, want := range []string{"Conecte uma fonte", `href="/app/fitness"`, `href="/app/settings/connections"`, `href="/app/knowledge"`, "Vou registrar à mão"} {
		if !strings.Contains(html, want) {
			t.Errorf("done page is missing %q", want)
		}
	}
}
