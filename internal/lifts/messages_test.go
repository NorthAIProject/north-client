package lifts

import (
	"context"
	"testing"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/i18n/fieldmsg"
)

// The service writes these messages in English; the table is how a handler
// finds their translation, so the two must agree word for word.
func TestFieldMessagesMatchTheCatalogue(t *testing.T) {
	t.Parallel()
	for _, m := range fieldmsg.Mismatches(fieldMessages) {
		t.Error(m)
	}
}

func TestSetErrorsReadTheRequestLanguage(t *testing.T) {
	t.Parallel()

	err := apperr.FieldErrors{}.Add("reps", "Enter between 1 and 100 reps.").Add("kind", "Choose work, warm-up or drop set.")
	var fields apperr.FieldErrors
	if !apperr.As(localize(i18n.WithLocale(context.Background(), "es"), err), &fields) {
		t.Fatal("expected field errors")
	}
	got := fields.Messages()
	if got["reps"] != "Escribe entre 1 y 100 repeticiones." || got["kind"] != "Elige serie efectiva, de calentamiento o descendente." {
		t.Fatalf("es messages = %v", got)
	}

	// No locale, and the coach's English, stay as written.
	if !apperr.As(localize(context.Background(), err), &fields) || fields.Messages()["reps"] != "Enter between 1 and 100 reps." {
		t.Fatalf("English messages = %v", fields.Messages())
	}
}
