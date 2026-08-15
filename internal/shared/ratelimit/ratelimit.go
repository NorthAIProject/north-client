// Package ratelimit holds the per-key request bound every bearer-authenticated
// surface applies.
//
// One module rather than a copy per endpoint, because these surfaces share the
// property that makes the bound necessary: they are reachable from the open
// internet with nothing but a token in front of them. A limit that only one of
// them enforces is not a limit — an attacker picks the other door.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
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

// Limiters holds one rate limiter per key, whatever the caller is counting.
//
// The key is a string so the same type serves both stages: the account id after
// authentication, and the remote address before it. Two near-identical maps
// differing only in key type would be a worse answer than one conversion.
type Limiters struct {
	mu        sync.Mutex
	perMinute int
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// New returns limiters bounding each key to perMinute requests.
//
// A non-positive rate falls back to DefaultPerMinute rather than denying
// everything: a configuration mistake should not be indistinguishable from an
// outage.
func New(perMinute int) *Limiters {
	if perMinute <= 0 {
		perMinute = DefaultPerMinute
	}
	return &Limiters{perMinute: perMinute, buckets: make(map[string]*bucket), lastSweep: time.Now()}
}

// Allow reports whether this key may make a request now.
func (l *Limiters) Allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Swept on access rather than on a ticker: a handler with no goroutine
	// behind it cannot leak one, and the only cost of sweeping late is a map
	// that is briefly larger than it needs to be.
	if now.Sub(l.lastSweep) > ttl {
		for k, b := range l.buckets {
			if now.Sub(b.lastSeen) > ttl {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}

	b, ok := l.buckets[key]
	if !ok {
		// Burst is a full minute's worth: a client that authenticates and then
		// makes several calls in quick succession is normal, and shaping that
		// into a trickle would make the server feel broken.
		b = &bucket{limiter: rate.NewLimiter(rate.Limit(float64(l.perMinute)/60.0), l.perMinute)}
		l.buckets[key] = b
	}
	b.lastSeen = now

	return b.limiter.Allow()
}
