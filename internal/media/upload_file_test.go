package media_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func pdfBytes() []byte {
	return []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")
}

// docxBytes is only the zip signature: enough for the sniff, which is all the
// upload checks. Reading the document is planimport's job.
func docxBytes() []byte {
	b := make([]byte, 64)
	copy(b, []byte("PK\x03\x04"))
	return b
}

func pngBytes() []byte {
	b := make([]byte, 64)
	copy(b, []byte("\x89PNG\x0d\x0a\x1a\x0a"))
	return b
}

func uploadFile(t *testing.T, svc *media.Service, userID uuid.UUID, name string, body []byte) (media.Media, error) {
	t.Helper()
	return svc.UploadFile(context.Background(), userID, name, int64(len(body)), bytes.NewReader(body))
}

func registerOther(t *testing.T, userSvc *users.Service) users.User {
	t.Helper()
	other, err := userSvc.Register(context.Background(), users.Registration{
		Email:        "other@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Other",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register other: %v", err)
	}
	return other
}

func TestUploadFileStoresDocumentsWithTheirOwnType(t *testing.T) {
	svc, _, user, store := imageFixture(t)

	cases := []struct {
		name     string
		body     []byte
		wantMIME string
		wantExt  string
	}{
		{"dieta.pdf", pdfBytes(), "application/pdf", ".pdf"},
		// iOS hands over whatever the Files app called it.
		{"PLANO.PDF", pdfBytes(), "application/pdf", ".pdf"},
		{"plan.docx", docxBytes(), "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx"},
		{"week.xlsx", docxBytes(), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx"},
		{"meals.csv", []byte("day,meal,food,grams\nMon,Lunch,Rice,120\n"), "text/csv", ".csv"},
		{"meals.tsv", []byte("day\tmeal\nMon\tLunch\n"), "text/tab-separated-values", ".tsv"},
		{"notes.txt", []byte("Breakfast: oats"), "text/plain", ".txt"},
		{"notes.md", []byte("# Plan\n- oats"), "text/markdown", ".md"},
		{"plan.json", []byte(`{"days":[]}`), "application/json", ".json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := uploadFile(t, svc, user.ID, tc.name, tc.body)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != media.KindFile {
				t.Errorf("kind = %q, want %q", got.Kind, media.KindFile)
			}
			if got.MIMEType != tc.wantMIME {
				t.Errorf("mime = %q, want %q", got.MIMEType, tc.wantMIME)
			}
			if got.OriginalName != tc.name {
				t.Errorf("name = %q, want %q", got.OriginalName, tc.name)
			}
			if !strings.HasSuffix(got.StorageKey, tc.wantExt) {
				t.Errorf("storage key %q does not end in %q", got.StorageKey, tc.wantExt)
			}
			if !bytes.Equal(store.objs[got.StorageKey], tc.body) {
				t.Errorf("stored %d bytes, want the %d sent", len(store.objs[got.StorageKey]), len(tc.body))
			}
		})
	}
}

func TestUploadFileRefusesWhatItCannotTrust(t *testing.T) {
	svc, _, user, store := imageFixture(t)

	cases := []struct {
		name string
		body []byte
		want string
	}{
		// A PNG renamed to .csv: the extension says text, the bytes say image.
		{"meals.csv", pngBytes(), ".csv"},
		{"plan.pdf", []byte("just some words"), ".pdf"},
		{"plan.docx", pdfBytes(), ".docx"},
		{"run.exe", []byte("MZ\x90\x00"), "PDF"},
		{"noextension", []byte("hello"), "PDF"},
		{"empty.txt", nil, "empty"},
		// UTF-16 without a byte-order mark sniffs as octet-stream.
		{"utf16.txt", []byte{'h', 0, 'i', 0}, "UTF-8"},
		// With one it sniffs as text, in a charset the readers cannot take.
		{"utf16bom.txt", []byte{0xff, 0xfe, 'h', 0, 'i', 0}, "UTF-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := uploadFile(t, svc, user.ID, tc.name, tc.body)
			if !apperr.Is(err, apperr.ErrValidation) {
				t.Fatalf("got %v, want a validation error", err)
			}
			var fields apperr.FieldErrors
			if !errors.As(err, &fields) || !strings.Contains(fields.Messages()["attachment"], tc.want) {
				t.Fatalf("message %q should mention %q", err, tc.want)
			}
		})
	}
	if len(store.objs) != 0 {
		t.Fatalf("a refused file was stored: %d objects", len(store.objs))
	}
}

func TestUploadFileRefusesOversized(t *testing.T) {
	svc, _, user, _ := imageFixture(t)

	_, err := svc.UploadFile(context.Background(), user.ID, "big.pdf", media.MaxFileBytes+1, bytes.NewReader(pdfBytes()))
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("got %v, want validation", err)
	}
}

func TestStoreChatAttachmentSendsPhotosAndFilesToTheirOwnKind(t *testing.T) {
	svc, _, user, _ := imageFixture(t)
	ctx := context.Background()

	photo, err := svc.StoreChatAttachment(ctx, user.ID, "squat.jpg", int64(len(jpegBytes())), bytes.NewReader(jpegBytes()))
	if err != nil {
		t.Fatal(err)
	}
	if photo.Kind != media.KindImage || photo.MIMEType != "image/jpeg" || photo.Name != "squat.jpg" || photo.MediaID == uuid.Nil {
		t.Fatalf("photo stored as %+v", photo)
	}

	doc, err := svc.StoreChatAttachment(ctx, user.ID, "dieta.pdf", int64(len(pdfBytes())), bytes.NewReader(pdfBytes()))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != media.KindFile || doc.MIMEType != "application/pdf" || doc.Name != "dieta.pdf" {
		t.Fatalf("pdf stored as %+v", doc)
	}

	loaded, err := svc.LoadChatAttachment(ctx, doc.MediaID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != doc {
		t.Fatalf("loaded %+v, want %+v", loaded, doc)
	}

	// A photo that is not one is still refused as a photo would be.
	if _, err := svc.StoreChatAttachment(ctx, user.ID, "fake.jpg", 4, bytes.NewReader([]byte("text"))); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("got %v, want validation", err)
	}
}

func TestChatAttachmentsAreNeverAVideo(t *testing.T) {
	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := media.NewRepository(pool)
	svc := media.NewService(media.Options{Repository: repo, Storage: newMemStorage()})
	ctx := context.Background()

	// A form-check clip is stored directly: UploadVideo needs a job queue.
	clip, err := repo.CreateMedia(ctx, media.NewMedia{
		UserID: user.ID, Kind: media.KindVideo, MIMEType: "video/mp4",
		SizeBytes: 10, StorageKey: "users/x/clip.mp4", OriginalName: "set.mp4",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.LoadChatAttachment(ctx, clip.ID, user.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("LoadChatAttachment: got %v, want not found for a video", err)
	}
	if _, _, err := svc.ReadFile(ctx, user.ID, clip.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("ReadFile: got %v, want not found for a video", err)
	}
	if _, _, err := svc.LatestChatFile(ctx, user.ID, "set.mp4"); !errors.Is(err, media.ErrNoChatFile) {
		t.Fatalf("LatestChatFile: got %v, want ErrNoChatFile for a video", err)
	}
}

func TestReadFileReturnsTheOwnersBytesOnly(t *testing.T) {
	svc, userSvc, user, _ := imageFixture(t)
	ctx := context.Background()

	doc, err := uploadFile(t, svc, user.ID, "dieta.pdf", pdfBytes())
	if err != nil {
		t.Fatal(err)
	}
	m, data, err := svc.ReadFile(ctx, user.ID, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != doc.ID || m.OriginalName != "dieta.pdf" || !bytes.Equal(data, pdfBytes()) {
		t.Fatalf("read back %+v with %d bytes", m, len(data))
	}

	photo, err := svc.UploadImage(ctx, user.ID, "me.jpg", int64(len(jpegBytes())), bytes.NewReader(jpegBytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, data, err := svc.ReadFile(ctx, user.ID, photo.ID); err != nil || !bytes.Equal(data, jpegBytes()) {
		t.Fatalf("a photo should read too: %v", err)
	}

	other := registerOther(t, userSvc)
	if _, _, err := svc.ReadFile(ctx, other.ID, doc.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("got %v, want not found for another account", err)
	}
}

func TestLatestChatFileIsTheNewestOrTheNamedOne(t *testing.T) {
	svc, userSvc, user, _ := imageFixture(t)
	ctx := context.Background()

	if _, _, err := svc.LatestChatFile(ctx, user.ID, ""); !errors.Is(err, media.ErrNoChatFile) || !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("with nothing sent: got %v, want ErrNoChatFile", err)
	}

	dieta, err := uploadFile(t, svc, user.ID, "Dieta.pdf", pdfBytes())
	if err != nil {
		t.Fatal(err)
	}
	notes, err := uploadFile(t, svc, user.ID, "notes.txt", []byte("Lunch: rice"))
	if err != nil {
		t.Fatal(err)
	}

	m, data, err := svc.LatestChatFile(ctx, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != notes.ID || string(data) != "Lunch: rice" {
		t.Fatalf("newest = %q, want notes.txt", m.OriginalName)
	}

	m, data, err = svc.LatestChatFile(ctx, user.ID, "  dieta.PDF ")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != dieta.ID || !bytes.Equal(data, pdfBytes()) {
		t.Fatalf("by name = %q, want Dieta.pdf", m.OriginalName)
	}

	// A second file of the same name is the one meant now.
	again, err := uploadFile(t, svc, user.ID, "dieta.pdf", pdfBytes())
	if err != nil {
		t.Fatal(err)
	}
	if m, _, err = svc.LatestChatFile(ctx, user.ID, "DIETA.pdf"); err != nil || m.ID != again.ID {
		t.Fatalf("by name after a resend = %v %v, want the newer one", m.ID, err)
	}

	if _, _, err := svc.LatestChatFile(ctx, user.ID, "missing.pdf"); !errors.Is(err, media.ErrNoChatFile) {
		t.Fatalf("unknown name: got %v, want ErrNoChatFile", err)
	}

	other := registerOther(t, userSvc)
	if _, _, err := svc.LatestChatFile(ctx, other.ID, ""); !errors.Is(err, media.ErrNoChatFile) {
		t.Fatalf("another account: got %v, want ErrNoChatFile", err)
	}
}
