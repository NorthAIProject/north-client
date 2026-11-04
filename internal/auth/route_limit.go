package auth

import (
	"log/slog"

	"github.com/labstack/echo/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
)

// These bound how fast the app's own clients may call /api/v1. They are floors
// against a retry loop starving the database pool, so they sit far above what
// the clients do, and a person using the app should never meet them. The spend
// and abuse limits are internal/quota's job, per action.
//
// What the clients do, read from their code on 2026-11-04 (not measured from
// traffic):
//
//   - The heaviest web page, a nutrition plan, has about 15 API call sites; most
//     have 4 to 8. No iOS screen has more than 5 concurrent call sites, though a
//     screen can open several in turn.
//   - The iOS health sync sends batches of 2000 readings one after another, so
//     its 90-day backfill is a few requests, and it gives up at the first
//     refusal. The bound for a phone on /ingest/health is 240 a minute.
//
// ratelimit.New also allows a full minute's worth as a burst, so all of a bound
// can arrive at once. Each web replica counts on its own, which makes the real
// allowance higher still.
const (
	// DefaultBearerRequestsPerAccount is ten requests a second, held: a person
	// loading forty calls' worth of pages every four seconds, indefinitely. That
	// leaves room for health sync and a live crew on top, at 2.5 times what the
	// phone gets on /ingest/health, since this surface carries all of that and
	// every screen. A retry loop passes it within a second or two.
	DefaultBearerRequestsPerAccount = 600

	// DefaultBearerRequestsPerAddress is five accounts at their full bound.
	//
	// Higher than the account bound because an address is not a person. A
	// household, an office or a mobile carrier's NAT shares one, and web calls
	// share one too if the Worker's forwarded address is ever lost, since
	// Cloudflare then reports the Worker's own (see middleware.TrustedProxies).
	// This one only exists so a loop guessing tokens costs a map lookup rather
	// than a session query, and fifty a second of those is still survivable.
	DefaultBearerRequestsPerAddress = 3000
)

// retryAfterSeconds is what a refusal asks a client to wait. The buckets refill
// continuously, so a second is always enough for at least one request.
const retryAfterSeconds = "1"

// RouteLimits is the per-minute allowance on bearer routes. Zero uses the
// defaults above.
type RouteLimits struct {
	PerAddressPerMinute int
	PerAccountPerMinute int
}

// RouteLimiter bounds bearer routes: by address before the token is checked, by
// account after. One instance per process, shared by every bearer group, because
// a limit each group counts separately would let a caller spend it once per
// group.
type RouteLimiter struct {
	addresses *ratelimit.Store
	accounts  *ratelimit.Store
	log       *slog.Logger
}

func NewRouteLimiter(limits RouteLimits, log *slog.Logger) *RouteLimiter {
	if limits.PerAddressPerMinute <= 0 {
		limits.PerAddressPerMinute = DefaultBearerRequestsPerAddress
	}
	if limits.PerAccountPerMinute <= 0 {
		limits.PerAccountPerMinute = DefaultBearerRequestsPerAccount
	}
	if log == nil {
		log = slog.Default()
	}
	return &RouteLimiter{
		addresses: ratelimit.New(limits.PerAddressPerMinute),
		accounts:  ratelimit.New(limits.PerAccountPerMinute),
		log:       log,
	}
}

// PerAddress goes in front of Bearer, where an address is the only identity a
// request has.
//
// c.RealIP is the address echox.New configured: the caller Cloudflare names when
// the hop is its edge, otherwise the earliest hop behind the trusted proxies. Not
// RemoteAddr, which behind an ingress is the ingress, so one bucket would be
// shared by the whole internet and any one caller could empty it.
func (l *RouteLimiter) PerAddress() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			addr := c.RealIP()
			if ok, _ := l.addresses.Allow(addr); !ok {
				l.log.Warn("bearer request throttled before authentication",
					slog.String("path", c.Path()),
					slog.String("client_ip", addr))
				return refuse(c)
			}
			return next(c)
		}
	}
}

// PerAccount goes behind Bearer, so the allowance belongs to a known account and
// an unauthenticated caller can never spend somebody else's.
func (l *RouteLimiter) PerAccount() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			user, ok := UserFrom(c.Request().Context())
			if !ok {
				// Unreachable behind Bearer. Refused rather than let through,
				// because it would mean the route is mounted wrong.
				return httpx.Fail(apperr.ErrUnauthenticated, "A bearer token is required.")
			}
			if allowed, _ := l.accounts.Allow(user.ID.String()); !allowed {
				l.log.Warn("bearer request throttled",
					slog.String("path", c.Path()),
					slog.String("user_id", user.ID.String()))
				return refuse(c)
			}
			return next(c)
		}
	}
}

func refuse(c *echo.Context) error {
	c.Response().Header().Set("Retry-After", retryAfterSeconds)
	return httpx.Fail(httpx.ErrRateLimited, i18n.T(c.Request().Context(), "auth.err.rate_limited"))
}
