// Package strava imports a person's recorded activities from Strava into
// North's own activity log.
//
// It lives under internal/fitness because that package already claims
// provider integrations (see its doc comment); this is the first one.
//
// The shape of the integration:
//
//   - OAuth connects one Strava athlete to one North user. It is not a way
//     to sign in — disconnecting Strava must never affect someone's ability
//     to log in, which is why this does not touch auth_identities.
//   - Sync is triggered by an event (connecting) or by a person (a Sync now
//     button), never by a timer. North has no scheduler, and adding one for
//     this would be a larger decision than the feature warrants.
//   - Imported sessions land in activity_sessions with source='strava' and
//     the Strava activity id in external_id, where the existing
//     UNIQUE (source, external_id) index makes re-importing a no-op.
//
// Tokens are secrets: they are never logged, never rendered, and never
// included in a wrapped error.
package strava

import "time"

// Connection is one linked Strava account.
//
// The token fields are unexported-by-convention in every direction that
// matters: nothing outside this package reads them, and Status is what the
// UI is given instead.
type Connection struct {
	UserID       string
	AthleteID    int64
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Scopes       string
	LastSyncedAt *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Expired reports whether the access token needs refreshing before use. The
// minute of slack avoids racing a token that expires mid-request.
func (c Connection) Expired(now time.Time) bool {
	return !c.ExpiresAt.After(now.Add(time.Minute))
}

// Status is the connection as the UI is allowed to see it: whether it exists
// and when it last ran, with nothing secret in it.
type Status struct {
	Configured   bool
	Connected    bool
	AthleteID    int64
	LastSyncedAt *time.Time
}

// SyncResult reports what one sync did, for the flash message and the logs.
type SyncResult struct {
	Fetched  int
	Imported int

	// Unmapped counts activities whose sport this package has no mapping
	// for. They are still imported under a fallback code; this is how the
	// gap becomes visible rather than silent.
	Unmapped int
}
