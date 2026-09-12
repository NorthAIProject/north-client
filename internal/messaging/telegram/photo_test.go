package telegram

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/NorthAIProject/north-client/internal/messaging"
)

// uploadAPI records the multipart requests it is sent.
type uploadAPI struct {
	mu      sync.Mutex
	methods []string
	fields  []map[string]string
	files   map[string][]byte
	srv     *httptest.Server
}

func newUploadAPI(t *testing.T) *uploadAPI {
	t.Helper()

	api := &uploadAPI{files: map[string][]byte{}}
	api.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		method := parts[len(parts)-1]

		fields := map[string]string{}
		mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if strings.HasPrefix(mediaType, "multipart/") {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					break
				}
				body, _ := io.ReadAll(p)
				if p.FileName() != "" {
					api.mu.Lock()
					api.files[p.FormName()] = body
					api.mu.Unlock()
					continue
				}
				fields[p.FormName()] = string(body)
			}
		}

		api.mu.Lock()
		api.methods = append(api.methods, method)
		api.fields = append(api.fields, fields)
		api.mu.Unlock()

		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(api.srv.Close)
	return api
}

func (a *uploadAPI) calls() ([]string, []map[string]string, map[string][]byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.methods...), append([]map[string]string(nil), a.fields...), a.files
}

func uploadClient(t *testing.T, api *uploadAPI) *Client {
	t.Helper()
	c := NewClient("test-token")
	c.baseURL = api.srv.URL
	return c
}

func TestAPhotoIsUploadedAsMultipart(t *testing.T) {
	// Telegram fetches an animation from a URL, which is why the client has
	// never needed multipart. A digest card is private health data and must
	// not be fetchable from anywhere, so its bytes go up the wire instead.
	api := newUploadAPI(t)
	png := []byte("\x89PNG\r\n\x1a\n fake bytes")

	err := uploadClient(t, api).Send(context.Background(), "884422", messaging.OutboundMessage{
		Photo:        png,
		PhotoCaption: "Last 7 days",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	methods, fields, files := api.calls()
	if len(methods) == 0 || methods[0] != "sendPhoto" {
		t.Fatalf("methods = %v, want sendPhoto first", methods)
	}
	if got := string(files["photo"]); got != string(png) {
		t.Errorf("uploaded %d bytes, want the %d we passed", len(got), len(png))
	}
	if fields[0]["chat_id"] != "884422" {
		t.Errorf("chat_id = %q, want the chat", fields[0]["chat_id"])
	}
}

func TestAPhotoCaptionIsTrimmedToWhatTelegramAccepts(t *testing.T) {
	// Captions cap at 1024 characters. A longer one is refused outright,
	// which would lose the picture as well as the words.
	api := newUploadAPI(t)

	err := uploadClient(t, api).Send(context.Background(), "884422", messaging.OutboundMessage{
		Photo:        []byte("png"),
		PhotoCaption: strings.Repeat("a", 2000),
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	_, fields, _ := api.calls()
	if n := len([]rune(fields[0]["caption"])); n > 1024 {
		t.Errorf("caption is %d runes, want no more than 1024", n)
	}
}

func TestAMessageWithBothAPhotoAndTextSendsBoth(t *testing.T) {
	// The caption cannot hold a full digest, so the picture leads and the
	// words follow as their own message — the order Send already uses for an
	// animation.
	api := newUploadAPI(t)

	err := uploadClient(t, api).Send(context.Background(), "884422", messaging.OutboundMessage{
		Photo:        []byte("png"),
		PhotoCaption: "Last 7 days",
		Text:         "Body — Strong (88)",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	methods, _, _ := api.calls()
	if len(methods) < 2 {
		t.Fatalf("methods = %v, want the photo and then the text", methods)
	}
	if methods[0] != "sendPhoto" || methods[1] != "sendMessage" {
		t.Errorf("methods = %v, want sendPhoto then sendMessage", methods)
	}
}

func TestAMessageWithNoPhotoNeverUploads(t *testing.T) {
	api := newUploadAPI(t)

	err := uploadClient(t, api).Send(context.Background(), "884422", messaging.OutboundMessage{
		Text: "just words",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	methods, _, files := api.calls()
	for _, m := range methods {
		if m == "sendPhoto" {
			t.Error("uploaded a photo for a text-only message")
		}
	}
	if len(files) != 0 {
		t.Errorf("uploaded %d files for a text-only message", len(files))
	}
}

func TestAPhotoWithNoTextSendsNoEmptyMessage(t *testing.T) {
	// splitMessage returns one empty part for an empty string, so a card with
	// its whole story in the caption would otherwise be followed by a blank
	// message Telegram would refuse.
	api := newUploadAPI(t)

	err := uploadClient(t, api).Send(context.Background(), "884422", messaging.OutboundMessage{
		Photo:        []byte("png"),
		PhotoCaption: "Last 7 days",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	methods, _, _ := api.calls()
	for _, m := range methods {
		if m == "sendMessage" {
			t.Errorf("sent an empty text message alongside the photo: %v", methods)
		}
	}
}
