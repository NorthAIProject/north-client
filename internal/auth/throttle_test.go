package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/auth"
)

func attempt(t *auth.Throttle, remote, email string) bool {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	r.RemoteAddr = remote
	return t.AllowAccount(r, email)
}

// The reason the bucket is keyed on the account: web sign-ins all arrive from
// the Worker's address, and a botnet spreads one account's guesses across many.
func TestGuessingOneAccountFromManyAddressesIsRefused(t *testing.T) {
	t.Parallel()

	th := auth.NewThrottle(auth.ThrottleConfig{PerEmailPerMinute: 3}, nil)

	for i := range 3 {
		if !attempt(th, "203.0.113."+string(rune('1'+i))+":5000", "victim@north.test") {
			t.Fatalf("attempt %d refused while the budget still covered it", i+1)
		}
	}

	if attempt(th, "198.51.100.77:5000", "victim@north.test") {
		t.Fatal("a 4th address guessing the same account was let through")
	}
}

// Otherwise alternating the capitalisation buys a fresh allowance per spelling.
func TestTheEmailBucketIgnoresCase(t *testing.T) {
	t.Parallel()

	th := auth.NewThrottle(auth.ThrottleConfig{PerEmailPerMinute: 2}, nil)

	attempt(th, "203.0.113.1:5000", "victim@north.test")
	attempt(th, "203.0.113.2:5000", " VICTIM@North.Test ")

	if attempt(th, "203.0.113.3:5000", "Victim@north.test") {
		t.Fatal("a respelled address got a fresh budget")
	}
}

// Every web sign-in shares one address, so one account being guessed must not
// lock anybody else out.
func TestOneAccountBeingGuessedLeavesEveryoneElseAlone(t *testing.T) {
	t.Parallel()

	th := auth.NewThrottle(auth.ThrottleConfig{PerEmailPerMinute: 2}, nil)
	const worker = "[2a06:98c0:3600::103]:443"

	for range 3 {
		attempt(th, worker, "victim@north.test")
	}
	if attempt(th, worker, "victim@north.test") {
		t.Fatal("setup failed: the guessed account was not exhausted")
	}

	if !attempt(th, worker, "someone@north.test") {
		t.Fatal("an unrelated account from the same address was refused")
	}
}

// A request with no email — passkeys, Google — has no account to key on.
func TestAnAttemptWithoutAnEmailPasses(t *testing.T) {
	t.Parallel()

	th := auth.NewThrottle(auth.ThrottleConfig{PerEmailPerMinute: 1}, nil)

	for range 3 {
		if !attempt(th, "203.0.113.9:5000", "  ") {
			t.Fatal("an attempt with no email was refused")
		}
	}
}

// A nil throttle is the test wiring, and it must pass traffic rather than panic.
func TestANilThrottlePassesEverythingThrough(t *testing.T) {
	t.Parallel()

	var nilThrottle *auth.Throttle
	for range 10 {
		if !attempt(nilThrottle, "203.0.113.9:5000", "a@north.test") {
			t.Fatal("a nil throttle refused")
		}
	}
}

// The routes refuse before the service is reached: the service here is empty
// and would panic if called. The refusal is the API's JSON error, with a
// Retry-After so a script knows when to come back.
func TestTheCredentialRoutesRefuseAnExhaustedAccount(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ path, body string }{
		{"/auth/login", `{"email":"victim@north.test","password":"guess"}`},
		{"/auth/signup", `{"email":"victim@north.test","password":"guess","passwordConfirmation":"guess"}`},
		{"/auth/forgot-password", `{"email":"victim@north.test"}`},
	} {
		th := auth.NewThrottle(auth.ThrottleConfig{PerEmailPerMinute: 1}, nil)
		attempt(th, "203.0.113.9:5000", "victim@north.test")

		api := auth.NewAPI(nil).WithAuthService(&auth.Service{}, &auth.Middleware{}).WithThrottle(th)
		rec := postJSON(t, publicRouter(api), tc.path, tc.body)

		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("%s: code = %d, want 429; body %s", tc.path, rec.Code, rec.Body)
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s: no Retry-After on a 429", tc.path)
		}
		// The web app shows error.message to the person; anything else and it
		// falls back to the status text.
		var envelope struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error.Message == "" {
			t.Errorf("%s: body %s is not the API's JSON error envelope", tc.path, rec.Body)
		}
	}
}
