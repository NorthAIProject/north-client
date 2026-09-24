package health

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// SourceAppleHealth is the source the native app writes under. The app reads
// Apple Health on the phone and speaks for it; there is no other way in (see
// the package note).
const SourceAppleHealth = "apple_health"

// maxSyncBytes bounds one sync from the phone, and matches the JSON group's
// cap in cmd/web. The phone sends daily aggregates, a few kilobytes a sync;
// raw per-beat samples, which would need more, go to /ingest/health instead.
const maxSyncBytes = 1 << 20

// API is health ingest for the native app: the same Ingest and IngestWorkouts
// the /ingest/health bridge endpoint uses, behind the app's session token
// rather than a connection token.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Post("/health/samples", a.sync)
	r.Delete("/health/samples", a.forget)
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

func (a *API) sync(w http.ResponseWriter, r *http.Request) {
	var req SyncRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: maxSyncBytes}); err != nil {
		httpx.Error(w, err, "The health data could not be read.")
		return
	}
	if len(req.Readings) == 0 && len(req.Workouts) == 0 {
		httpx.Error(w, apperr.Wrap(apperr.ErrValidation, "empty sync"), "There was nothing to sync.")
		return
	}

	userID := auth.MustUser(r.Context()).ID
	var out SyncResult

	if len(req.Readings) > 0 {
		readings := make([]Reading, 0, len(req.Readings))
		for _, in := range req.Readings {
			readings = append(readings, Reading(in))
		}
		result, err := a.svc.Ingest(r.Context(), userID, SourceAppleHealth, readings)
		if err != nil {
			httpx.Error(w, err, "The health data could not be saved.")
			return
		}
		out.Readings = result.Written
	}

	if len(req.Workouts) > 0 {
		workouts := make([]Workout, 0, len(req.Workouts))
		for _, in := range req.Workouts {
			workouts = append(workouts, Workout(in))
		}
		result, err := a.svc.IngestWorkouts(r.Context(), userID, SourceAppleHealth, workouts)
		if err != nil {
			httpx.Error(w, err, "The workouts could not be saved.")
			return
		}
		out.Workouts = result.Written
	}

	httpx.WriteJSON(w, http.StatusOK, out)
}

// forget deletes every reading Apple Health sent, on the person's request.
// Workouts already in their activity history stay: those are sessions they
// did, and removing one is its own decision on the activity page.
func (a *API) forget(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Forget(r.Context(), auth.MustUser(r.Context()).ID, SourceAppleHealth); err != nil {
		httpx.Error(w, err, "The health data could not be removed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
