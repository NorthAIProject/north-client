package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
)

// Throttle bounds how often sign-in, sign-up and password-reset may be tried:
// against one email from every address at once, and from one address against
// every email at once.
//
// # Why this is separate from internal/quota
//
// quota counts in Postgres and is keyed on a signed-in account. On these routes
// nobody is signed in yet — that is the whole point of them — so the key is the
// email being tried, and a bucket per email is cheap enough to keep in memory.
//
// # Where the address comes from
//
// kheprios.com is behind Cloudflare, and the web app's server signs people in
// from its Worker, so the address the ingress sees is an edge, not the caller.
// middleware.TrustedProxies.IPExtractor reads Cloudflare's CF-Connecting-IP for
// requests that came from the edge, and the web app's server sets X-Real-IP on
// its calls so that Cloudflare fills that header with the browser's address.
// Without both, every web sign-in would share one address bucket.
//
// # What it is and is not
//
// It is a floor. The email bucket stops an unattended loop from guessing one
// account's password, and a script from mailing one person reset links. The
// address bucket stops one machine spraying a password across many accounts.
// Neither stops a botnet that spreads a spray across addresses.
//
// # Why in memory is enough today
//
// The API Deployment runs a single replica, so one process sees every attempt.
// At two replicas each holds its own buckets and the effective limit doubles,
// which is a weakening rather than a break — but it is the point at which this
// should move to the quota counter table.
type Throttle struct {
	emailBuckets   *ratelimit.Store
	addressBuckets *ratelimit.Store
	clientIP       echo.IPExtractor
	log            *slog.Logger
}

// ThrottleConfig is the per-minute allowance for one account and one address.
type ThrottleConfig struct {
	// PerEmailPerMinute bounds attempts against one account from every
	// address at once. Generous enough that a person mistyping a password
	// never meets it.
	PerEmailPerMinute int

	// PerAddressPerMinute bounds attempts from one address against every
	// account at once. A household or an office behind one address shares it.
	PerAddressPerMinute int

	// TrustedProxies decide which address a request is counted against; see
	// middleware.TrustedProxies.IPExtractor.
	TrustedProxies middleware.TrustedProxies
}

// DefaultEmailAttemptsPerMinute is chosen so a real person never sees it:
// five against one email is a mistyped password four times and a password
// manager retry. A guessing loop wants thousands.
const DefaultEmailAttemptsPerMinute = 5

// DefaultAddressAttemptsPerMinute is the bound the Go web app's sign-in form
// had, which nobody signing in by hand ever reported meeting.
const DefaultAddressAttemptsPerMinute = 10

func NewThrottle(cfg ThrottleConfig, log *slog.Logger) *Throttle {
	if cfg.PerEmailPerMinute <= 0 {
		cfg.PerEmailPerMinute = DefaultEmailAttemptsPerMinute
	}
	if cfg.PerAddressPerMinute <= 0 {
		cfg.PerAddressPerMinute = DefaultAddressAttemptsPerMinute
	}
	if log == nil {
		log = slog.Default()
	}

	return &Throttle{
		emailBuckets:   ratelimit.New(cfg.PerEmailPerMinute),
		addressBuckets: ratelimit.New(cfg.PerAddressPerMinute),
		clientIP:       cfg.TrustedProxies.IPExtractor(),
		log:            log,
	}
}

// Allow reports whether one more attempt against the account behind email,
// from the request's address, fits both allowances, and logs the refusal when
// it does not. An empty email always passes: there is no account to guess at.
// A nil Throttle passes everything, which is the test wiring.
//
// Lower-cased so that two spellings of one address are one bucket; a caller
// that alternated the capitalisation would otherwise get a fresh allowance per
// variation.
func (t *Throttle) Allow(r *http.Request, email string) bool {
	if t == nil {
		return true
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return true
	}
	ip := t.clientIP(r)
	bucket := "address"
	if ok, _ := t.addressBuckets.Allow(ip); ok {
		if ok, _ := t.emailBuckets.Allow(email); ok {
			return true
		}
		bucket = "email"
	}
	// The email is a bucket key and never reaches the log. Recording which
	// accounts are being guessed would put a list of this deployment's users
	// into whatever reads the logs.
	t.log.Warn("authentication attempt throttled",
		slog.String("path", r.URL.Path),
		slog.String("bucket", bucket),
		slog.String("client_ip", ip))
	return false
}
