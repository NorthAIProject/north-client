package legal

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// A translated policy has to say that the English text binds, and its date has
// to be in the reader's language too.
func TestATranslatedPolicySaysTheEnglishPrevails(t *testing.T) {
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), "es")
	if err := Terms().Render(ctx, &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := b.String()
	for _, want := range []string{"Términos del servicio", "prevalece el inglés", "29 de agosto de 2026", "No es consejo médico"} {
		if !strings.Contains(html, want) {
			t.Errorf("Spanish terms do not contain %q", want)
		}
	}
	if strings.Contains(html, "not a medical device") {
		t.Error("English prose on the Spanish terms page")
	}
}

// The English page carries no translation notice: it is the binding text.
func TestTheEnglishPolicyHasNoTranslationNotice(t *testing.T) {
	html := render(t, "privacy")
	if strings.Contains(html, i18n.Translate("en", "legal.doc.translation")) {
		t.Error("the English policy calls itself a translation")
	}
	if !strings.Contains(html, "29 August 2026") {
		t.Error("the English policy lost its date")
	}
}

// Operator details are interpolated into prose rendered unescaped, so they
// must be escaped on the way in.
func TestProseEscapesItsArguments(t *testing.T) {
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), "en")
	if err := prose(ctx, "legal.terms.law.p", "<script>x</script>").Render(ctx, &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(b.String(), "<script>") {
		t.Errorf("argument rendered unescaped: %s", b.String())
	}
}
