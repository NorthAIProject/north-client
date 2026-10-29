// Package ratelimit holds the per-key request bound every bearer-authenticated
// surface applies.
//
// One module rather than a copy per endpoint, because these surfaces share the
// property that makes the bound necessary: they are reachable from the open
// internet with nothing but a token in front of them. A limit that only one of
// them enforces is not a limit — an attacker picks the other door.
//
// The buckets are Echo's own memory store; this package only fixes the shape
// every caller shares, so services that are not HTTP handlers (crew chat,
// Telegram code redemption) count the same way the endpoints do.
package ratelimit

import (
	"time"

	echomw "github.com/labstack/echo/v5/middleware"
)

// DefaultPerMinute is used when a caller configures no bound. Generous for a
// human-driven client, and low enough that a retry loop is noticed.
const DefaultPerMinute = 120

// ttl bounds how long an idle key keeps its bucket.
//
// Long enough that a working session never loses its burst allowance, short
// enough that the map tracks active callers rather than every caller who has
// ever connected.
const ttl = 30 * time.Minute

// Store is Echo's in-memory token-bucket store. Allow never returns an error;
// the error exists for stores backed by something that can fail.
type Store = echomw.RateLimiterMemoryStore

// New returns a store bounding each key to perMinute requests.
//
// The key is whatever the caller is counting: the account id after
// authentication, the remote address before it.
//
// Burst is a full minute's worth: a client that authenticates and then makes
// several calls in quick succession is normal, and shaping that into a trickle
// would make the server feel broken.
//
// A non-positive rate falls back to DefaultPerMinute rather than denying
// everything: a configuration mistake should not be indistinguishable from an
// outage.
func New(perMinute int) *Store {
	if perMinute <= 0 {
		perMinute = DefaultPerMinute
	}
	return echomw.NewRateLimiterMemoryStoreWithConfig(echomw.RateLimiterMemoryStoreConfig{
		Rate:      float64(perMinute) / 60,
		Burst:     perMinute,
		ExpiresIn: ttl,
	})
}
