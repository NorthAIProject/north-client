package openaicompat_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/openaicompat"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// seen is what the server received, so a test can assert the wire rather than
// the client's own idea of what it sent.
type seen struct {
	auth     []string
	fields   map[string]string
	present  map[string]bool
	filename string
	partType string
	audio    []byte
}

// transcribeServer answers like the cluster's speaches does and records the
// request. Everything here is the real multipart parse, not a stub of one.
func transcribeServer(t *testing.T, status int, body string) (*httptest.Server, *seen) {
	t.Helper()

	got := &seen{fields: map[string]string{}, present: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth = r.Header.Values("Authorization")

		if err := r.ParseMultipartForm(16 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for key, values := range r.MultipartForm.Value {
			got.present[key] = true
			if len(values) > 0 {
				got.fields[key] = values[0]
			}
		}
		if files := r.MultipartForm.File["file"]; len(files) > 0 {
			got.present["file"] = true
			got.filename = files[0].Filename
			got.partType = files[0].Header.Get("Content-Type")
			f, err := files[0].Open()
			if err == nil {
				got.audio, _ = io.ReadAll(f)
				_ = f.Close()
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func recording() []byte { return append([]byte("OggS"), []byte("pretend-this-is-opus")...) }

func newTranscriber(t *testing.T, baseURL, apiKey string) *openaicompat.TranscriptionClient {
	t.Helper()
	c, err := openaicompat.NewTranscriptionClient(openaicompat.TranscriptionOptions{
		BaseURL:      baseURL,
		APIKey:       apiKey,
		Model:        "Systran/faster-whisper-small",
		EnglishModel: "Systran/faster-whisper-small.en",
	})
	if err != nil {
		t.Fatalf("NewTranscriptionClient: %v", err)
	}
	return c
}

func request() ai.TranscribeRequest {
	return ai.TranscribeRequest{Audio: recording(), MIMEType: "audio/ogg", Language: "en"}
}

func TestTheRecordingIsSentAsMultipart(t *testing.T) {
	srv, got := transcribeServer(t, http.StatusOK, `{"text":"I slept six hours"}`)

	out, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), request())
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if out.Text != "I slept six hours" {
		t.Fatalf("text = %q", out.Text)
	}
	if !got.present["file"] {
		t.Fatal("no file part")
	}
	if string(got.audio) != string(recording()) {
		t.Fatalf("the audio did not round-trip: %q", got.audio)
	}
	if got.fields["response_format"] != "json" {
		t.Fatalf("response_format = %q, want json pinned rather than left to the server", got.fields["response_format"])
	}
}

// The server sits behind a NetworkPolicy, not a credential. An empty header is
// not the same as no header: some gateways reject the former.
func TestAnEmptyKeySendsNoAuthorizationHeaderAtAll(t *testing.T) {
	srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)

	if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), request()); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if len(got.auth) != 0 {
		t.Fatalf("Authorization = %q, want the header absent", got.auth)
	}
}

func TestAKeyIsSentAsABearerToken(t *testing.T) {
	srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)

	if _, err := newTranscriber(t, srv.URL, "sk-secret").Transcribe(context.Background(), request()); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if len(got.auth) != 1 || got.auth[0] != "Bearer sk-secret" {
		t.Fatalf("Authorization = %q", got.auth)
	}
}

// Telegram frequently omits the type on a voice note, and the server decides
// how to demux partly by filename — so the default has to be what Telegram
// actually sends.
func TestTheFilenameFollowsTheContainer(t *testing.T) {
	cases := map[string]string{
		"audio/ogg":  "audio.ogg",
		"audio/webm": "audio.webm",
		"audio/mp4":  "audio.m4a",
		"audio/mpeg": "audio.mp3",
		"audio/wav":  "audio.wav",
		"":           "audio.ogg",
	}
	for mime, want := range cases {
		t.Run(mime, func(t *testing.T) {
			srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)
			req := request()
			req.MIMEType = mime

			if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), req); err != nil {
				t.Fatalf("transcribe: %v", err)
			}
			if got.filename != want {
				t.Fatalf("filename = %q, want %q", got.filename, want)
			}
		})
	}
}

// CreateFormFile would label every part application/octet-stream. Telling the
// server what the bytes are is strictly better than telling it nothing.
func TestThePartCarriesTheContainersType(t *testing.T) {
	srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)

	if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), request()); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if got.partType != "audio/ogg" {
		t.Fatalf("part Content-Type = %q, want audio/ogg", got.partType)
	}
}

func TestEnglishGetsTheEnglishModel(t *testing.T) {
	for _, language := range []string{"en", "EN", "en-GB"} {
		t.Run(language, func(t *testing.T) {
			srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)
			req := request()
			req.Language = language

			if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), req); err != nil {
				t.Fatalf("transcribe: %v", err)
			}
			if got.fields["model"] != "Systran/faster-whisper-small.en" {
				t.Fatalf("model = %q, want the .en model", got.fields["model"])
			}
		})
	}
}

func TestEveryOtherLanguageGetsTheMultilingualModel(t *testing.T) {
	for _, language := range []string{"pt-PT", "pt-BR", "es", ""} {
		t.Run("lang="+language, func(t *testing.T) {
			srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)
			req := request()
			req.Language = language

			if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), req); err != nil {
				t.Fatalf("transcribe: %v", err)
			}
			if got.fields["model"] != "Systran/faster-whisper-small" {
				t.Fatalf("model = %q, want the multilingual model", got.fields["model"])
			}
		})
	}
}

// The recogniser wants ISO-639-1. Both Portuguese variants are "pt" to it, and
// sending the region would be sending something it does not know.
func TestTheLanguageIsSentWithoutItsRegion(t *testing.T) {
	srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)
	req := request()
	req.Language = "pt-BR"

	if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), req); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if got.fields["language"] != "pt" {
		t.Fatalf("language = %q, want pt", got.fields["language"])
	}
}

// Telling a multilingual model nothing means it guesses, and a guess is how
// Portuguese comes back as Spanish.
func TestAnUnknownLanguageSendsNoLanguageField(t *testing.T) {
	srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)
	req := request()
	req.Language = ""

	if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), req); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if got.present["language"] {
		t.Fatalf("language = %q, want the field absent when nothing is known", got.fields["language"])
	}
}

// An empty prompt is not neutral — it is still an input to the decoder.
func TestNoVocabularySendsNoPromptField(t *testing.T) {
	srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)

	if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), request()); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if got.present["prompt"] {
		t.Fatalf("prompt = %q, want the field absent", got.fields["prompt"])
	}
}

func TestAVocabularyIsSentAsThePromptField(t *testing.T) {
	srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)
	req := request()
	req.Vocabulary = []string{"kettlebell swings", "Yerba mate"}

	if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), req); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if !strings.Contains(got.fields["prompt"], "kettlebell swings") ||
		!strings.Contains(got.fields["prompt"], "Yerba mate") {
		t.Fatalf("prompt = %q, want both terms", got.fields["prompt"])
	}
}

// Whisper's prompt window is 224 tokens and it truncates from the wrong end, so
// a runaway vocabulary silently costs accuracy rather than erroring.
func TestAVocabularyIsCappedBeforeItIsSent(t *testing.T) {
	srv, got := transcribeServer(t, http.StatusOK, `{"text":"ok"}`)
	req := request()
	for i := 0; i < 500; i++ {
		req.Vocabulary = append(req.Vocabulary, "a-fairly-long-goal-title-number")
	}

	if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), req); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if len(got.fields["prompt"]) > 800 {
		t.Fatalf("prompt is %d bytes; it must be capped", len(got.fields["prompt"]))
	}
}

// The advice differs: "type it instead" for something retrying cannot fix,
// "try again" for a busy pod.
func TestStatusCodesMapToTheRightAdvice(t *testing.T) {
	cases := []struct {
		status      int
		unavailable bool
		validation  bool
	}{
		{http.StatusBadRequest, false, true},
		{http.StatusUnsupportedMediaType, false, true},
		{http.StatusUnprocessableEntity, false, true},
		{http.StatusUnauthorized, true, false},
		{http.StatusForbidden, true, false},
		{http.StatusTooManyRequests, false, false},
		{http.StatusInternalServerError, false, false},
		{http.StatusServiceUnavailable, false, false},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv, _ := transcribeServer(t, tc.status, `{"error":"nope"}`)

			_, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), request())
			if err == nil {
				t.Fatal("err = nil, want a failure")
			}
			if apperr.Is(err, apperr.ErrUnavailable) != tc.unavailable {
				t.Fatalf("ErrUnavailable = %v, want %v (err: %v)", !tc.unavailable, tc.unavailable, err)
			}
			if apperr.Is(err, apperr.ErrValidation) != tc.validation {
				t.Fatalf("ErrValidation = %v, want %v (err: %v)", !tc.validation, tc.validation, err)
			}
		})
	}
}

// A response body can echo a key back. It must never reach an error string.
func TestAFailureNeverPutsTheResponseBodyInTheError(t *testing.T) {
	srv, _ := transcribeServer(t, http.StatusUnauthorized, `{"error":"bad key sk-secret-leaked"}`)

	_, err := newTranscriber(t, srv.URL, "sk-secret-leaked").Transcribe(context.Background(), request())
	if err == nil {
		t.Fatal("err = nil")
	}
	if strings.Contains(err.Error(), "sk-secret-leaked") {
		t.Fatalf("the error carries the response body: %v", err)
	}
}

func TestTheContextEndsTheRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if _, err := newTranscriber(t, srv.URL, "").Transcribe(ctx, request()); err == nil {
		t.Fatal("err = nil, want the cancelled context to stop it")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("the cancelled context did not stop the request")
	}
}

// Silence is not a failure; that contract holds all the way up.
func TestAnEmptyTranscriptIsNotAnError(t *testing.T) {
	srv, _ := transcribeServer(t, http.StatusOK, `{"text":"   "}`)

	out, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), request())
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if out.Text != "" {
		t.Fatalf("text = %q, want empty", out.Text)
	}
}

func TestConstructionRefusesWhatItCannotUse(t *testing.T) {
	if _, err := openaicompat.NewTranscriptionClient(openaicompat.TranscriptionOptions{Model: "m"}); err == nil {
		t.Fatal("a client with no base URL was accepted")
	}
	if _, err := openaicompat.NewTranscriptionClient(openaicompat.TranscriptionOptions{BaseURL: "http://x/v1"}); err == nil {
		t.Fatal("a client with no model was accepted")
	}
}

// It must not be registrable as a chat provider: the whole point is that it is
// not in the chain and cannot be reached by a failover walk.
func TestATranscriptionClientIsNotAnAIClient(t *testing.T) {
	c := newTranscriber(t, "http://example.invalid/v1", "")
	if _, ok := any(c).(ai.Client); ok {
		t.Fatal("the transcription client satisfies ai.Client and could be registered in the chain")
	}
	var _ ai.Transcriber = c
}

func TestAMissingBodyIsAFailureNotAnEmptyTranscript(t *testing.T) {
	srv, _ := transcribeServer(t, http.StatusOK, `not json at all`)

	if _, err := newTranscriber(t, srv.URL, "").Transcribe(context.Background(), request()); err == nil {
		t.Fatal("err = nil, want a decode failure")
	}
}
