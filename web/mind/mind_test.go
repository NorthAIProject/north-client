package mind

import (
	"context"
	"strings"
	"testing"
)

// The journal form is re-rendered around its error messages when a submission
// is rejected, so whatever the writer typed has to come back with it. It did
// not: the reflection was passed to the textarea component as children, and
// that component renders Props.Value and has no children block, so the text was
// dropped on the floor and the writer had to type it again.
func TestJournalFormCardKeepsRejectedContent(t *testing.T) {
	mood := 4
	f := JournalForm{
		Content: "a reflection worth not losing",
		Mood:    &mood,
		Errors:  map[string]string{"content": "Keep it under 10000 characters."},
	}

	var b strings.Builder
	if err := journalFormCard(f).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := b.String()

	if !strings.Contains(body, f.Content) {
		t.Error("the submitted reflection was dropped from the re-rendered form")
	}
	if !strings.Contains(body, "Keep it under 10000 characters.") {
		t.Error("field error not rendered inline")
	}
	if !strings.Contains(body, `aria-invalid="true"`) {
		t.Error("textarea not marked invalid despite carrying an error")
	}
}

// Without an error the textarea must not claim to be invalid.
func TestJournalFormCardCleanFormIsNotMarkedInvalid(t *testing.T) {
	var b strings.Builder
	if err := journalFormCard(JournalForm{}).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(b.String(), `aria-invalid="true"`) {
		t.Error("clean form marked invalid")
	}
}
