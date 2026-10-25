package award_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

func TestMonthlyCoinMonthIsShortUpperAndLocalised(t *testing.T) {
	for _, c := range []struct{ locale, key, want string }{
		{"en", "2026-10", "OCT"},
		{"en", "2026-09", "SEP"},
		{"es", "2026-09", "SEPT"},
		{"es", "2026-01", "ENE"},
		{"pt-PT", "2026-10", "OUT"},
		{"pt-BR", "2026-02", "FEV"},
		{"en", "bad", ""},
	} {
		ctx := i18n.WithLocale(context.Background(), c.locale)
		if got := award.MonthlyCoinMonth(ctx, c.key); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.locale, c.key, got, c.want)
		}
	}
}

// A target of 1 reads in the singular on every line that carries it, in every
// locale, and never says "1 days".
func TestMonthlyTargetOneIsSingular(t *testing.T) {
	want := map[string]map[string][3]string{
		"en":    {"checkin_days": {"Check in on 1 day in October", "0 of 1 check-in day", "Checked in on 1 day in October"}, "workouts": {"Log 1 workout in October", "0 of 1 workout", "Logged 1 workout in October"}},
		"es":    {"habit_days": {"Mantén un hábito durante 1 día en octubre", "0 de 1 día de hábito", "Mantuviste un hábito durante 1 día en octubre"}},
		"pt-PT": {"checkin_days": {"Faz check-in em 1 dia de outubro", "0 de 1 dia de check-in", "Fizeste check-in em 1 dia de outubro"}},
		"pt-BR": {"workouts": {"Registre 1 treino em outubro", "0 de 1 treino", "Você registrou 1 treino em outubro"}},
	}
	for locale, byMetric := range want {
		ctx := i18n.WithLocale(context.Background(), locale)
		for metric, w := range byMetric {
			v := award.MonthlyChallenge(ctx, "2026-10", metric, 1, 0, false)
			m := award.MonthlyMedal(ctx, "2026-10", metric, 1, timeNowForTest())
			got := [3]string{v.Goal, v.ProgressText, m.Description}
			if got != w {
				t.Errorf("%s %s = %q, want %q", locale, metric, got, w)
			}
		}
	}
	ctx := i18n.WithLocale(context.Background(), "en")
	if got := award.MonthlyChallenge(ctx, "2026-10", "checkin_days", 2, 0, false).Goal; got != "Check in on 2 days in October" {
		t.Errorf("target 2 = %q", got)
	}
}

func timeNowForTest() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) }
