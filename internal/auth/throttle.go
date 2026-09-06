package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
)

// Throttle bounds how often one caller may try to authenticate.
//
// # Why this is separate from internal/quota
//
// quota counts in Postgres and is keyed on an account. On these routes there is
// no account yet — that is the whole point of them — so there is nothing to key
// on but an address, and an address is cheap enough to bound in memory. This is
// the same choice internal/mcpserver makes for its pre-authentication throttle.
//
// # What it is and is not
//
// It is a floor. A caller with many addresses walks around it, and that is
// understood: the job here is to make an unattended guessing loop from one place
// stop being free, and to keep a signup script from minting accounts that each
// spend model credit. It is not a defence against a distributed attack, and it
// is not a per-account lockout — see the note on emailBuckets below.
//
// # Why in memory is enough today
//
// The web Deployment runs a single replica, so one process sees every attempt.
// At two replicas each holds its own buckets and the effective limit doubles,
// which is a weakening rather than a break — but it is the point at which this
// should move to the quota counter table.
type Throttle struct {
	ipBuckets    *ratelimit.Limiters
	emailBuckets *ratelimit.Limiters
	trusted      middleware.TrustedProxies
	log          *slog.Logger
}

// ThrottleConfig is the per-minute allowance for one address.
type ThrottleConfig struct {
	// PerMinute bounds attempts from one address across all of these routes.
	// Generous enough that a person mistyping a password never meets it.
	PerMinute int

	// PerEmailPerMinute bounds attempts against one account from every address
	// at once. Lower, because a person only has one password to get wrong.
	PerEmailPerMinute int

	// TrustedProxies decide whether X-Forwarded-For may be believed. Empty
	// means key on the peer address, which is right on a laptop and wrong
	// behind an ingress — see middleware.ClientIP.
	TrustedProxies middleware.TrustedProxies
}

// Defaults chosen so a real person never sees them.
//
// Ten attempts a minute is a mistyped password four times and a password
// manager retry; five against one email is the same person on one account. A
// guessing loop wants thousands.
const (
	DefaultAttemptsPerMinute      = 10
	DefaultEmailAttemptsPerMinute = 5
)

func NewThrottle(cfg ThrottleConfig, log *slog.Logger) *Throttle {
	if cfg.PerMinute <= 0 {
		cfg.PerMinute = DefaultAttemptsPerMinute
	}
	if cfg.PerEmailPerMinute <= 0 {
		cfg.PerEmailPerMinute = DefaultEmailAttemptsPerMinute
	}
	if log == nil {
		log = slog.Default()
	}

	return &Throttle{
		ipBuckets:    ratelimit.New(cfg.PerMinute),
		emailBuckets: ratelimit.New(cfg.PerEmailPerMinute),
		trusted:      cfg.TrustedProxies,
		log:          log,
	}
}

// Guard is the middleware. It runs before the handler parses anything.
//
// A refusal is 429 with Retry-After and a plain body: these routes are reached
// by a browser form, but they are also reached by a script, and a script is the
// case this exists for.
func (t *Throttle) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t == nil {
			next.ServeHTTP(w, r)
			return
		}

		addr := middleware.ClientIP(r, t.trusted)
		if !t.ipBuckets.Allow(addr) {
			t.refuse(w, r, "address", addr)
			return
		}

		// Parsing the form here is safe: the handler calls the same method and
		// gets the cached result, exactly as the CSRF middleware relies on.
		if email := submittedEmail(r); email != "" && !t.emailBuckets.Allow(email) {
			// The email is a bucket key and never reaches the log. Recording
			// which accounts are being guessed would put a list of this
			// deployment's users into whatever reads the logs.
			t.refuse(w, r, "account", addr)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (t *Throttle) refuse(w http.ResponseWriter, r *http.Request, on, addr string) {
	t.log.Warn("authentication attempt throttled",
		slog.String("path", r.URL.Path),
		slog.String("keyed_on", on),
		slog.String("client_ip", addr))

	w.Header().Set("Retry-After", "60")
	http.Error(w, "Too many attempts. Wait a minute and try again.", http.StatusTooManyRequests)
}

// submittedEmail reads the account a request is aimed at, or "" when there is
// none to read.
//
// Lower-cased so that two spellings of one address are one bucket; a caller
// that alternated the capitalisation would otherwise get a fresh allowance per
// variation.
func submittedEmail(r *http.Request) string {
	if r.Method != http.MethodPost {
		return ""
	}
	if err := r.ParseForm(); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
}
