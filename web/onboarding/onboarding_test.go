package onboarding

import (
	"context"
	"strings"
	"testing"
)

func renderForm(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	if err := FormPage(Form{}).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// Skip sits under the card in its own form. Nested forms are invalid HTML and
// the browser would drop the inner one, so the only way to place it outside the
// card is to give it a form of its own — which is safe because the handler
// ignores every field above it.
func TestSkipIsItsOwnFormBelowTheCard(t *testing.T) {
	out := renderForm(t)

	if strings.Contains(out, "formaction") {
		t.Error("skip is still a formaction on the main form's submit button")
	}
	if !strings.Contains(out, `action="/app/onboarding/skip"`) {
		t.Fatalf("no skip form rendered:\n%s", out)
	}

	skipAt := strings.Index(out, `action="/app/onboarding/skip"`)
	mainAt := strings.Index(out, `action="/app/onboarding"`)
	if mainAt == -1 {
		t.Fatal("main onboarding form missing")
	}
	if skipAt < mainAt {
		t.Error("skip form renders before the main form; it belongs under the card")
	}

	// The main form must be closed before the skip form opens, or the skip form
	// is nested and the browser will discard it.
	mainClose := strings.Index(out[mainAt:], "</form>")
	if mainClose == -1 || mainAt+mainClose > skipAt {
		t.Error("skip form is nested inside the main form")
	}
}

// Skipping is a state change, so it posts, and a post without the token is
// rejected by the CSRF middleware — the button would look fine and do nothing.
func TestSkipFormCarriesCSRF(t *testing.T) {
	out := renderForm(t)

	skipAt := strings.Index(out, `action="/app/onboarding/skip"`)
	if skipAt == -1 {
		t.Fatal("no skip form rendered")
	}
	form := out[skipAt:]
	if end := strings.Index(form, "</form>"); end != -1 {
		form = form[:end]
	}

	if !strings.Contains(form, "csrf_token") {
		t.Errorf("skip form has no CSRF token:\n%s", form)
	}
	if !strings.Contains(form, "Skip for now") {
		t.Errorf("skip form has no button:\n%s", form)
	}
}
