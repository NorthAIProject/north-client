package progresschecks

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/biometrics/biometric"
	"github.com/NorthAIProject/north-client/internal/bodymap"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Photos stores and removes the private photo files. media.Service satisfies it.
type Photos interface {
	UploadProgressPhoto(ctx context.Context, userID uuid.UUID, filename string, size int64, body io.Reader) (media.Media, error)
	DeleteOwned(ctx context.Context, userID, id uuid.UUID) error
	SignedURL(ctx context.Context, userID, id uuid.UUID) (string, error)
	ReadBytes(ctx context.Context, userID, mediaID uuid.UUID) (string, []byte, error)
}

// Weights is the person's current measurement. biometrics.Service satisfies it.
type Weights interface {
	Current(ctx context.Context, userID uuid.UUID) (biometric.Biometric, error)
	RecordWeight(ctx context.Context, userID uuid.UUID, weightKg float64) (biometric.Biometric, error)
}

type Service struct {
	repo    *Repository
	photos  Photos
	weights Weights
	assist  *Assist
	now     func() time.Time
	awards  Awards
}

// Awards checks a person's badges after a write. awards.Service satisfies it.
type Awards interface {
	Check(ctx context.Context, userID uuid.UUID, at time.Time, areas ...award.Area)
}

func (s *Service) WithAwards(a Awards) *Service {
	s.awards = a
	return s
}

func NewService(repo *Repository, photos Photos, weights Weights) *Service {
	return &Service{repo: repo, photos: photos, weights: weights, now: time.Now}
}

// WithClock fixes now for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// DefaultLimit and MaxLimit bound a list of checks.
const (
	DefaultLimit = 12
	MaxLimit     = 52
)

// PhotoUpload is one pose's file as it arrives.
type PhotoUpload struct {
	Pose     progresscheck.Pose
	Filename string
	Size     int64
	Body     io.Reader
}

// Save stores a check the person confirmed, with its photos.
//
// A weight given for today becomes their current weight too, so Today and the
// calculator follow; a back-dated one is history only. With no weight, the
// last known one is copied and marked carried.
func (s *Service) Save(ctx context.Context, user users.User, in Input, uploads []PhotoUpload) (Check, error) {
	today := s.today(user)
	clean, err := progresscheck.Validate(in, today)
	if err != nil {
		return Check{}, err
	}
	if err = distinctPoses(uploads); err != nil {
		return Check{}, err
	}
	if clean.Empty() && len(uploads) == 0 {
		return Check{}, apperr.FieldErrors{}.Add("waist_cm", "Add a photo or at least one measurement.")
	}

	carried := false
	switch {
	case clean.WeightKg != nil && clean.MeasuredOn.Equal(today):
		if _, err = s.weights.RecordWeight(ctx, user.ID, *clean.WeightKg); err != nil && !apperr.Is(err, biometrics.ErrNoBaseline) {
			return Check{}, err
		}
	case clean.WeightKg == nil:
		b, cerr := s.weights.Current(ctx, user.ID)
		switch {
		case cerr == nil:
			w := b.WeightKg
			clean.WeightKg, carried = &w, true
		case !apperr.Is(cerr, apperr.ErrNotFound):
			return Check{}, cerr
		}
	}

	photos, err := s.store(ctx, user, uploads)
	if err != nil {
		return Check{}, err
	}
	check, err := s.repo.Create(ctx, user.ID, clean, carried, photos)
	if err != nil {
		s.discard(ctx, user, photos)
		return Check{}, err
	}

	if s.awards != nil {
		s.awards.Check(ctx, user.ID, s.now(), award.Body)
	}
	if err := s.advance(ctx, user, check.MeasuredOn); err != nil {
		return Check{}, err
	}
	return check, nil
}

// store uploads each pose, removing what it stored if a later one fails.
func (s *Service) store(ctx context.Context, user users.User, uploads []PhotoUpload) ([]Photo, error) {
	photos := make([]Photo, 0, len(uploads))
	for _, u := range uploads {
		m, err := s.photos.UploadProgressPhoto(ctx, user.ID, u.Filename, u.Size, u.Body)
		if err != nil {
			s.discard(ctx, user, photos)
			return nil, renameField(err, string(u.Pose))
		}
		photos = append(photos, Photo{Pose: u.Pose, MediaID: m.ID})
	}
	return photos, nil
}

// discard removes photos stored for a check that was not saved. Best effort:
// the save already failed, and that is the error the caller reports.
func (s *Service) discard(ctx context.Context, user users.User, photos []Photo) {
	for _, p := range photos {
		_ = s.photos.DeleteOwned(ctx, user.ID, p.MediaID)
	}
}

// advance moves the next due date on from a check, never back: a check
// back-dated to last month does not make this week's one due early.
func (s *Service) advance(ctx context.Context, user users.User, measuredOn time.Time) error {
	sched, err := s.repo.Schedule(ctx, user.ID)
	if err != nil || sched.Cadence == "" {
		return err
	}
	next := sched.Cadence.Next(measuredOn)
	if !next.After(sched.NextDueOn) {
		return nil
	}
	sched.NextDueOn = next
	_, err = s.repo.SetSchedule(ctx, user.ID, sched)
	return err
}

func (s *Service) List(ctx context.Context, user users.User, limit int) ([]Check, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return s.repo.List(ctx, user.ID, min(limit, MaxLimit))
}

func (s *Service) Get(ctx context.Context, user users.User, id uuid.UUID) (Check, error) {
	return s.repo.Get(ctx, id, user.ID)
}

// PhotoURL is a short-lived link to one of a check's photos.
func (s *Service) PhotoURL(ctx context.Context, user users.User, mediaID uuid.UUID) (string, error) {
	return s.photos.SignedURL(ctx, user.ID, mediaID)
}

// allChecks bounds History; years of weekly checks fit well inside it.
const allChecks = 5000

// History is every check, newest first: what an export needs.
func (s *Service) History(ctx context.Context, user users.User) ([]Check, error) {
	return s.repo.List(ctx, user.ID, allChecks)
}

// PhotoBytes reads one of a check's photos back, for an export.
func (s *Service) PhotoBytes(ctx context.Context, user users.User, mediaID uuid.UUID) (string, []byte, error) {
	return s.photos.ReadBytes(ctx, user.ID, mediaID)
}

// Latest is the newest check, if there is one.
func (s *Service) Latest(ctx context.Context, user users.User) (Check, bool, error) {
	checks, err := s.repo.List(ctx, user.ID, 1)
	if err != nil || len(checks) == 0 {
		return Check{}, false, err
	}
	return checks[0], true, nil
}

// Delete removes a check and its photos, objects included.
func (s *Service) Delete(ctx context.Context, user users.User, id uuid.UUID) error {
	check, err := s.repo.Get(ctx, id, user.ID)
	if err != nil {
		return err
	}
	for _, p := range check.Photos {
		if err := s.photos.DeleteOwned(ctx, user.ID, p.MediaID); err != nil && !apperr.Is(err, apperr.ErrNotFound) {
			return err
		}
	}
	return s.repo.Delete(ctx, id, user.ID)
}

// Schedule is the person's cadence; the zero Schedule when none is set.
func (s *Service) Schedule(ctx context.Context, user users.User) (Schedule, error) {
	return s.repo.Schedule(ctx, user.ID)
}

// SetCadence chooses how often to check. The next one falls a cadence after
// the latest check, or today when there is none yet.
func (s *Service) SetCadence(ctx context.Context, user users.User, cadence progresscheck.Cadence) (Schedule, error) {
	next := s.today(user)
	latest, ok, err := s.Latest(ctx, user)
	if err != nil {
		return Schedule{}, err
	}
	if ok {
		next = cadence.Next(latest.MeasuredOn)
	}
	return s.repo.SetSchedule(ctx, user.ID, Schedule{Cadence: cadence, NextDueOn: next})
}

// StopSchedule turns the reminder off. The checks stay.
func (s *Service) StopSchedule(ctx context.Context, user users.User) error {
	return s.repo.ClearSchedule(ctx, user.ID)
}

// Due reports whether a check is due for this person on the local date of now.
func (s *Service) Due(ctx context.Context, user users.User, now time.Time) (Schedule, bool, error) {
	sched, err := s.repo.Schedule(ctx, user.ID)
	if err != nil {
		return Schedule{}, false, err
	}
	return sched, sched.Due(localDate(user, now)), nil
}

// Body is what the figure is drawn from: the latest check, if any, with the
// weight and height it is read against.
type Body struct {
	Check    *Check
	WeightKg *float64
	BMI      *float64
	Shape    bodymap.Shape
}

// Body works out the figure's shape from the latest check. Weight is the
// check's own when it has one, otherwise the current measurement's; height
// only ever comes from the measurement.
func (s *Service) Body(ctx context.Context, user users.User) (Body, error) {
	var out Body
	latest, ok, err := s.Latest(ctx, user)
	if err != nil {
		return Body{}, err
	}
	if ok {
		out.Check = &latest
		out.WeightKg = latest.WeightKg
	}

	var heightCm float64
	b, err := s.weights.Current(ctx, user.ID)
	switch {
	case err == nil:
		heightCm = b.HeightCm
		if out.WeightKg == nil || (out.Check != nil && out.Check.WeightCarried && b.CreatedAt.After(out.Check.CreatedAt)) {
			w := b.WeightKg
			out.WeightKg = &w
		}
	case !apperr.Is(err, apperr.ErrNotFound):
		return Body{}, err
	}

	var bmi float64
	if out.WeightKg != nil && heightCm > 0 {
		m := heightCm / 100
		bmi = *out.WeightKg / (m * m)
		rounded := float64(int(bmi*10+0.5)) / 10
		out.BMI = &rounded
	}
	if out.Check != nil {
		out.Shape = progresscheck.Shape(out.Check.Measurements, heightCm, bmi, bodymap.ShellBaseline)
	}
	return out, nil
}

func (s *Service) today(user users.User) time.Time { return localDate(user, s.now()) }

// localDate is the person's calendar date at t, as midnight UTC: the form a
// Postgres date comes back in, so the two compare directly.
func localDate(user users.User, t time.Time) time.Time {
	y, m, d := t.In(user.Location()).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func distinctPoses(uploads []PhotoUpload) error {
	seen := map[progresscheck.Pose]bool{}
	for _, u := range uploads {
		if seen[u.Pose] {
			return apperr.FieldErrors{}.Add(string(u.Pose), "One photo per pose.")
		}
		seen[u.Pose] = true
	}
	return nil
}

// renameField points media's "photo" field error at the pose it came from.
func renameField(err error, field string) error {
	var fe apperr.FieldErrors
	if !apperr.As(err, &fe) {
		return err
	}
	out := make(apperr.FieldErrors, len(fe))
	for i, e := range fe {
		e.Field = field
		out[i] = e
	}
	return out
}

// Shape is the figure's shape from the latest check, the same four numbers
// /body/current carries, for a page that draws the figure itself.
func (s *Service) Shape(ctx context.Context, user users.User) (bodymap.Shape, error) {
	b, err := s.Body(ctx, user)
	if err != nil {
		return bodymap.Shape{}, err
	}
	return b.Shape, nil
}
