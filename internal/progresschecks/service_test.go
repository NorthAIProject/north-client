package progresschecks_test

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/progresschecks"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// 23:30 in Lisbon on Thursday 15 October 2026 (UTC+1): still the 15th there,
// whatever UTC says.
var lateEvening = time.Date(2026, 10, 15, 22, 30, 0, 0, time.UTC)

type fixture struct {
	svc   *progresschecks.Service
	media *media.Service
	bio   *biometrics.Service
	store *memStorage
	user  users.User
	clock *time.Time
	ctx   context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "progress@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	store := &memStorage{objs: map[string][]byte{}}
	mediaSvc := media.NewService(media.Options{Repository: media.NewRepository(pool), Storage: store})
	bio := biometrics.NewService(biometrics.NewRepository(pool))
	clock := lateEvening
	svc := progresschecks.NewService(progresschecks.NewRepository(pool), mediaSvc, bio).
		WithClock(func() time.Time { return clock })
	return &fixture{svc: svc, media: mediaSvc, bio: bio, store: store, user: user, clock: &clock, ctx: context.Background()}
}

func ptr(v float64) *float64 { return &v }

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func jpeg(pose progresscheck.Pose) progresschecks.PhotoUpload {
	b := make([]byte, 64)
	copy(b, []byte{0xff, 0xd8, 0xff, 0xe0})
	return progresschecks.PhotoUpload{Pose: pose, Filename: string(pose) + ".jpg", Size: int64(len(b)), Body: bytes.NewReader(b)}
}

func (f *fixture) recordBaseline(t *testing.T) {
	t.Helper()
	if _, err := f.bio.Record(f.ctx, f.user.ID, biometrics.Input{
		WeightKg: 102, HeightCm: 185, DateOfBirth: date(1990, 1, 1), Sex: "male",
	}); err != nil {
		t.Fatalf("baseline: %v", err)
	}
}

func TestSaveStoresPhotosAndUpdatesTodaysWeight(t *testing.T) {
	f := newFixture(t)
	f.recordBaseline(t)

	check, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{
		Measurements: progresscheck.Measurements{WeightKg: ptr(100), WaistCm: ptr(96)},
	}, []progresschecks.PhotoUpload{jpeg(progresscheck.Side), jpeg(progresscheck.Front)})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !check.MeasuredOn.Equal(date(2026, 10, 15)) {
		t.Errorf("measured on %s, want the local date 2026-10-15", check.MeasuredOn)
	}
	if !check.Complete() || len(f.store.objs) != 2 {
		t.Errorf("want front+side stored, got %d photos, %d objects", len(check.Photos), len(f.store.objs))
	}

	got, err := f.svc.Get(f.ctx, f.user, check.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Photos[0].Pose != progresscheck.Front || got.Photos[1].Pose != progresscheck.Side {
		t.Errorf("photos not in pose order: %+v", got.Photos)
	}

	current, err := f.bio.Current(f.ctx, f.user.ID)
	if err != nil || current.WeightKg != 100 {
		t.Errorf("current weight = %v (%v), want 100", current.WeightKg, err)
	}

	// The stored file is a progress photo, never a chat photo.
	if _, ok, _ := f.media.LastImageAt(f.ctx, f.user.ID); ok {
		t.Error("a progress photo counted as a chat photo")
	}
}

func TestSaveCarriesTheLastWeightAndBackdatedWeightIsHistoryOnly(t *testing.T) {
	f := newFixture(t)
	f.recordBaseline(t)

	carried, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{
		Measurements: progresscheck.Measurements{WaistCm: ptr(97)},
	}, nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !carried.WeightCarried || carried.WeightKg == nil || *carried.WeightKg != 102 {
		t.Errorf("want carried 102 kg, got %+v", carried)
	}

	if _, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{
		MeasuredOn:   date(2026, 10, 1),
		Measurements: progresscheck.Measurements{WeightKg: ptr(90)},
	}, nil); err != nil {
		t.Fatalf("save back-dated: %v", err)
	}
	current, _ := f.bio.Current(f.ctx, f.user.ID)
	if current.WeightKg != 102 {
		t.Errorf("a back-dated weight changed the current one to %v", current.WeightKg)
	}
}

func TestSaveRefusesNothingAndDuplicatePoses(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{}, nil); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("empty check: err = %v, want validation", err)
	}
	_, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{}, []progresschecks.PhotoUpload{jpeg(progresscheck.Front), jpeg(progresscheck.Front)})
	if !apperr.Is(err, apperr.ErrValidation) || len(f.store.objs) != 0 {
		t.Errorf("duplicate pose: err = %v, %d objects stored", err, len(f.store.objs))
	}
}

func TestSaveRemovesStoredPhotosWhenALaterOneIsRefused(t *testing.T) {
	f := newFixture(t)
	bad := progresschecks.PhotoUpload{Pose: progresscheck.Side, Filename: "side.txt", Size: 5, Body: bytes.NewReader([]byte("hello"))}
	_, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{}, []progresschecks.PhotoUpload{jpeg(progresscheck.Front), bad})
	var fe apperr.FieldErrors
	if !apperr.As(err, &fe) || fe.Messages()["side"] == "" {
		t.Fatalf("want a field error on side, got %v", err)
	}
	if len(f.store.objs) != 0 {
		t.Errorf("front photo left behind: %d objects", len(f.store.objs))
	}
}

func TestDeleteRemovesPhotosAndObjects(t *testing.T) {
	f := newFixture(t)
	check, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{
		Measurements: progresscheck.Measurements{WaistCm: ptr(96)},
	}, []progresschecks.PhotoUpload{jpeg(progresscheck.Front), jpeg(progresscheck.Side), jpeg(progresscheck.Back)})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := f.svc.Delete(f.ctx, f.user, check.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(f.store.objs) != 0 {
		t.Errorf("%d objects left in storage", len(f.store.objs))
	}
	for _, p := range check.Photos {
		if _, err := f.media.GetMedia(f.ctx, p.MediaID, f.user.ID); !apperr.Is(err, apperr.ErrNotFound) {
			t.Errorf("media %s still there: %v", p.MediaID, err)
		}
	}
	if _, err := f.svc.Get(f.ctx, f.user, check.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("check still there: %v", err)
	}
}

func TestScheduleDueAndAdvance(t *testing.T) {
	f := newFixture(t)

	if _, due, _ := f.svc.Due(f.ctx, f.user, lateEvening); due {
		t.Fatal("due with no schedule")
	}
	sched, err := f.svc.SetCadence(f.ctx, f.user, progresscheck.Weekly)
	if err != nil {
		t.Fatalf("set cadence: %v", err)
	}
	if !sched.NextDueOn.Equal(date(2026, 10, 15)) {
		t.Errorf("first check due %s, want today", sched.NextDueOn)
	}
	if _, due, _ := f.svc.Due(f.ctx, f.user, lateEvening); !due {
		t.Error("not due on the local date it falls on")
	}

	if _, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{Measurements: progresscheck.Measurements{WaistCm: ptr(96)}}, nil); err != nil {
		t.Fatalf("save: %v", err)
	}
	sched, _, _ = f.svc.Due(f.ctx, f.user, lateEvening)
	if !sched.NextDueOn.Equal(date(2026, 10, 22)) {
		t.Errorf("next due %s, want 2026-10-22", sched.NextDueOn)
	}

	// A back-dated check never pulls the next one earlier.
	if _, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{
		MeasuredOn: date(2026, 9, 1), Measurements: progresscheck.Measurements{WaistCm: ptr(99)},
	}, nil); err != nil {
		t.Fatalf("save back-dated: %v", err)
	}
	sched, _, _ = f.svc.Due(f.ctx, f.user, lateEvening)
	if !sched.NextDueOn.Equal(date(2026, 10, 22)) {
		t.Errorf("back-dated check moved next due to %s", sched.NextDueOn)
	}

	sched, _ = f.svc.SetCadence(f.ctx, f.user, progresscheck.Monthly)
	if !sched.NextDueOn.Equal(date(2026, 11, 15)) {
		t.Errorf("monthly from the latest check: %s, want 2026-11-15", sched.NextDueOn)
	}
	if err := f.svc.StopSchedule(f.ctx, f.user); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if s, _ := f.svc.Schedule(f.ctx, f.user); s.Cadence != "" {
		t.Errorf("schedule still set: %+v", s)
	}
}

func TestBodyFromTheLatestCheck(t *testing.T) {
	f := newFixture(t)

	body, err := f.svc.Body(f.ctx, f.user)
	if err != nil || body.Check != nil || !body.Shape.Zero() {
		t.Fatalf("no checks: %+v, %v", body, err)
	}

	f.recordBaseline(t)
	if _, err = f.svc.Save(f.ctx, f.user, progresschecks.Input{
		Measurements: progresscheck.Measurements{WeightKg: ptr(100), WaistCm: ptr(110)},
	}, nil); err != nil {
		t.Fatalf("save: %v", err)
	}
	body, err = f.svc.Body(f.ctx, f.user)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	if body.Check == nil || body.BMI == nil || *body.BMI != 29.2 {
		t.Errorf("body = %+v bmi=%v, want BMI 29.2", body, body.BMI)
	}
	if body.Shape.Waist <= 0 || body.Shape.Overall <= 0 || body.Shape.Hips != 0 {
		t.Errorf("shape = %+v", body.Shape)
	}
}

type memStorage struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (m *memStorage) Put(_ context.Context, key, _ string, body io.Reader) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = b
	return nil
}

func (m *memStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

func (m *memStorage) SignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://storage.test/" + key, nil
}
