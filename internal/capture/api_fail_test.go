package capture

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// A provider's refusal reaches the capture API below 500 — 402 out of credit,
// 403 a key it will not take — and its text names the model and quotes the
// provider's response body. None of that is the caller's business.
func TestAPIFailKeepsProviderErrorsOffTheWire(t *testing.T) {
	t.Parallel()

	a := &API{log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for _, sentinel := range []error{apperr.ErrPaymentRequired, apperr.ErrForbidden} {
		err := fmt.Errorf("parse the capture: %w: openrouter=nvidia/nemotron-3-ultra-550b-a55b:free returned 402: {\"error\":\"nope\"}", sentinel)

		rec := httptest.NewRecorder()
		a.fail(rec, err, "That could not be read.")

		body := rec.Body.String()
		for _, leak := range []string{"openrouter", "nemotron", "nope"} {
			if strings.Contains(body, leak) {
				t.Errorf("%v: body %s leaks %q", sentinel, body, leak)
			}
		}
		if !strings.Contains(body, "That could not be read.") {
			t.Errorf("%v: body %s lacks the fixed message", sentinel, body)
		}
	}

	// A validation failure still carries its own sentence.
	rec := httptest.NewRecorder()
	a.fail(rec, apperr.Wrap(apperr.ErrValidation, "write down what you want to log first"), "That could not be read.")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Write down what you want to log first") {
		t.Errorf("validation lost its sentence: %d %s", rec.Code, rec.Body.String())
	}
}
