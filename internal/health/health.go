// Package health owns scalar health readings — heart rate, HRV, VO2max, SpO2,
// sleep stages, step counts, body composition — however they arrive.
//
// It exists because Apple Health and Google Health Connect have no server API
// and never have. Both are on-device frameworks, so no amount of OAuth reaches
// them from a web app; the data has to be pushed from the phone. That makes
// this package's job ingest rather than sync: North does not fetch here, it
// receives.
//
// A provider is named by a string rather than an enum on purpose. The lesson is
// activity_sessions.source, whose CHECK constraint means a new provider cannot
// write a single row until someone ships a migration.
//
// TODO: nothing pushes to this package yet. The endpoint, the schema and the
// coach summary are all in place and tested, but no real device has ever sent
// a payload, so every number this package has stored so far was written by a
// test or by hand.
//
// Closing that is not a code change here. Apple Health can only be read by
// code running on the phone, so it needs one of:
//
//   - an off-the-shelf bridge app (Health Auto Export and similar) that a
//     person configures with a token and this endpoint's URL — no code, and
//     the fastest way to find out whether any of this is worth keeping; or
//   - a native iOS client, which is a separate product surface, not a package.
//
// Android has no equivalent: Health Connect is on-device only and Google Fit's
// REST API is being turned off, so there is no cheap path there at all.
//
// Until one of those exists, treat the coverage here as describing behaviour
// that has never met a real payload. Field names, units and timestamp formats
// are guesses at what a bridge sends, and the first real sync is likely to
// correct some of them.
package health

import (
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// MetricSleepAsleep is the total time a device recorded a person as asleep,
// in minutes.
//
// A named constant, unlike the metric names in summary.go, because this is the
// one metric two packages have to agree on rather than merely render: the
// sleep-truth comparison reads it and the simulator writes it, and a typo on
// either side would show up as "no bias detected" rather than as an error.
//
// The name itself is still a guess, per the package note above — no real bridge
// payload has arrived yet. When the first one does and it disagrees, this
// constant is the single place to correct.
const MetricSleepAsleep = "sleep_asleep"

// Reading is one measurement arriving from a provider.
//
// The unit travels with the value rather than being assumed from the metric
// name: the same metric arrives in different units from different devices, and
// normalising on write would throw away the provider's own answer with nothing
// to check it against later.
type Reading struct {
	Metric    string
	Value     float64
	Unit      string
	StartedAt time.Time

	// EndedAt is nil for an instantaneous sample — a single heart-rate beat —
	// and set for an interval, such as a deep-sleep block or a day's steps.
	EndedAt *time.Time
}

// Stored is a reading that has been persisted, with the source it came from.
type Stored struct {
	ID        uuid.UUID
	Source    string
	Metric    string
	Value     float64
	Unit      string
	StartedAt time.Time
	EndedAt   *time.Time
}

// Result reports what an ingest did.
type Result struct {
	// Written counts rows inserted or corrected. A replayed payload reports the
	// same count as its first delivery, because the bridge cannot tell the
	// difference and does not need to.
	Written int
}

// maxReadings bounds one payload.
//
// A phone syncing a week of per-beat heart rate genuinely produces tens of
// thousands of samples, so this is not small. It exists because the body is
// decoded into memory before any of it is written, and an unbounded payload
// would let one caller decide how much memory North spends.
const maxReadings = 50_000

// validate checks a whole payload before any of it is written.
//
// Rejecting the batch rather than skipping bad rows is deliberate: a partially
// applied sync leaves a gap that neither side can see, and the bridge has no
// way to learn which half to send again.
func validate(source string, readings []Reading) error {
	if source == "" {
		return apperr.Wrap(apperr.ErrValidation, "a reading needs a source")
	}
	if len(readings) == 0 {
		return apperr.Wrap(apperr.ErrValidation, "no readings")
	}
	if len(readings) > maxReadings {
		return apperr.Wrap(apperr.ErrValidation, "too many readings: %d, max %d", len(readings), maxReadings)
	}

	for i, r := range readings {
		if r.Metric == "" {
			return apperr.Wrap(apperr.ErrValidation, "reading %d has no metric", i)
		}
		if r.Unit == "" {
			return apperr.Wrap(apperr.ErrValidation, "reading %d (%s) has no unit", i, r.Metric)
		}
		if r.StartedAt.IsZero() {
			return apperr.Wrap(apperr.ErrValidation, "reading %d (%s) has no start time", i, r.Metric)
		}
		if r.EndedAt != nil && !r.EndedAt.After(r.StartedAt) {
			return apperr.Wrap(apperr.ErrValidation,
				"reading %d (%s) ends at or before it starts", i, r.Metric)
		}
	}
	return nil
}
