package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

type body struct {
	Name string `json:"name"`
}

func TestReadJSONRejectsUnknownFieldsByDefault(t *testing.T) {
	t.Parallel()

	var dst body
	err := read(t, `{"name":"a","extra":1}`, &dst, httpx.ReadOptions{})
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("want a validation error, got %v", err)
	}
}

func TestReadJSONKeepsUnknownFieldsWhenAsked(t *testing.T) {
	t.Parallel()

	var dst body
	if err := read(t, `{"name":"a","extra":1}`, &dst, httpx.ReadOptions{AllowUnknownFields: true}); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dst.Name != "a" {
		t.Fatalf("name = %q, want %q", dst.Name, "a")
	}
}

func TestReadJSONCapsTheBody(t *testing.T) {
	t.Parallel()

	var dst body
	err := read(t, `{"name":"`+strings.Repeat("x", 200)+`"}`, &dst, httpx.ReadOptions{MaxBytes: 16})
	if !apperr.Is(err, httpx.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if got := httpx.Status(err); got != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", got, http.StatusRequestEntityTooLarge)
	}
}

func TestStatusMapsEveryDomainError(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err  error
		want int
	}{
		"nil":             {nil, http.StatusOK},
		"validation":      {apperr.ErrValidation, http.StatusUnprocessableEntity},
		"not found":       {apperr.ErrNotFound, http.StatusNotFound},
		"unauthenticated": {apperr.ErrUnauthenticated, http.StatusUnauthorized},
		"forbidden":       {apperr.ErrForbidden, http.StatusForbidden},
		"conflict":        {apperr.ErrConflict, http.StatusConflict},
		"payment":         {apperr.ErrPaymentRequired, http.StatusPaymentRequired},
		"unavailable":     {apperr.ErrUnavailable, http.StatusServiceUnavailable},
		"unknown":         {apperr.New("boom"), http.StatusInternalServerError},
		"wrapped":         {apperr.Wrap(apperr.ErrNotFound, "load thing"), http.StatusNotFound},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := httpx.Status(tc.err); got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestErrorCarriesFieldMessages(t *testing.T) {
	t.Parallel()

	fields := apperr.FieldErrors{}.Add("amount", "must be positive")

	rec := httptest.NewRecorder()
	httpx.Error(rec, fields, "That log is not valid.")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}

	var got httpx.ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if got.Error.Fields["amount"] != "must be positive" {
		t.Fatalf("fields = %v, want the amount message", got.Error.Fields)
	}
}

// Error must never hand an internal message back to the caller.
func TestErrorDoesNotLeakTheUnderlyingMessage(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	httpx.Error(rec, apperr.New("pq: relation \"secret_table\" does not exist"), "Something went wrong.")

	if strings.Contains(rec.Body.String(), "secret_table") {
		t.Fatalf("reply leaked the internal error: %s", rec.Body.String())
	}
}

func read(t *testing.T, raw string, dst any, opts httpx.ReadOptions) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))
	return httpx.ReadJSON(httptest.NewRecorder(), req, dst, opts)
}
