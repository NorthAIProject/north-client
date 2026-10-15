package weekly

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/weekly/week"
)

func renderWeeklyIn(t *testing.T, locale string) string {
	t.Helper()

	rv := week.Review{
		Reviewing: time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC),
		Planning:  time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC),
		Goals:     []week.Goal{{ID: uuid.New(), Title: "Run a 10K"}},
	}
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), locale)
	if err := Page(users.User{DisplayName: "Ana"}, rv, Form{Saved: true}).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestWeeklyRendersInTheChosenLanguage(t *testing.T) {
	cases := map[string]struct {
		want   []string
		absent []string
	}{
		"en":    {want: []string{"Plan the week of 9 Mar", "Priorities", "Save the week", "Deload"}},
		"es":    {want: []string{"Planifica la semana del 9 mar", "Prioridades", "Guardar la semana", "Descarga", "Run a 10K"}, absent: []string{"Save the week", "Goals in order", "Train the plan as written."}},
		"pt-PT": {want: []string{"Planeia a semana de 9 mar", "Objetivos por ordem", "Guardar a semana"}, absent: []string{"Save the week", "Priorities"}},
		"pt-BR": {want: []string{"Planeje a semana de 9 mar", "Objetivos em ordem", "Salvar a semana"}, absent: []string{"Save the week", "Goals in order"}},
	}

	for locale, tc := range cases {
		html := renderWeeklyIn(t, locale)
		for _, want := range tc.want {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
		for _, english := range tc.absent {
			if strings.Contains(html, english) {
				t.Errorf("%s: page still contains the English %q", locale, english)
			}
		}
	}
}
