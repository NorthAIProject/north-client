package documents_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// memStorage is the object store for upload tests. A map, not a bucket: these
// tests assert that the service writes and deletes the right key, not that S3
// works.
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
		return nil, fmt.Errorf("missing %s", key)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

func (m *memStorage) get(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	return b, ok
}

func (m *memStorage) len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objs)
}

func uploadFixture(t *testing.T, email string) (*documents.Service, *memStorage, users.User, context.Context) {
	t.Helper()
	pool := testdb.New(t)
	store := newMemStorage()
	user := seedUser(t, pool, email)
	svc := documents.NewService(documents.NewRepository(pool), store, nil)
	return svc, store, user, context.Background()
}

func TestUploadStoresATextFile(t *testing.T) {
	svc, store, user, ctx := uploadFixture(t, "upload-text@north.test")

	body := "# Physio notes\n\nNarrow grip is fine.\n"
	doc, err := svc.Upload(ctx, user.ID, "physio-notes.md", "application/octet-stream", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	if doc.SourceKind != documents.SourceUpload {
		t.Errorf("source = %q, want upload", doc.SourceKind)
	}
	if doc.MIME != "text/markdown" {
		t.Errorf("mime = %q, want text/markdown (sniffed, not the form's octet-stream)", doc.MIME)
	}
	if doc.ByteSize != int64(len(body)) {
		t.Errorf("byte size = %d, want %d", doc.ByteSize, len(body))
	}

	prefix := "users/" + user.ID.String() + "/documents/"
	if !strings.HasPrefix(doc.StorageKey, prefix) {
		t.Errorf("storage key %q is not namespaced to the owner", doc.StorageKey)
	}
	if !strings.HasSuffix(doc.StorageKey, ".md") {
		t.Errorf("storage key %q does not keep the file extension", doc.StorageKey)
	}

	got, ok := store.get(doc.StorageKey)
	if !ok {
		t.Fatal("upload did not write the object")
	}
	if string(got) != body {
		t.Errorf("stored bytes = %q, want %q", got, body)
	}

	listed, err := svc.List(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != doc.ID {
		t.Errorf("list = %+v, want the uploaded document", listed)
	}
}

func TestUploadStoresAPdf(t *testing.T) {
	svc, store, user, ctx := uploadFixture(t, "upload-pdf@north.test")

	pdf := readPDF(t, "physio-report.pdf")
	doc, err := svc.Upload(ctx, user.ID, "physio-report.pdf", "application/octet-stream", bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	if doc.MIME != "application/pdf" {
		t.Errorf("mime = %q, want application/pdf", doc.MIME)
	}
	got, ok := store.get(doc.StorageKey)
	if !ok {
		t.Fatal("upload did not write the PDF")
	}
	if !bytes.Equal(got, pdf) {
		t.Error("stored PDF does not match the file that was uploaded")
	}
}

func TestUploadRejectsUnknownType(t *testing.T) {
	svc, store, user, ctx := uploadFixture(t, "upload-exe@north.test")

	_, err := svc.Upload(ctx, user.ID, "notes.exe", "application/octet-stream", strings.NewReader("MZ"))
	assertFileError(t, err)
	if store.len() != 0 {
		t.Error("a rejected upload left an object in storage")
	}
}

func TestUploadRejectsDisguisedBinary(t *testing.T) {
	svc, store, user, ctx := uploadFixture(t, "upload-binary@north.test")

	body := append([]byte("looks like text"), 0, 1, 2, 3)
	_, err := svc.Upload(ctx, user.ID, "notes.txt", "text/plain", bytes.NewReader(body))
	assertFileError(t, err)
	if store.len() != 0 {
		t.Error("a binary disguised as text was stored")
	}
}

func TestUploadRejectsFakePdf(t *testing.T) {
	svc, store, user, ctx := uploadFixture(t, "upload-fakepdf@north.test")

	_, err := svc.Upload(ctx, user.ID, "report.pdf", "application/pdf", strings.NewReader("this is not a pdf"))
	assertFileError(t, err)
	if store.len() != 0 {
		t.Error("a fake PDF was stored")
	}
}

func TestUploadRejectsOversizedFile(t *testing.T) {
	svc, store, user, ctx := uploadFixture(t, "upload-huge@north.test")

	// One byte over the cap. The content has to look like text so the failure
	// is the size check, not the type sniff.
	body := bytes.Repeat([]byte("a"), 8<<20+1)
	_, err := svc.Upload(ctx, user.ID, "notes.txt", "text/plain", bytes.NewReader(body))
	assertFileError(t, err)
	if store.len() != 0 {
		t.Error("an oversized upload left an object in storage")
	}
}

func TestDeleteRemovesStorageObjectAndRow(t *testing.T) {
	svc, store, user, ctx := uploadFixture(t, "upload-delete@north.test")

	doc, err := svc.Upload(ctx, user.ID, "notes.md", "text/markdown", strings.NewReader("# Keep\n\nThis stays until deleted.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Delete(ctx, doc.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	if _, err = svc.Get(ctx, doc.ID, user.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("get after delete = %v, want not found", err)
	}
	if _, ok := store.get(doc.StorageKey); ok {
		t.Error("delete left the storage object behind")
	}
	listed, err := svc.List(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Errorf("list after delete = %+v, want empty", listed)
	}
}

func TestUploadAndDeleteStayInsideOneAccount(t *testing.T) {
	pool := testdb.New(t)
	store := newMemStorage()
	repo := documents.NewRepository(pool)
	svc := documents.NewService(repo, store, nil)
	ctx := context.Background()

	owner := seedUser(t, pool, "upload-owner@north.test")
	stranger := seedUser(t, pool, "upload-stranger@north.test")

	doc, err := svc.Upload(ctx, owner.ID, "notes.md", "text/markdown", strings.NewReader("# Mine\n\nPrivate.\n"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.Get(ctx, doc.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("stranger get = %v, want not found", err)
	}
	if err = svc.Delete(ctx, doc.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("stranger delete = %v, want not found", err)
	}
	listed, err := svc.List(ctx, stranger.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Errorf("stranger list = %+v, want empty", listed)
	}

	if _, err = svc.Get(ctx, doc.ID, owner.ID); err != nil {
		t.Errorf("owner can no longer read their own file: %v", err)
	}
	if _, ok := store.get(doc.StorageKey); !ok {
		t.Error("a stranger's delete removed the owner's object")
	}
}

func assertFileError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a validation error, got nil")
	}
	var fields apperr.FieldErrors
	if !apperr.As(err, &fields) {
		t.Fatalf("error = %v, want a field error on file", err)
	}
	if fields.Messages()["file"] == "" {
		t.Fatalf("error = %v, missing a message on file", err)
	}
}
