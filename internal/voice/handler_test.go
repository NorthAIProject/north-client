package voice_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/voice"
)

// upload builds the request the browser sends: the recording in an audio field
// and, when a page names one, a surface beside it.
func upload(t *testing.T, recording []byte, withAudio bool, surface string) *http.Request {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if withAudio {
		part, err := form.CreateFormFile("audio", "note.wav")
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		if _, err := part.Write(recording); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if surface != "" {
		if err := form.WriteField("surface", surface); err != nil {
			t.Fatalf("write surface: %v", err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/app/voice/transcribe", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())

	ctx := i18n.WithLocale(context.Background(), i18n.DefaultLocale)
	ctx = auth.ContextWithUser(ctx, newUser())
	return req.WithContext(ctx)
}

// reply is either shape the endpoint answers with.
type reply struct {
	Text  string `json:"text"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// The endpoint's whole contract with dictate.js: a status the script can
// branch on, and either words for the box or words to show beside the button.
func TestTheTranscribeEndpoint(t *testing.T) {
	cases := []struct {
		name string

		// transcriber nil is a deployment with no endpoint configured.
		transcriber *stubTranscriber
		audio       []byte
		withAudio   bool
		surface     string

		wantStatus  int
		wantText    string
		wantMessage string // an i18n key
		wantSurface string
		wantCalls   int
	}{
		{
			name:        "words come back for the box",
			transcriber: &stubTranscriber{text: "ran five kilometres"},
			audio:       wavHeader(),
			withAudio:   true,
			wantStatus:  http.StatusOK,
			wantText:    "ran five kilometres",
			wantSurface: spend.SurfaceDictation,
			wantCalls:   1,
		},
		{
			name:        "quick capture is recorded as a voice note",
			transcriber: &stubTranscriber{text: "slept six hours"},
			audio:       wavHeader(),
			withAudio:   true,
			surface:     "capture",
			wantStatus:  http.StatusOK,
			wantText:    "slept six hours",
			wantSurface: spend.SurfaceVoiceCapture,
			wantCalls:   1,
		},
		{
			// A request body must not be able to invent a spend label.
			name:        "a surface nobody listed is dictation",
			transcriber: &stubTranscriber{text: "mood four"},
			audio:       wavHeader(),
			withAudio:   true,
			surface:     "telegram_voice",
			wantStatus:  http.StatusOK,
			wantText:    "mood four",
			wantSurface: spend.SurfaceDictation,
			wantCalls:   1,
		},
		{
			name:        "an empty recording is refused before the recogniser",
			transcriber: &stubTranscriber{text: "should not be reached"},
			audio:       nil,
			withAudio:   true,
			wantStatus:  http.StatusUnprocessableEntity,
			wantMessage: "dictate.unreadable",
		},
		{
			name:        "no recording at all is refused",
			transcriber: &stubTranscriber{text: "should not be reached"},
			withAudio:   false,
			wantStatus:  http.StatusUnprocessableEntity,
			wantMessage: "dictate.unreadable",
		},
		{
			name:        "silence is refused with words to show",
			transcriber: &stubTranscriber{text: "   "},
			audio:       wavHeader(),
			withAudio:   true,
			wantStatus:  http.StatusUnprocessableEntity,
			wantMessage: "dictate.silent",
			wantSurface: spend.SurfaceDictation,
			wantCalls:   1,
		},
		{
			name:        "nothing to listen with is unavailable, not a retry",
			transcriber: nil,
			audio:       wavHeader(),
			withAudio:   true,
			wantStatus:  http.StatusServiceUnavailable,
			wantMessage: "dictate.unavailable",
		},
		{
			// Just past the recording ceiling but inside the envelope, so it is
			// the handler's own count that refuses it rather than the body cap.
			name:        "a recording past the ceiling is too long",
			transcriber: &stubTranscriber{text: "should not be reached"},
			audio:       append(wavHeader(), bytes.Repeat([]byte{0}, voice.MaxBytes)...),
			withAudio:   true,
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantMessage: "dictate.toolong",
		},
		{
			name:        "a body past the envelope is cut off unread",
			transcriber: &stubTranscriber{text: "should not be reached"},
			audio:       append(wavHeader(), bytes.Repeat([]byte{0}, voice.MaxBytes+2<<20)...),
			withAudio:   true,
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantMessage: "dictate.toolong",
		},
		{
			name:        "a recogniser failure keeps its detail off the screen",
			transcriber: &stubTranscriber{err: errors.New("dial tcp 10.0.0.7:8000: connection refused")},
			audio:       wavHeader(),
			withAudio:   true,
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "dictate.failed",
			wantSurface: spend.SurfaceDictation,
			wantCalls:   1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := voice.Options{}
			if tc.transcriber != nil {
				opts.Transcriber = tc.transcriber
			}
			h := voice.NewHandler(voice.NewService(opts), nil)

			rec := httptest.NewRecorder()
			h.Transcribe(rec, upload(t, tc.audio, tc.withAudio, tc.surface))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}

			var got reply
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("reply is not JSON: %v (%s)", err, rec.Body.String())
			}
			if got.Text != tc.wantText {
				t.Errorf("text = %q, want %q", got.Text, tc.wantText)
			}
			wantMessage := ""
			if tc.wantMessage != "" {
				wantMessage = i18n.Translate(i18n.DefaultLocale, tc.wantMessage)
			}
			if got.Error.Message != wantMessage {
				t.Errorf("message = %q, want %q", got.Error.Message, wantMessage)
			}

			if tc.transcriber == nil {
				return
			}
			if tc.transcriber.calls != tc.wantCalls {
				t.Errorf("recogniser called %d times, want %d", tc.transcriber.calls, tc.wantCalls)
			}
			if tc.transcriber.sawSurface != tc.wantSurface {
				t.Errorf("surface = %q, want %q", tc.transcriber.sawSurface, tc.wantSurface)
			}
		})
	}
}

// The templates read availability from the context the middleware sets, and a
// page rendered without it must offer the typed path only.
func TestAdvertiseTellsThePagesWhetherToOfferAMicrophone(t *testing.T) {
	cases := map[string]struct {
		transcriber ai.Transcriber
		want        bool
	}{
		"a deployment that can listen": {&stubTranscriber{}, true},
		"one that cannot":              {nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := voice.NewHandler(voice.NewService(voice.Options{Transcriber: tc.transcriber}), nil)

			var saw bool
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				saw = voice.DictationAvailable(r.Context())
			})
			h.Advertise(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/app", nil))

			if saw != tc.want {
				t.Fatalf("available = %v, want %v", saw, tc.want)
			}
		})
	}

	if voice.DictationAvailable(context.Background()) {
		t.Fatal("a context the middleware never saw claims dictation")
	}
}
