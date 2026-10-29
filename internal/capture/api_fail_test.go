package capture

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// reply writes err the way the API's error handler does.
func reply(err error) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e := echox.New(echox.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	echox.ErrorHandler(e.NewContext(httptest.NewRequest(http.MethodPost, "/api/v1/capture/parse", nil), rec), err)
	return rec
}

// A provider's refusal reaches the capture API below 500 — 402 out of credit,
// 403 a key it will not take — and its text names the model and quotes the
// provider's response body. None of that is the caller's business.
func TestAPIFailKeepsProviderErrorsOffTheWire(t *testing.T) {
	t.Parallel()

	a := &API{log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for _, sentinel := range []error{apperr.ErrPaymentRequired, apperr.ErrForbidden} {
		err := fmt.Errorf("parse the capture: %w: openrouter=nvidia/nemotron-3-ultra-550b-a55b:free returned 402: {\"error\":\"nope\"}", sentinel)

		rec := reply(a.fail(err, "That could not be read."))

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
	rec := reply(a.fail(apperr.Wrap(apperr.ErrValidation, "write down what you want to log first"), "That could not be read."))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Write down what you want to log first") {
		t.Errorf("validation lost its sentence: %d %s", rec.Code, rec.Body.String())
	}
}

// A caller without a valid token is refused before any handler runs, with the
// challenge header and the JSON error body.
func TestAPIRefusesAnUnknownToken(t *testing.T) {
	t.Parallel()

	a := NewAPI(nil, rejectAll{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e := echox.New(echox.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	a.Register(e.Group("/api/v1"))

	for _, header := range []string{"", "Bearer nk_nope"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/capture/parse", strings.NewReader(`{"text":"two eggs"}`))
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%q: status = %d, want 401", header, rec.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="north-capture"` {
			t.Fatalf("%q: WWW-Authenticate = %q", header, got)
		}
		if got := rec.Body.String(); got != `{"error":{"message":"That token is not valid."}}`+"\n" {
			t.Fatalf("%q: body = %s", header, got)
		}
	}
}

type rejectAll struct{}

func (rejectAll) Authenticate(context.Context, string) (users.User, error) {
	return users.User{}, apperr.ErrUnauthenticated
}
