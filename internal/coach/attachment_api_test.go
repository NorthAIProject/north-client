package coach_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// stubChatMedia stores nothing: it records what it was handed and answers
// with a fixed attachment, or with refuse.
type stubChatMedia struct {
	stored  conversations.Attachment
	refuse  error
	gotName string
	gotBody []byte
	gotUser uuid.UUID
}

func (s *stubChatMedia) StoreChatAttachment(_ context.Context, userID uuid.UUID, filename string, _ int64, body io.Reader) (conversations.Attachment, error) {
	if s.refuse != nil {
		return conversations.Attachment{}, s.refuse
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return conversations.Attachment{}, err
	}
	s.gotUser, s.gotName, s.gotBody = userID, filename, data
	return s.stored, nil
}

func (s *stubChatMedia) LoadChatAttachment(context.Context, uuid.UUID, uuid.UUID) (conversations.Attachment, error) {
	return s.stored, nil
}

// attachmentRouter mounts the upload route as cmd/web does, with the bearer
// check replaced by a fixed signed-in user.
func attachmentRouter(h harness, store *stubChatMedia, as users.User) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.ContextWithUser(req.Context(), as)))
		})
	})
	coach.NewAPI(h.coach, nil, store).UploadRoutes(r)
	return r
}

func postAttachment(t *testing.T, r http.Handler, conversationID uuid.UUID, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/conversations/"+conversationID.String()+"/attachments", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func storedPDF() conversations.Attachment {
	return conversations.Attachment{
		MediaID:  uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		Kind:     "file",
		MIMEType: "application/pdf",
		Name:     "dieta.pdf",
	}
}

func TestUploadingAnAttachmentAnswersWithItsReference(t *testing.T) {
	h := newHarness(t, &fake.Client{})
	conversationID := newConversation(t, h)
	store := &stubChatMedia{stored: storedPDF()}

	rec := postAttachment(t, attachmentRouter(h, store, h.user), conversationID, "dieta.pdf", []byte("%PDF-1.4 plan"))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	// The same bytes as the contract golden file, which the iOS tests decode.
	want, err := os.ReadFile("testdata/chat_attachment.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var got, golden map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	if err := json.Unmarshal(want, &golden); err != nil {
		t.Fatal(err)
	}
	gotJSON, _ := json.Marshal(got)
	goldenJSON, _ := json.Marshal(golden)
	if string(gotJSON) != string(goldenJSON) {
		t.Fatalf("body = %s, want %s", gotJSON, goldenJSON)
	}

	if store.gotName != "dieta.pdf" || string(store.gotBody) != "%PDF-1.4 plan" || store.gotUser != h.user.ID {
		t.Fatalf("store got name=%q body=%q user=%s", store.gotName, store.gotBody, store.gotUser)
	}
}

func TestUploadingToSomeoneElsesConversationIsNotFound(t *testing.T) {
	h := newHarness(t, &fake.Client{})
	conversationID := newConversation(t, h)
	store := &stubChatMedia{stored: storedPDF()}

	stranger := h.user
	stranger.ID = uuid.New()
	rec := postAttachment(t, attachmentRouter(h, store, stranger), conversationID, "dieta.pdf", []byte("%PDF-1.4"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if store.gotBody != nil {
		t.Fatal("the file was stored for a conversation the caller does not own")
	}
}

func TestARefusedFileIsAValidationError(t *testing.T) {
	h := newHarness(t, &fake.Client{})
	conversationID := newConversation(t, h)
	store := &stubChatMedia{refuse: apperr.FieldErrors{{Field: "attachment", Message: "That .csv file is not plain UTF-8 text."}}}

	rec := postAttachment(t, attachmentRouter(h, store, h.user), conversationID, "meals.csv", []byte{0x89, 'P', 'N', 'G'})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not plain UTF-8 text") {
		t.Fatalf("the refusal should carry the media service's sentence: %s", rec.Body)
	}
}

func TestAnOversizedUploadIsRefusedBeforeStoring(t *testing.T) {
	h := newHarness(t, &fake.Client{})
	conversationID := newConversation(t, h)
	store := &stubChatMedia{stored: storedPDF()}

	rec := postAttachment(t, attachmentRouter(h, store, h.user), conversationID, "big.pdf", bytes.Repeat([]byte("a"), 10<<20))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if store.gotBody != nil {
		t.Fatal("an oversized file reached the store")
	}
}
