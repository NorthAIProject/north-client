package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/config"
)

// A note goes into the library, is found by search and read back; a file
// larger than the JSON cap uploads through the upload group; deleting removes
// it.
func TestKnowledgeAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	note := api.call(http.MethodPost, "/api/v1/knowledge/notes", `{"title":"Knee rehab","body":"Wall sits, three sets of forty seconds, before every long run."}`)
	if note.Code != http.StatusCreated {
		t.Fatalf("note: %d %s", note.Code, note.Body)
	}
	var doc struct{ ID string }
	_ = json.Unmarshal(note.Body.Bytes(), &doc)

	var detail struct{ Text string }
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/knowledge/"+doc.ID, "").Body.Bytes(), &detail)
	if !strings.Contains(detail.Text, "Wall sits") {
		t.Errorf("detail text = %q", detail.Text)
	}

	// Bigger than the 1 MiB JSON cap: only the upload group admits it.
	big := "# Training log\n" + strings.Repeat("Easy run, 8 km, felt fine.\n", 56_000)
	upload := multipartRequest(t, "/api/v1/knowledge/uploads", "file", "log.md", "text/markdown", []byte(big), api.bearer)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, upload)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload of %d bytes: %d %s", len(big), rec.Code, rec.Body)
	}

	var list struct{ Documents []struct{ ID string } }
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/knowledge", "").Body.Bytes(), &list)
	if len(list.Documents) != 2 {
		t.Errorf("library has %d documents, want 2", len(list.Documents))
	}

	if gone := api.call(http.MethodDelete, "/api/v1/knowledge/"+doc.ID, ""); gone.Code != http.StatusNoContent {
		t.Errorf("delete: %d", gone.Code)
	}
	if missing := api.call(http.MethodGet, "/api/v1/knowledge/"+doc.ID, ""); missing.Code != http.StatusNotFound {
		t.Errorf("deleted document: %d, want 404", missing.Code)
	}
}

func TestFormChecksAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	var list struct{ Checks []struct{} }
	if rec := api.call(http.MethodGet, "/api/v1/form-checks", ""); rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	} else {
		_ = json.Unmarshal(rec.Body.Bytes(), &list)
	}
	if len(list.Checks) != 0 {
		t.Errorf("a new account has %d form checks", len(list.Checks))
	}

	noVideo := multipartRequest(t, "/api/v1/form-checks", "other", "x.txt", "text/plain", []byte("x"), api.bearer)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, noVideo)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("upload without a video: %d, want 422", rec.Code)
	}

	if missing := api.call(http.MethodGet, "/api/v1/form-checks/00000000-0000-0000-0000-000000000000", ""); missing.Code != http.StatusNotFound {
		t.Errorf("unknown check: %d, want 404", missing.Code)
	}
}

func multipartRequest(t *testing.T, path, field, filename, contentType string, data []byte, bearer string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="`+field+`"; filename="`+filename+`"`)
	header.Set("Content-Type", contentType)
	part, err := w.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", bearer)
	return req
}
