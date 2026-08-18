package media_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

type memStorage struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func newMemStorage() *memStorage {
	return &memStorage{objs: map[string][]byte{}}
}

func (m *memStorage) Put(_ context.Context, key, _ string, body io.Reader) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = append([]byte(nil), b...)
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

func jpegBytes() []byte {
	b := make([]byte, 64)
	copy(b, []byte{0xff, 0xd8, 0xff, 0xe0})
	return b
}

func imageFixture(t *testing.T) (*media.Service, *users.Service, users.User, *memStorage) {
	t.Helper()
	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	store := newMemStorage()
	svc := media.NewService(media.Options{
		Repository: media.NewRepository(pool),
		Storage:    store,
	})
	return svc, userSvc, user, store
}

func TestUploadImageStoresAJPEG(t *testing.T) {
	svc, _, user, store := imageFixture(t)
	ctx := context.Background()
	body := jpegBytes()

	got, err := svc.UploadImage(ctx, user.ID, "squat.jpg", int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != media.KindImage || got.MIMEType != "image/jpeg" {
		t.Fatalf("stored %+v", got)
	}
	if got.OriginalName != "squat.jpg" {
		t.Fatalf("name = %q", got.OriginalName)
	}
	_ = store

	mime, data, err := svc.ReadBytes(ctx, user.ID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/jpeg" || !bytes.Equal(data, body) {
		t.Fatalf("read back mime=%q len=%d", mime, len(data))
	}
}

func TestUploadImageRejectsATextFile(t *testing.T) {
	svc, _, user, _ := imageFixture(t)
	body := []byte("this is not a photo")

	_, err := svc.UploadImage(context.Background(), user.ID, "notes.txt", int64(len(body)), bytes.NewReader(body))
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("got %v, want validation", err)
	}
	if !strings.Contains(err.Error(), "photo") {
		t.Fatalf("error should mention a photo: %v", err)
	}
}

func TestUploadImageRejectsOversized(t *testing.T) {
	svc, _, user, _ := imageFixture(t)
	size := media.MaxImageBytes + 1

	_, err := svc.UploadImage(context.Background(), user.ID, "huge.jpg", size, bytes.NewReader(jpegBytes()))
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("got %v, want validation", err)
	}
}

func TestReadBytesRefusesAnotherUsersImage(t *testing.T) {
	svc, userSvc, user, _ := imageFixture(t)
	ctx := context.Background()
	body := jpegBytes()

	stored, err := svc.UploadImage(ctx, user.ID, "me.jpg", int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	other, err := userSvc.Register(ctx, users.Registration{
		Email:        "other@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Other",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register other: %v", err)
	}

	if _, _, err := svc.ReadBytes(ctx, other.ID, stored.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("got %v, want not found", err)
	}
}
