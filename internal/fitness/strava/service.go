package strava

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/jobs"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// StateBytes is the entropy in the OAuth state parameter, exported so the
// handler that mints the cookie and this package agree on one number.
const StateBytes = 32

// BiometricsLookup is this package's view of biometrics: enough to cost an
// imported session, and no more. Satisfied directly by *biometrics.Service,
// the same arrangement internal/activity uses.
type BiometricsLookup interface {
	Current(ctx context.Context, userID uuid.UUID) (biometrics.Biometric, error)
}

// Enqueuer is the queue as this package needs it, so a sync can be handed to
// the worker without importing the whole jobs surface.
//
// HasPendingJobForUser is here for Status, not for enqueueing: a sync that was
// queued and never claimed is the failure this integration actually suffered,
// and it is invisible in the connection row because nothing ever wrote to it.
type Enqueuer interface {
	Enqueue(ctx context.Context, kind jobs.Kind, payload any) (jobs.Job, error)
	HasPendingJobForUser(ctx context.Context, kind jobs.Kind, userID uuid.UUID) (bool, error)
}

type Service struct {
	repo       *Repository
	oauth      *oauthConfig
	activity   *activity.Service
	biometrics BiometricsLookup
	queue      Enqueuer

	// funnel records the connection as a product event. Nil is a no-op.
	funnel *analytics.Funnel
}

// WithFunnel attaches product analytics.
func (s *Service) WithFunnel(f *analytics.Funnel) *Service {
	s.funnel = f
	return s
}

type Options struct {
	Repository   *Repository
	Activity     *activity.Service
	Biometrics   BiometricsLookup
	Queue        Enqueuer
	ClientID     string
	ClientSecret string
	BaseURL      string
}

func NewService(opts Options) *Service {
	return &Service{
		repo:       opts.Repository,
		oauth:      newOAuth(opts.ClientID, opts.ClientSecret, opts.BaseURL),
		activity:   opts.Activity,
		biometrics: opts.Biometrics,
		queue:      opts.Queue,
	}
}

// Configured reports whether Strava credentials were supplied. False is a
// normal local-development state, not an error: the UI says so and every
// other route keeps working.
func (s *Service) Configured() bool { return s.oauth.enabled() }

// NewState mints the opaque CSRF state for an authorization request.
func NewState() (string, error) {
	buf := make([]byte, StateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", apperr.Wrap(err, "generate oauth state")
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Service) AuthCodeURL(state string) (string, error) {
	if !s.Configured() {
		return "", apperr.New("strava is not configured")
	}
	return s.oauth.authCodeURL(state), nil
}

// Status is what the UI is allowed to know: connected or not, and when it
// last ran. Never the tokens.
func (s *Service) Status(ctx context.Context, userID uuid.UUID) (Status, error) {
	out := Status{Configured: s.Configured()}
	if !out.Configured {
		return out, nil
	}

	conn, err := s.repo.Get(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return out, nil
		}
		return Status{}, err
	}

	out.Connected = true
	out.AthleteID = conn.AthleteID
	out.LastSyncedAt = conn.LastSyncedAt
	out.LastSyncError = conn.LastSyncError
	out.LastSyncAttemptedAt = conn.LastSyncAttemptedAt

	// A queued sync is worth saying out loud. Without it, "we asked and
	// nothing has run it" is indistinguishable from "we never asked", which
	// is the state a real account sat in for eleven days.
	if s.queue != nil {
		pending, err := s.queue.HasPendingJobForUser(ctx, jobs.KindSyncStrava, userID)
		if err != nil {
			// Not worth failing the card over: the rest of the status is
			// already correct without it.
			slog.Default().Warn("could not check for a queued strava sync",
				slog.Any("error", err), slog.String("user_id", userID.String()))
		}
		out.SyncPending = pending
	}
	return out, nil
}

// Connect exchanges the authorization code and stores the connection, then
// queues a first sync so the athlete sees their data without a second click.
func (s *Service) Connect(ctx context.Context, userID uuid.UUID, code string) error {
	if !s.Configured() {
		return apperr.New("strava is not configured")
	}
	if strings.TrimSpace(code) == "" {
		return apperr.Wrap(apperr.ErrValidation, "missing authorization code")
	}

	token, err := s.oauth.exchange(ctx, code)
	if err != nil {
		return err
	}
	if token.Athlete.ID == 0 {
		return apperr.New("strava: token response was missing the athlete")
	}

	if _, err := s.repo.Upsert(ctx, userID, token.Athlete.ID,
		token.AccessToken, token.RefreshToken, time.Unix(token.ExpiresAt, 0), strings.Join(s.oauth.cfg.Scopes, ",")); err != nil {
		return err
	}

	// The first real signal in the funnel: nothing about North works before a
	// source exists, so this is the step people drop at.
	s.funnel.SourceConnected(ctx, userID, analytics.SourceStrava)

	s.queueSync(ctx, userID)
	return nil
}

func (s *Service) Disconnect(ctx context.Context, userID uuid.UUID) error {
	return s.repo.Delete(ctx, userID)
}

// RecentActivities is what the 3D view draws. Reads from North's own copy
// rather than calling Strava, so opening the page is fast, works when Strava
// is down, and costs nothing against the rate limit.
func (s *Service) RecentActivities(ctx context.Context, userID uuid.UUID, limit int) ([]Activity, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	return s.repo.RecentActivities(ctx, userID, limit)
}

// RouteTotals is the distance and climb recorded over a window, for the coach's
// weekly training picture. Like RecentActivities it reads North's own copy, so
// it costs nothing against the rate limit and keeps working while Strava is
// down — the coach never blocks on a third party to describe someone's week.
//
// This is what satisfies activity.RouteLookup. The signature is fixed by that
// interface, which the activity slice declares because strava already imports
// it and the reverse would be an import cycle.
func (s *Service) RouteTotals(ctx context.Context, userID uuid.UUID, since, until time.Time) (activity.RouteTotals, error) {
	return s.repo.RouteTotals(ctx, userID, since, until)
}

// RequestSync queues a sync rather than running it inline, so a click
// returns immediately and a slow or rate-limited Strava never holds a
// request open.
func (s *Service) RequestSync(ctx context.Context, userID uuid.UUID) error {
	if _, err := s.repo.Get(ctx, userID); err != nil {
		return err
	}
	s.queueSync(ctx, userID)
	return nil
}

// queueSync is best-effort: a failure to enqueue should not fail the connect
// that just succeeded, since the person can always press Sync now.
func (s *Service) queueSync(ctx context.Context, userID uuid.UUID) {
	if s.queue == nil {
		return
	}
	if _, err := s.queue.Enqueue(ctx, jobs.KindSyncStrava, jobs.SyncStravaPayload{UserID: userID}); err != nil {
		slog.Default().Warn("could not queue strava sync", slog.Any("error", err), slog.String("user_id", userID.String()))
	}
}

// Sync imports one page of recent activities.
//
// Bounded on purpose: Strava allows 100 reads per 15 minutes, and a sync
// that walked an athlete's whole history would spend that budget on data
// nobody asked for. The window starts from the last successful sync, or a
// fixed backfill for the first one.
func (s *Service) Sync(ctx context.Context, userID uuid.UUID) (SyncResult, error) {
	// Checked here as well as in Connect because the two run in different
	// processes. A worker deployment missing the credentials the web one has
	// would otherwise reach refresh with a nil oauth config and panic, which
	// the job runner turns into a bare stack trace nobody can act on.
	if !s.Configured() {
		return SyncResult{}, apperr.New("strava is not configured")
	}

	conn, err := s.repo.Get(ctx, userID)
	if err != nil {
		return SyncResult{}, err
	}

	conn, err = s.ensureFreshToken(ctx, conn)
	if err != nil {
		return SyncResult{}, err
	}

	after := time.Now().Add(-firstSyncLookback)
	if conn.LastSyncedAt != nil && conn.LastSyncedAt.After(after) {
		after = *conn.LastSyncedAt
	}

	remote, err := fetchActivities(ctx, conn.AccessToken, after)
	if err != nil {
		return SyncResult{}, err
	}

	weight, err := s.currentWeight(ctx, userID)
	if err != nil {
		return SyncResult{}, err
	}

	result := SyncResult{Fetched: len(remote)}
	for _, a := range remote {
		imported, mapped, err := s.importOne(ctx, userID, a, weight)
		if err != nil {
			// One malformed activity should not abandon the rest of the
			// page: the sync is a best-effort catch-up, not a transaction.
			slog.Default().Warn("skipped a strava activity",
				slog.Any("error", err), slog.Int64("activity_id", a.ID))
			continue
		}
		if imported {
			result.Imported++
		}
		if !mapped {
			result.Unmapped++
		}
	}

	// Stamped from the newest activity seen rather than "now", so activities
	// uploaded late (a watch synced hours after a run) are not skipped by
	// the next window.
	if err := s.repo.MarkSynced(ctx, userID, newestStart(remote, after)); err != nil {
		return result, err
	}

	return result, nil
}

func (s *Service) importOne(ctx context.Context, userID uuid.UUID, a apiActivity, weightKg float64) (imported, mapped bool, err error) {
	startedAt, err := a.startedAt()
	if err != nil {
		return false, false, err
	}

	// Strava's own record is saved unconditionally, before the normalised
	// import: it is what the 3D view draws, and it stays useful (and
	// correctable) even for an activity already imported, whose session row
	// the dedupe below will leave alone.
	if err = s.repo.SaveActivity(ctx, userID, Activity{
		StravaID:        a.ID,
		Name:            a.Name,
		SportType:       a.sport(),
		StartDate:       startedAt,
		DistanceM:       a.Distance,
		MovingTimeS:     a.MovingTime,
		ElapsedTimeS:    a.ElapsedTime,
		ElevationGainM:  a.TotalElevationGain,
		AverageSpeedMS:  a.AverageSpeed,
		SummaryPolyline: a.Map.SummaryPolyline,
	}); err != nil {
		return false, false, err
	}

	code, mapped := MapSportType(a.SportType, a.Type)

	_, imported, err = s.activity.Import(ctx, activity.ImportInput{
		UserID:       userID,
		ActivityCode: code,
		Source:       activity.SourceStrava,
		ExternalID:   externalID(a.ID),
		StartedAt:    startedAt,
		EndedAt:      startedAt.Add(a.duration()),
		WeightKg:     weightKg,
		Calories:     a.Calories,
	})
	if err != nil {
		return false, mapped, err
	}
	return imported, mapped, nil
}

// ensureFreshToken refreshes an expiring access token and persists the new
// pair. Strava rotates the refresh token on every refresh, so storing the
// result is not optional.
func (s *Service) ensureFreshToken(ctx context.Context, conn Connection) (Connection, error) {
	if !conn.Expired(time.Now()) {
		return conn, nil
	}

	token, err := s.oauth.refresh(ctx, conn.RefreshToken)
	if err != nil {
		return Connection{}, err
	}

	userID, err := uuid.Parse(conn.UserID)
	if err != nil {
		return Connection{}, apperr.Wrap(err, "parse connection user id")
	}

	return s.repo.UpdateTokens(ctx, userID, token.AccessToken, token.RefreshToken, time.Unix(token.ExpiresAt, 0))
}

// currentWeight is needed to cost a session when Strava has no calorie figure
// of its own — which is most of them, since the list endpoint omits calories.
//
// Absent biometrics is not fatal: the estimate is skipped and the session is
// imported with calories unknown (NULL), rather than blocking the import of a
// run that genuinely happened. Recording biometrics later does not backfill
// these, which is the honest trade for not inventing a number.
func (s *Service) currentWeight(ctx context.Context, userID uuid.UUID) (float64, error) {
	bio, err := s.biometrics.Current(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return bio.WeightKg, nil
}

func newestStart(activities []apiActivity, fallback time.Time) time.Time {
	newest := fallback
	for _, a := range activities {
		if started, err := a.startedAt(); err == nil && started.After(newest) {
			newest = started
		}
	}
	return newest
}

// externalID is Strava's activity id as text, which is what
// activity_sessions.external_id holds and what the dedupe index compares.
func externalID(id int64) string { return strconv.FormatInt(id, 10) }

// HandleSyncJob is the worker entry point for jobs.KindSyncStrava.
func (s *Service) HandleSyncJob(ctx context.Context, userID uuid.UUID) error {
	result, err := s.Sync(ctx, userID)
	if err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			// Disconnected between enqueue and run. Nothing to do, and
			// retrying will not help.
			return nil
		}

		// Recorded before the error is returned, so the reason is on screen
		// while the job is still working through its retries rather than only
		// after the last one has failed.
		//
		// A failure to record must not mask the failure being recorded: it is
		// logged and the original error is what the job runner sees.
		if markErr := s.repo.MarkSyncFailed(ctx, userID, err.Error()); markErr != nil {
			slog.Default().Error("could not record strava sync failure",
				slog.Any("error", markErr), slog.String("user_id", userID.String()))
		}
		return err
	}

	slog.Default().Info("strava sync finished",
		slog.String("user_id", userID.String()),
		slog.Int("fetched", result.Fetched),
		slog.Int("imported", result.Imported),
		slog.Int("unmapped", result.Unmapped))
	return nil
}
