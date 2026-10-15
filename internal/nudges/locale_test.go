package nudges

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/users"
)

// A nudge is written in its recipient's language when it is raised, since the
// sweep has no request to carry one. An account without a locale reads English.
func TestNudgeTextSpeaksTheRecipientsLanguage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		locale users.Locale
		got    func(users.User) string
		want   string
	}{
		{"", func(u users.User) string { return text(u, "nudges.streak.title", 5) }, "Your 5-day streak ends at midnight"},
		{"es", func(u users.User) string { return text(u, "nudges.streak.title", 5) }, "Tu racha de 5 días termina a medianoche"},
		{"pt-PT", func(u users.User) string { return text(u, "nudges.streak.title", 5) }, "A tua sequência de 5 dias termina à meia-noite"},
		{"es", func(u users.User) string { return text(u, "nudges.missed.body.days", 7) }, "Han pasado 7 días desde tu último registro."},
		{"pt-PT", func(u users.User) string { return text(u, "nudges.missed.body.days", 7) }, "Passaram 7 dias desde o teu último registo."},
		{"es", func(u users.User) string { return text(u, "nudges.medication.body", "08:00") }, "Tu dosis de las 08:00. Márcala como tomada u omitida cuando puedas."},
		{"pt-PT", func(u users.User) string { return text(u, "nudges.decision.body.month", "Mudar de casa") }, "Há um mês decidiste: Mudar de casa. Como correu?"},
		{"", func(u users.User) string { return crewTitle(u, []string{"Ana", "Leo", "Rui"}) }, "Ana, Leo and 1 more checked in today"},
		{"es", func(u users.User) string { return crewTitle(u, []string{"Ana", "Leo"}) }, "Ana y Leo hicieron su registro hoy"},
		{"pt-PT", func(u users.User) string { return crewTitle(u, []string{"Ana", "Leo", "Rui", "Eva"}) }, "Ana, Leo e mais 2 fizeram o registo hoje"},
	}
	for _, c := range cases {
		if got := c.got(users.User{Locale: c.locale}); got != c.want {
			t.Errorf("%q: got %q, want %q", c.locale, got, c.want)
		}
	}
}

// The weekday in a deadline's title is named in the recipient's language too.
func TestGoalDeadlineTitleNamesTheWeekdayInTheRecipientsLanguage(t *testing.T) {
	t.Parallel()

	wednesday := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	cases := map[users.Locale]string{
		"":      "“Run a 10K” is due Wednesday",
		"es":    "“Run a 10K” vence el miércoles",
		"pt-PT": "“Run a 10K” — prazo: quarta-feira",
	}
	for locale, want := range cases {
		u := users.User{Locale: locale}
		got := text(u, "nudges.goal.title", "Run a 10K", weekday(u, wednesday))
		if got != want {
			t.Errorf("%q: got %q, want %q", locale, got, want)
		}
	}
}
