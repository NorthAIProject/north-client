package insights

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/insights/insightview"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// localizedSummaryView builds the view the way digestView does for a user who
// reads tag: under that user's locale.
func localizedSummaryView(t *testing.T, tag string, data SummaryData) insightview.SummaryView {
	t.Helper()
	ctx := digestContext(context.Background(), users.User{Locale: users.Locale(tag)})
	view, err := buildSummaryView(ctx, data)
	if err != nil {
		t.Fatalf("buildSummaryView: %v", err)
	}
	return view
}

func TestDigestReadsInTheRecipientsLanguage(t *testing.T) {
	cases := map[string]struct {
		domain, onTrack, rangeLabel string
	}{
		"es":    {domain: "Cuerpo", onTrack: " van bien.", rangeLabel: "Últimos 7 días"},
		"pt-PT": {domain: "Corpo", onTrack: " no bom caminho.", rangeLabel: "Últimos 7 dias"},
	}
	for tag, want := range cases {
		t.Run(tag, func(t *testing.T) {
			view := localizedSummaryView(t, tag, wellLogged(t))

			got := renderDigest(tag, view, "https://north.test")

			for _, s := range []string{want.domain, want.onTrack, "*" + want.rangeLabel + "*"} {
				if !strings.Contains(got, s) {
					t.Errorf("digest does not say %q:\n%s", s, got)
				}
			}
			for _, english := range []string{"on track", "Body", "Last 7 days"} {
				if strings.Contains(got, english) {
					t.Errorf("digest still says %q in English:\n%s", english, got)
				}
			}
			// The link is a URL, not copy: it must not change with the language.
			if !strings.Contains(got, "https://north.test/app/insights?range="+view.Range.Key) {
				t.Errorf("digest lost its link:\n%s", got)
			}
		})
	}
}

func TestEmptyDigestReadsInTheRecipientsLanguage(t *testing.T) {
	cases := map[string]string{
		"en":    "Nothing logged last 7 days yet.",
		"es":    "Últimos 7 días: aún no hay nada registrado.",
		"pt-PT": "Últimos 7 dias: ainda não há nada registado.",
	}
	for tag, want := range cases {
		view := localizedSummaryView(t, tag, SummaryData{Range: weekRange(t)})
		if got := renderDigest(tag, view, "https://north.test"); got != want {
			t.Errorf("%s: empty digest = %q, want %q", tag, got, want)
		}
	}
}

func TestDigestContextUsesTheUsersLocale(t *testing.T) {
	// The worker sends with no request behind it; /stats arrives with one.
	// Either way the reader's own setting decides.
	ctx := i18n.WithLocale(context.Background(), "es")
	got := i18n.LocaleFrom(digestContext(ctx, users.User{Locale: users.Locale("pt-PT")}))
	if got != "pt-PT" {
		t.Errorf("digest locale = %q, want the user's pt-PT", got)
	}
}
