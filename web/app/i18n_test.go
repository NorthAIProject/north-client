package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderDashboardIn(t *testing.T, locale users.Locale, data DashboardData) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	user := users.User{DisplayName: "Ada Lovelace", Locale: locale}
	if err := Dashboard(user, data).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func weekRange() RangeView {
	return RangeView{
		Key:   "week",
		Label: "Last 7 days",
		Options: []RangeOption{
			{Key: "today", Label: "Today"},
			{Key: "week", Label: "Last 7 days", Selected: true},
		},
	}
}

// The first page after signing in.
func TestDashboardRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Command center", "Hello, Ada", "Last 7 days", "Mood &amp; energy"},
		users.LocalePTPT: {"Centro de comando", "Olá, Ada", "Últimos 7 dias", "Humor e energia"},
		users.LocalePTBR: {"Central de comando", "Olá, Ada", "Últimos 7 dias", "Humor e energia"},
		users.LocaleES:   {"Centro de mando", "Hola, Ada", "Últimos 7 días", "Ánimo y energía"},
	}

	for locale, wants := range cases {
		html := renderDashboardIn(t, locale, DashboardData{Range: weekRange()})
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: dashboard does not contain %q", locale, want)
			}
		}
	}
}

// The range selector is shared with the insights pages and its labels come
// from internal/shared/timerange, which has no context. RangeView derives the
// catalogue key from timerange.Key instead.
func TestRangeLabelsAreTranslated(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocalePTPT: {"Hoje", "Últimos 7 dias"},
		users.LocaleES:   {"Hoy", "Últimos 7 días"},
	}
	for locale, wants := range cases {
		html := renderDashboardIn(t, locale, DashboardData{Range: weekRange()})
		for _, want := range wants {
			if !strings.Contains(html, ">"+want+"<") {
				t.Errorf("%s: range option %q is not translated", locale, want)
			}
		}
	}
}

// A range key this build stops naming should still read as a period, not as
// "range.fortnight".
func TestAnUnknownRangeFallsBackToTheHandlersEnglish(t *testing.T) {
	data := DashboardData{Range: RangeView{Key: "fortnight", Label: "Last 14 days"}}
	html := renderDashboardIn(t, users.LocalePTPT, data)

	if !strings.Contains(html, "Last 14 days") {
		t.Error("an unknown range key did not fall back to the English label")
	}
	if strings.Contains(html, "range.fortnight") {
		t.Error("the raw catalogue key reached the page")
	}
}

// The streak reads "1 day", never "1 days" — which is wrong in all four.
func TestStreakUsesTheSingularForOneDay(t *testing.T) {
	cases := map[users.Locale]string{
		users.LocaleEN:   "1 day",
		users.LocalePTPT: "1 dia",
		users.LocaleES:   "1 día",
	}
	for locale, want := range cases {
		html := renderDashboardIn(t, locale, DashboardData{Range: weekRange(), Streak: 1})
		if !strings.Contains(html, want) {
			t.Errorf("%s: expected the singular %q", locale, want)
		}
		if strings.Contains(html, "1 days") || strings.Contains(html, "1 dias") || strings.Contains(html, "1 días") {
			t.Errorf("%s: rendered a plural for a one-day streak", locale)
		}
	}
}

// The timeline panel has its own sentence. It briefly shared the page header's
// during the conversion, which read as "Everything across Khepri" on a card
// about one window of logged activity.
func TestTheTimelinePanelKeepsItsOwnDescription(t *testing.T) {
	html := renderDashboardIn(t, users.LocaleEN, DashboardData{Range: weekRange()})

	if !strings.Contains(html, "Everything you did, Last 7 days.") {
		t.Error("the timeline panel is missing its own description")
	}
	if strings.Count(html, "Everything across Khepri") != 1 {
		t.Error("the header's sentence appears somewhere other than the header")
	}
}

// The panels' counting lines are sentences, not numbers glued to English.
func TestHabitsAndHydrationLinesAreTranslated(t *testing.T) {
	data := DashboardData{
		Habits:    HabitsView{HasHabits: true, Kept: 5, Scheduled: 7, BestStreak: 3},
		Hydration: HydrationView{TodayML: 1500, TargetML: 2000, Percent: 75},
	}
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"5 kept of 7 scheduled", "best streak 3", "Today 75% · 1.5 L"},
		users.LocalePTPT: {"5 cumpridos de 7 previstos", "melhor sequência: 3", "Hoje 75% · 1,5 L"},
		users.LocalePTBR: {"5 cumpridos de 7 previstos", "melhor sequência: 3", "Hoje 75% · 1,5 L"},
		users.LocaleES:   {"5 cumplidos de 7 previstos", "mejor racha: 3", "Hoy 75% · 1,5 L"},
	}
	for locale, wants := range cases {
		ctx := i18n.WithLocale(context.Background(), string(locale))
		var b strings.Builder
		if err := habitsPanel(data).Render(ctx, &b); err != nil {
			t.Fatalf("render habits %s: %v", locale, err)
		}
		if err := hydrationPanel(data).Render(ctx, &b); err != nil {
			t.Fatalf("render hydration %s: %v", locale, err)
		}
		for _, want := range wants {
			if !strings.Contains(b.String(), want) {
				t.Errorf("%s: panels do not contain %q", locale, want)
			}
		}
	}
}

// Feed rows say how long ago in the reader's language, and a row older than a
// week names the month in it too.
func TestRelativeTimeIsTranslated(t *testing.T) {
	es := i18n.WithLocale(context.Background(), string(users.LocaleES))
	pt := i18n.WithLocale(context.Background(), string(users.LocalePTPT))
	en := context.Background()

	cases := []struct {
		ctx  context.Context
		ago  time.Duration
		want string
	}{
		{en, 10 * time.Second, "just now"},
		{en, 5 * time.Minute, "5m ago"},
		{es, 10 * time.Second, "ahora mismo"},
		{es, 3 * time.Hour, "hace 3 h"},
		{pt, 2 * 24 * time.Hour, "há 2 d"},
	}
	for _, c := range cases {
		if got := relativeTime(c.ctx, time.Now().Add(-c.ago)); got != c.want {
			t.Errorf("relativeTime(-%s) = %q, want %q", c.ago, got, c.want)
		}
	}

	august := time.Date(2025, time.August, 4, 12, 0, 0, 0, time.UTC)
	if got := relativeTime(es, august); got != "4 ago" {
		t.Errorf("Spanish date = %q, want %q", got, "4 ago")
	}
}

func TestGoalActivityLabelIsTranslated(t *testing.T) {
	pt := i18n.WithLocale(context.Background(), string(users.LocalePTBR))
	if got := goalActivityLabel(pt, 3); got != "3 nesta semana" {
		t.Errorf("pt-BR: got %q", got)
	}
	if got := goalActivityLabel(context.Background(), 1); got != "1 this week" {
		t.Errorf("en: got %q", got)
	}
}
