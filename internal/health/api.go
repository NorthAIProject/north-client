package health

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// SourceAppleHealth is the source the native app writes under. The app reads
// Apple Health on the phone and speaks for it; there is no other way in (see
// the package note).
const SourceAppleHealth = "apple_health"

// maxSyncBytes bounds one sync from the phone, and matches the JSON group's
// cap in cmd/api. The phone sends daily aggregates, a few kilobytes a sync;
// raw per-beat samples, which would need more, go to /ingest/health instead.
const maxSyncBytes = 1 << 20

// API is health ingest for the native app: the same Ingest and IngestWorkouts
// the /ingest/health bridge endpoint uses, behind the app's session token
// rather than a connection token.
type API struct {
	svc *Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.POST("/health/samples", a.sync)
	g.DELETE("/health/samples", a.forget)
	g.POST("/health/blood-pressure", a.bloodPressure)
}

type SampleReading struct {
	// Metric is one of the names the coach summarises, e.g. resting_heart_rate,
	// hrv_sdnn, steps, active_calories, sleep_asleep.
	Metric string  `json:"metric"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	// StartedAt alone is an instantaneous sample; with EndedAt, an interval
	// such as a day's steps or a block of sleep.
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

type SampleWorkout struct {
	// ActivityCode is North's activity code, translated on the phone from the
	// Apple Health workout type.
	ActivityCode string `json:"activityCode"`
	// ExternalID is Apple Health's UUID for the workout; replaying it is a
	// no-op.
	ExternalID string    `json:"externalId"`
	StartedAt  time.Time `json:"startedAt"`
	EndedAt    time.Time `json:"endedAt"`
	// Calories is the device's active energy; zero means unknown.
	Calories float64 `json:"calories,omitempty"`
	// What the watch measured; zero or absent means unknown.
	DistanceM    float64 `json:"distanceM,omitempty"`
	AvgHeartRate float64 `json:"avgHeartRate,omitempty"`
	MaxHeartRate float64 `json:"maxHeartRate,omitempty"`
	ElevationM   float64 `json:"elevationM,omitempty"`
	// Indoor is Apple Health's indoor-workout flag; absent when not set.
	Indoor *bool `json:"indoor,omitempty"`
}

type SyncRequest struct {
	Readings []SampleReading `json:"readings"`
	Workouts []SampleWorkout `json:"workouts"`
}

type SyncResult struct {
	// Readings is how many readings were stored.
	Readings int `json:"readings"`
	// Workouts counts workouts delivered, including ones already stored and
	// ones recognised as a workout another provider already sent.
	Workouts int `json:"workouts"`
}

func (a *API) sync(c *echo.Context) error {
	var req SyncRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: maxSyncBytes}, "The health data could not be read."); err != nil {
		return err
	}
	if len(req.Readings) == 0 && len(req.Workouts) == 0 {
		return httpx.Fail(apperr.Wrap(apperr.ErrValidation, "empty sync"), "There was nothing to sync.")
	}

	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	var out SyncResult

	if len(req.Readings) > 0 {
		readings := make([]Reading, 0, len(req.Readings))
		for _, in := range req.Readings {
			readings = append(readings, Reading(in))
		}
		result, err := a.svc.Ingest(ctx, userID, SourceAppleHealth, readings)
		if err != nil {
			return httpx.Fail(err, "The health data could not be saved.")
		}
		out.Readings = result.Written
	}

	if len(req.Workouts) > 0 {
		workouts := make([]Workout, 0, len(req.Workouts))
		for _, in := range req.Workouts {
			workouts = append(workouts, Workout(in))
		}
		result, err := a.svc.IngestWorkouts(ctx, userID, SourceAppleHealth, workouts)
		if err != nil {
			return httpx.Fail(err, "The workouts could not be saved.")
		}
		out.Workouts = result.Written
	}

	return c.JSON(http.StatusOK, out)
}

// forget deletes every reading Apple Health sent, on the person's request.
// Workouts already in their activity history stay: those are sessions they
// did, and removing one is its own decision on the activity page.
func (a *API) forget(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := a.svc.Forget(ctx, auth.MustUser(ctx).ID, SourceAppleHealth); err != nil {
		return httpx.Fail(err, "The health data could not be removed.")
	}
	return c.NoContent(http.StatusNoContent)
}

// SourceManual is what a reading typed into North is stored under, so it never
// collides with, or is forgotten along with, what a device reported.
const SourceManual = "manual"

// Metric names for blood pressure, one reading each per measurement.
const (
	MetricSystolic  = "bp_systolic"
	MetricDiastolic = "bp_diastolic"
)

type BloodPressureRequest struct {
	Systolic  int        `json:"systolic"`
	Diastolic int        `json:"diastolic"`
	At        *time.Time `json:"at,omitempty"`
}

// bloodPressure records a cuff reading typed in by hand. The phone syncs its
// own readings through /health/samples; this is for everyone else.
func (a *API) bloodPressure(c *echo.Context) error {
	var req BloodPressureRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	res, err := a.svc.RecordBloodPressure(ctx, auth.MustUser(ctx).ID, req.Systolic, req.Diastolic, req.At)
	if err != nil {
		return httpx.Fail(err, "The reading could not be saved.")
	}
	return c.JSON(http.StatusCreated, SyncResult{Readings: res.Written})
}
