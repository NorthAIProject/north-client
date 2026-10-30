package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
)

// Throttle bounds how often one account may be guessed at: sign-in, sign-up
// and password-reset attempts against one email, from every address at once.
//
// # Why this is separate from internal/quota
//
// quota counts in Postgres and is keyed on a signed-in account. On these routes
// nobody is signed in yet — that is the whole point of them — so the key is the
// email being tried, and a bucket per email is cheap enough to keep in memory.
//
// # Why there is no address bucket
//
// These routes are called by the native app and by the web app's server, and
// the second means every web sign-in arrives from the same Cloudflare Worker
// address: the web app forwards no browser address Go could trust. A per-address
// limit would throttle everybody signing in on the web at once. Adding one back
// needs an address the edge vouches for, not X-Forwarded-For, which any caller
// of /api/v1 can set.
//
// # What it is and is not
//
// It is a floor. It stops an unattended loop from guessing one account's
// password, and a script from mailing one person reset links. It is not a
// defence against spraying one password across many accounts.
//
// # Why in memory is enough today
//
// The API Deployment runs a single replica, so one process sees every attempt.
// At two replicas each holds its own buckets and the effective limit doubles,
// which is a weakening rather than a break — but it is the point at which this
// should move to the quota counter table.
type Throttle struct {
	emailBuckets *ratelimit.Store
	clientIP     echo.IPExtractor
	log          *slog.Logger
}

// ThrottleConfig is the per-minute allowance for one account.
type ThrottleConfig struct {
	// PerEmailPerMinute bounds attempts against one account from every
	// address at once. Generous enough that a person mistyping a password
	// never meets it.
	PerEmailPerMinute int

	// TrustedProxies decide which address a refusal logs. They key nothing;
	// see middleware.TrustedProxies.IPExtractor.
	TrustedProxies middleware.TrustedProxies
}

// DefaultEmailAttemptsPerMinute is chosen so a real person never sees it:
// five against one email is a mistyped password four times and a password
// manager retry. A guessing loop wants thousands.
const DefaultEmailAttemptsPerMinute = 5

func NewThrottle(cfg ThrottleConfig, log *slog.Logger) *Throttle {
	if cfg.PerEmailPerMinute <= 0 {
		cfg.PerEmailPerMinute = DefaultEmailAttemptsPerMinute
	}
	if log == nil {
		log = slog.Default()
	}

	return &Throttle{
		emailBuckets: ratelimit.New(cfg.PerEmailPerMinute),
		clientIP:     cfg.TrustedProxies.IPExtractor(),
		log:          log,
	}
}

// AllowAccount reports whether one more attempt against the account behind
// email fits its allowance, and logs the refusal when it does not. An empty
// email always passes: there is no account to guess at. A nil Throttle passes
// everything, which is the test wiring.
//
// Lower-cased so that two spellings of one address are one bucket; a caller
// that alternated the capitalisation would otherwise get a fresh allowance per
// variation.
func (t *Throttle) AllowAccount(r *http.Request, email string) bool {
	if t == nil {
		return true
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return true
	}
	if ok, _ := t.emailBuckets.Allow(email); ok {
		return true
	}
	// The email is a bucket key and never reaches the log. Recording which
	// accounts are being guessed would put a list of this deployment's users
	// into whatever reads the logs.
	t.log.Warn("authentication attempt throttled",
		slog.String("path", r.URL.Path),
		slog.String("client_ip", t.clientIP(r)))
	return false
}
