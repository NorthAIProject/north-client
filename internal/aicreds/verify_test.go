package aicreds_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// stubVerifier stands in for the live provider call.
type stubVerifier struct {
	err    error
	asked  int
	sawKey string
}

func (v *stubVerifier) Verify(_ context.Context, _ providers.BYOProvider, key string) error {
	v.asked++
	v.sawKey = key
	return v.err
}

// The whole point: a mistyped key is refused while the person is still looking
// at the form, rather than discovered later as a coach that quietly got worse.
func TestARejectedKeyIsNotStored(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	verifier := &stubVerifier{err: aicreds.ErrKeyRejected}
	svc = svc.WithVerifier(verifier)
	ctx := context.Background()

	_, err := svc.Save(ctx, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})

	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("err = %v, want FieldErrors", err)
	}
	msg, ok := fieldErrs.Messages()["api_key"]
	if !ok {
		t.Fatalf("errors = %v, want one on api_key", fieldErrs.Messages())
	}
	if !strings.Contains(msg, "OpenRouter") {
		t.Errorf("message = %q, want it to name the provider", msg)
	}
	if strings.Contains(msg, testKey) {
		t.Errorf("the message echoes the key: %q", msg)
	}

	if verifier.asked != 1 {
		t.Errorf("the provider was asked %d times, want 1", verifier.asked)
	}
	if verifier.sawKey != testKey {
		t.Error("the verifier was not given the key being saved")
	}

	if _, err := svc.Get(ctx, user.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a rejected key was stored anyway: %v", err)
	}
}

// A provider having a bad minute says nothing about the key. Refusing then
// would make Khepri's settings page depend on somebody else's uptime.
func TestAnUnreachableProviderDoesNotBlockTheSave(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	svc = svc.WithVerifier(&stubVerifier{err: errors.New("dial tcp: no route to host")})
	ctx := context.Background()

	if _, err := svc.Save(ctx, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey}); err != nil {
		t.Fatalf("save was blocked by an unreachable provider: %v", err)
	}
	if _, err := svc.Get(ctx, user.ID); err != nil {
		t.Fatalf("the key was not stored: %v", err)
	}
}

func TestAnAcceptedKeyIsStored(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	verifier := &stubVerifier{}
	svc = svc.WithVerifier(verifier)
	ctx := context.Background()

	if _, err := svc.Save(ctx, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if verifier.asked != 1 {
		t.Errorf("the provider was asked %d times, want 1", verifier.asked)
	}
	if _, err := svc.Get(ctx, user.ID); err != nil {
		t.Fatalf("the key was not stored: %v", err)
	}
}

// A model-only save has no key to check, and must not spend a network call
// asking about one.
func TestAModelOnlySaveDoesNotCallTheProvider(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	verifier := &stubVerifier{}
	svc = svc.WithVerifier(verifier)
	ctx := context.Background()

	if _, err := svc.Save(ctx, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey}); err != nil {
		t.Fatalf("save: %v", err)
	}
	before := verifier.asked

	if _, err := svc.Save(ctx, user.ID, aicreds.Input{Provider: "openrouter", Model: "openai/gpt-4.1"}); err != nil {
		t.Fatalf("model-only save: %v", err)
	}
	if verifier.asked != before {
		t.Errorf("the provider was asked again for a model-only change (%d → %d)", before, verifier.asked)
	}
}

// The live verifier, against a stand-in for the provider.
//
// Only an explicit refusal counts as one. A 404 from a backend that routes
// /models differently, or a 500 from one having a bad minute, must not read as
// evidence about the key.
func TestHTTPVerifier(t *testing.T) {
	cases := map[string]struct {
		status   int
		rejected bool
	}{
		"accepted":     {http.StatusOK, false},
		"unauthorised": {http.StatusUnauthorized, true},
		"forbidden":    {http.StatusForbidden, true},
		"not found":    {http.StatusNotFound, false},
		"server error": {http.StatusInternalServerError, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				w.WriteHeader(tc.status)
				// A real provider's error body can quote the credential back,
				// which is why the verifier never reads one.
				_, _ = w.Write([]byte(`{"error":"` + testKey + `"}`))
			}))
			defer srv.Close()

			entry := providers.BYOProvider{
				Name: "openrouter", Label: "OpenRouter",
				BaseURL: srv.URL, VerifyPath: "/models",
			}

			err := aicreds.NewHTTPVerifier(srv.Client()).Verify(context.Background(), entry, testKey)

			if tc.rejected != errors.Is(err, aicreds.ErrKeyRejected) {
				t.Fatalf("err = %v, rejected = %v, want rejected = %v", err, !tc.rejected, tc.rejected)
			}
			if gotAuth != "Bearer "+testKey {
				t.Errorf("Authorization = %q, want the bearer form", gotAuth)
			}
		})
	}
}

// A provider with no models endpoint — Gemini — is stored unverified rather
// than refused.
func TestAProviderWithNoModelsEndpointIsNotChecked(t *testing.T) {
	entry := providers.BYOProvider{Name: "gemini", Label: "Google Gemini"}

	if err := aicreds.NewHTTPVerifier(nil).Verify(context.Background(), entry, testKey); err != nil {
		t.Fatalf("err = %v, want nil when there is nothing to ask", err)
	}
}

func TestSaveStillWorksWithNoVerifier(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	svc = svc.WithVerifier(nil)

	if _, err := svc.Save(context.Background(), user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey}); err != nil {
		t.Fatalf("save: %v", err)
	}
}
