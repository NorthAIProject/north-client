package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/users"
)

// spyStorage records what was on disk at the moment the object was stored.
type spyStorage struct {
	tempDir string
	spilled []string
}

var errStopAfterPut = errors.New("stop here: the test has what it came for")

func (s *spyStorage) Put(context.Context, string, string, io.Reader) error {
	s.spilled, _ = filepath.Glob(filepath.Join(s.tempDir, "multipart-*"))
	return errStopAfterPut
}
func (*spyStorage) Get(context.Context, string) (io.ReadCloser, error) { return nil, nil }
func (*spyStorage) Delete(context.Context, string) error               { return nil }
func (*spyStorage) SignedURL(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func multipartVideo(t *testing.T, field string, size int) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile(field, "squat.mp4")
	if err != nil {
		t.Fatal(err)
	}
	// An MP4 signature, then padding: enough for the sniff in storeUpload.
	if _, err = part.Write(append([]byte("\x00\x00\x00\x18ftypmp42"), make([]byte, size)...)); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, form.FormDataContentType()
}

// A video spills to disk past 4 MiB, like every other multipart handler, rather
// than taking net/http's default of 32 MiB of heap per concurrent upload.
//
// 8 MiB is between the two, so it stays in memory under the default and lands on
// disk here.
func TestAVideoUploadSpillsToDiskPastFourMebibytes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	store := &spyStorage{tempDir: dir}
	api := NewAPI(&Service{storage: store}, nil)

	body, contentType := multipartVideo(t, "video", 8<<20)
	req := httptest.NewRequest(http.MethodPost, "/form-checks", body)
	req.Header.Set("Content-Type", contentType)
	req = req.WithContext(auth.ContextWithUser(req.Context(), users.User{ID: uuid.New()}))

	if err := api.upload(echo.New().NewContext(req, httptest.NewRecorder())); err == nil {
		t.Fatal("upload succeeded past a storage that always fails")
	}
	if len(store.spilled) == 0 {
		t.Error("the part stayed in memory: ParseMultipartForm did not run with the 4 MiB bound")
	}

	left, err := filepath.Glob(filepath.Join(dir, "multipart-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// A wrong field name has already spooled the body. The refusal must not leave it.
func TestAVideoUploadWithTheWrongFieldLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	api := NewAPI(&Service{storage: &spyStorage{tempDir: dir}}, nil)

	body, contentType := multipartVideo(t, "not-video", 8<<20)
	req := httptest.NewRequest(http.MethodPost, "/form-checks", body)
	req.Header.Set("Content-Type", contentType)
	req = req.WithContext(auth.ContextWithUser(req.Context(), users.User{ID: uuid.New()}))

	if err := api.upload(echo.New().NewContext(req, httptest.NewRecorder())); err == nil {
		t.Fatal("a request with no video field was accepted")
	}

	left, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// fakeS3 answers just enough of a multipart upload, counting parts in flight.
type fakeS3 struct {
	inFlight atomic.Int32

	mu    sync.Mutex
	most  int32
	parts int
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		_, _ = fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>b</Bucket><Key>k</Key><UploadId>u</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == http.MethodPut && q.Has("partNumber"):
		now := f.inFlight.Add(1)
		defer f.inFlight.Add(-1)

		f.mu.Lock()
		f.parts++
		if now > f.most {
			f.most = now
		}
		f.mu.Unlock()

		// Long enough for a larger pool to have filled up alongside this part.
		time.Sleep(100 * time.Millisecond)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("ETag", `"etag"`)
	case r.Method == http.MethodPost && q.Has("uploadId"):
		_, _ = fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>b</Bucket><Key>k</Key><ETag>"etag"</ETag></CompleteMultipartUploadResult>`)
	case r.Method == http.MethodPut:
		w.Header().Set("ETag", `"etag"`)
	default:
		http.Error(w, "unexpected request: "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
	}
}

// Every part in flight holds a part-sized buffer, so concurrency is memory: the
// default of five is 40 MiB per upload, in a 768 Mi pod with no global cap on
// concurrent uploads.
func TestPutUploadsAtMostTwoPartsAtOnce(t *testing.T) {
	s3 := &fakeS3{}
	srv := httptest.NewServer(s3)
	defer srv.Close()

	store, err := NewS3Storage(context.Background(), S3Options{
		Endpoint:     srv.URL,
		Region:       "test",
		Bucket:       "b",
		AccessKey:    "test",
		SecretKey:    "test",
		UsePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// A plain reader, because that is what an upload hands it: never seekable.
	body := io.LimitReader(strings.NewReader(strings.Repeat("x", 48<<20)), 48<<20)
	if err := store.Put(context.Background(), "k", "video/mp4", body); err != nil {
		t.Fatal(err)
	}

	s3.mu.Lock()
	defer s3.mu.Unlock()
	if s3.parts < 3 {
		t.Fatalf("only %d parts sent; the test needs more than the limit to mean anything", s3.parts)
	}
	if s3.most > uploadConcurrency {
		t.Errorf("%d parts in flight at once, want at most %d", s3.most, uploadConcurrency)
	}
	if uploadConcurrency != 2 {
		t.Errorf("uploadConcurrency = %d, want 2", uploadConcurrency)
	}
}

var _ Storage = (*spyStorage)(nil)
