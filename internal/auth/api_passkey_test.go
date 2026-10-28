package auth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
)

// A server without passkeys configured answers every ceremony route with a
// JSON 404, never an HTML page or a redirect the native client cannot read.
func TestPasskeyRoutesWithoutPasskeysAreJSON404(t *testing.T) {
	t.Parallel()

	e := echox.New(echox.Config{})
	auth.NewAPI(fakeSessionResolver{}).WithAuthService(&auth.Service{}, &auth.Middleware{}).RegisterPublic(e.Group(""))

	for _, path := range []string{
		"/auth/passkey/register/begin",
		"/auth/passkey/register/finish",
		"/auth/passkey/login/begin",
		"/auth/passkey/login/finish",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Fatalf("content type = %q, want JSON", got)
			}
		})
	}
}

func TestPasskeyCeremonyResponseShape(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "passkey-ceremony.golden.json", auth.PasskeyCeremonyResponse{
		ChallengeID: "66666666-6666-6666-6666-666666666666",
		PublicKey: map[string]any{
			"challenge":        "dGhlLWNoYWxsZW5nZQ",
			"rpId":             "kheprios.com",
			"timeout":          60000,
			"userVerification": "preferred",
		},
	})
}
