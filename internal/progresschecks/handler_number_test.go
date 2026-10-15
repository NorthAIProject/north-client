package progresschecks

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// A chip is read; a form field is parsed back by the browser. Only the chip
// takes the reader's decimal mark.
func TestChipsReadInTheReadersDecimalMarkAndFormsKeepAPoint(t *testing.T) {
	es := i18n.WithLocale(context.Background(), "es")
	if got := readNumber(es, 81.5); got != "81,5" {
		t.Errorf("readNumber(es, 81.5) = %q, want 81,5", got)
	}
	if got := readNumber(es, 82.0); got != "82" {
		t.Errorf("readNumber(es, 82.0) = %q, want 82", got)
	}
	if got := delta(es, -1.25); got != "−1,3" {
		t.Errorf("delta(es, -1.25) = %q, want −1,3", got)
	}
	if got := readNumber(context.Background(), 81.5); got != "81.5" {
		t.Errorf("readNumber(en, 81.5) = %q, want 81.5", got)
	}
	if got := number(81.5); got != "81.5" {
		t.Errorf("number(81.5) = %q, want 81.5 for a form field", got)
	}
}
