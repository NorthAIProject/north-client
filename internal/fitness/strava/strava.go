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

	// LastSyncError is empty on success and after a first connection. It is a
	// wrapped Go error, never a Strava response body, so it carries no token.
	LastSyncError       string
	LastSyncAttemptedAt *time.Time
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

	// LastSyncError and LastSyncAttemptedAt exist so a failed sync reads as a
	// failure. Without them a broken token, an unreachable Strava and a job
	// nobody ran all render identically to a connection that is simply new.
	LastSyncError       string
	LastSyncAttemptedAt *time.Time

	// SyncPending means a sync job is queued or running. A queued job that
	// nothing claims is what a stopped worker looks like from here, and it
	// used to be completely invisible.
	SyncPending bool

	// Unavailable means the connection could not be read at all — a decrypt
	// failure or a database error. Distinct from Connected: "we do not know"
	// must not be shown as "not connected", which invites a reconnect that
	// overwrites a working credential.
	Unavailable bool
}

// Activity is Strava's own view of one recorded activity.
//
// Kept distinct from activity.Session: that is North's normalised log, this
// is the provider's richer record, and the 3D view needs the parts North's
// own model has no reason to carry — a route, an elevation profile, the name
// the athlete gave it.
type Activity struct {
	StravaID  int64
	Name      string
	SportType string
	StartDate time.Time

	DistanceM      float64
	MovingTimeS    int
	ElapsedTimeS   int
	ElevationGainM float64
	AverageSpeedMS float64

	// SummaryPolyline is Google-encoded. Empty for anything without GPS,
	// which the viewer handles rather than hides.
	SummaryPolyline string
}

// HasRoute reports whether there is a path to draw.
func (a Activity) HasRoute() bool { return a.SummaryPolyline != "" }

// DistanceKm and Pace are display helpers, kept here so the template and the
// JSON handed to the viewer agree on the arithmetic.
func (a Activity) DistanceKm() float64 { return a.DistanceM / 1000 }

// PaceMinPerKm is minutes per kilometre, the unit runners actually think in.
// Zero distance means the question does not apply.
func (a Activity) PaceMinPerKm() float64 {
	if a.DistanceM <= 0 || a.MovingTimeS <= 0 {
		return 0
	}
	return (float64(a.MovingTimeS) / 60) / a.DistanceKm()
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
