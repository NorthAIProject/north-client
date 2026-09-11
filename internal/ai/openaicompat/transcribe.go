package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// defaultTranscribeTimeout bounds one transcription.
//
// Generous on purpose. The service North uses is faster-whisper on CPU, which
// measures slightly slower than real time — a two-minute recording is a bit
// over two minutes of work, and a timeout shorter than the clip is a timeout
// that only ever fires on the people with the most to say.
const defaultTranscribeTimeout = 180 * time.Second

// maxTranscribeResponse bounds the reply. It is one short JSON object; anything
// larger is a wrong endpoint answering, and reading it all would be the bug.
const maxTranscribeResponse = 64 << 10

// maxPromptBytes bounds the vocabulary hint.
//
// Whisper's prompt window is 224 tokens and it truncates from the wrong end —
// silently, so an overrun costs accuracy rather than raising anything. Bounded
// here, where the wire format is known, rather than at every call site.
const maxPromptBytes = 700

// TranscriptionOptions configure a client for POST {BaseURL}/audio/transcriptions.
type TranscriptionOptions struct {
	// Name labels what answered, for TranscribeResult.Provider and the logs.
	// Not a vendor: the same code reaches a self-hosted server and a hosted one.
	Name string

	// BaseURL is the API root, up to and including the version segment.
	// "/audio/transcriptions" is appended. Required.
	BaseURL string

	// APIKey is empty for a self-hosted server, which sits behind a network
	// policy rather than a credential. Empty sends no Authorization header at
	// all — not an empty one, which some gateways reject outright.
	APIKey string

	// Model is used for every language that is not English. Required.
	Model string

	// EnglishModel is an English-only model, which is smaller and better at
	// English than a multilingual one of the same size. Optional: empty means
	// Model answers everything.
	EnglishModel string

	HTTPClient *http.Client
	Log        *slog.Logger
}

// TranscriptionClient turns recordings into words over OpenAI's
// POST /v1/audio/transcriptions shape.
//
// Named for the wire format rather than for a vendor, and that is not a
// nicety: a hosted API and the cluster's own whisper server are the same
// request with a different base URL, and only one of them needs a key. Naming
// the type after either would make moving between them a code change instead
// of a configuration change.
//
// Deliberately not an ai.Client. It has no Generate and no Name, so it cannot
// be registered in the provider chain — which matters, because a chat model
// handed a recording answers the prompt instead of the audio, and its answer
// is then stored as the user's own words. The type system rules that out here
// rather than a guard having to.
type TranscriptionClient struct {
	http         *http.Client
	name         string
	baseURL      string
	apiKey       string
	model        string
	englishModel string
	log          *slog.Logger
}

// NewTranscriptionClient builds a client, or says what is missing.
//
// A key is not required. That is the point: the service this was written for
// has none.
func NewTranscriptionClient(opts TranscriptionOptions) (*TranscriptionClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "openaicompat: transcription needs a base URL")
	}
	if strings.TrimSpace(opts.Model) == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "openaicompat: transcription needs a model")
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTranscribeTimeout}
	}
	name := opts.Name
	if name == "" {
		name = "openai-compatible"
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}

	return &TranscriptionClient{
		http:         client,
		name:         name,
		baseURL:      baseURL,
		apiKey:       strings.TrimSpace(opts.APIKey),
		model:        strings.TrimSpace(opts.Model),
		englishModel: strings.TrimSpace(opts.EnglishModel),
		log:          log,
	}, nil
}

// Transcribe implements ai.Transcriber.
func (c *TranscriptionClient) Transcribe(ctx context.Context, req ai.TranscribeRequest) (ai.TranscribeResult, error) {
	if len(req.Audio) == 0 {
		return ai.TranscribeResult{}, apperr.Wrap(apperr.ErrValidation, "there was no audio in that")
	}

	model := c.modelFor(req.Language)

	body, contentType, err := buildTranscriptionBody(req, model)
	if err != nil {
		return ai.TranscribeResult{}, apperr.Wrap(err, "build the transcription request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/audio/transcriptions", body)
	if err != nil {
		return ai.TranscribeResult{}, apperr.Wrap(err, "build the transcription request")
	}
	httpReq.Header.Set("Content-Type", contentType)
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		// The hint is here because this failure has a much more likely cause
		// than the obvious one, and the obvious one costs an afternoon: in the
		// cluster the service is behind a default-deny namespace, so a consumer
		// that is not named in the policy sees a hang or a refused connection
		// that reads exactly like the service being down.
		c.log.Warn("transcription endpoint unreachable",
			"error", err,
			"endpoint", c.baseURL,
			"hint", "in the cluster this is usually the allow-whisper NetworkPolicy in cluster/network-policies/horus.yaml, not the service being down")
		return ai.TranscribeResult{}, fmt.Errorf("transcription endpoint unreachable")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// The body is never read into the error. It can echo the key back, and
		// an error string travels further than a log line does.
		c.log.Warn("transcription refused", "status", resp.StatusCode, "endpoint", c.baseURL)
		return ai.TranscribeResult{}, statusError(resp.StatusCode)
	}

	var decoded struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTranscribeResponse)).Decode(&decoded); err != nil {
		return ai.TranscribeResult{}, fmt.Errorf("the transcription service answered with something that is not a transcript")
	}

	// An empty transcript is not an error. Silence, a pocket, a tap: the caller
	// is better placed to say what that means to the person.
	return ai.TranscribeResult{
		Text:     strings.TrimSpace(decoded.Text),
		Provider: c.name,
		Model:    model,
	}, nil
}

// modelFor picks between the two models that are loaded.
//
// Two, not a table: an English-only model is smaller and better at English, and
// everything else needs the multilingual one. A fifth language is a catalogue
// change and nothing here.
func (c *TranscriptionClient) modelFor(language string) string {
	if c.englishModel != "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "en") {
		return c.englishModel
	}
	return c.model
}

// buildTranscriptionBody writes the multipart request.
//
// Split out from the call so the wire format can be asserted without a network,
// which is where most of the mistakes in this file would live.
func buildTranscriptionBody(req ai.TranscribeRequest, model string) (io.Reader, string, error) {
	var buf bytes.Buffer
	buf.Grow(len(req.Audio) + 512)
	mw := multipart.NewWriter(&buf)

	// CreatePart rather than CreateFormFile: the latter hard-codes
	// application/octet-stream, and the server demuxes better when it is told
	// what the bytes are than when it is told nothing. This matters most for
	// the case the default covers — Telegram often sends no type at all.
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="file"; filename=%q`, filenameFor(req.MIMEType)))
	header.Set("Content-Type", contentTypeFor(req.MIMEType))
	part, err := mw.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(req.Audio); err != nil {
		return nil, "", err
	}

	if err := mw.WriteField("model", model); err != nil {
		return nil, "", err
	}
	// Pinned rather than left to the server's default, which is a thing that
	// can change under us in an image bump.
	if err := mw.WriteField("response_format", "json"); err != nil {
		return nil, "", err
	}
	if language := baseLanguage(req.Language); language != "" {
		if err := mw.WriteField("language", language); err != nil {
			return nil, "", err
		}
	}
	if prompt := joinVocabulary(req.Vocabulary); prompt != "" {
		if err := mw.WriteField("prompt", prompt); err != nil {
			return nil, "", err
		}
	}

	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return &buf, mw.FormDataContentType(), nil
}

// filenameFor names the part after its container.
//
// The default is audio.ogg rather than something neutral because Telegram
// frequently omits the type on a voice note, and Ogg is what a voice note is.
func filenameFor(mime string) string {
	switch normaliseMIME(mime) {
	case "audio/webm":
		return "audio.webm"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return "audio.m4a"
	case "audio/mpeg", "audio/mp3":
		return "audio.mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "audio.wav"
	default:
		return "audio.ogg"
	}
}

func contentTypeFor(mime string) string {
	if m := normaliseMIME(mime); m != "" {
		return m
	}
	return "audio/ogg"
}

func normaliseMIME(mime string) string {
	base, _, _ := strings.Cut(mime, ";")
	return strings.ToLower(strings.TrimSpace(base))
}

// baseLanguage reduces a locale to the tag the recogniser understands.
//
// ISO-639-1, so "pt-BR" and "pt-PT" are both "pt" — the recogniser has one
// Portuguese, and sending it a region would be sending it something it does not
// know. Empty stays empty: unknown is a real answer, and detection is a better
// response to it than a guessed language.
func baseLanguage(locale string) string {
	tag, _, _ := strings.Cut(strings.TrimSpace(locale), "-")
	return strings.ToLower(tag)
}

// joinVocabulary renders the hint, bounded.
func joinVocabulary(terms []string) string {
	var b strings.Builder
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		next := term
		if b.Len() > 0 {
			next = ", " + term
		}
		if b.Len()+len(next) > maxPromptBytes {
			break
		}
		b.WriteString(next)
	}
	return b.String()
}

// statusError maps a refusal onto the advice the person should get.
//
// The distinction that matters is not the number, it is what the person should
// do next: a malformed recording and a busy pod both failed, but only one of
// them is worth trying again, and only one of them means "type it instead".
func statusError(status int) error {
	switch status {
	case http.StatusBadRequest, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return apperr.Wrap(apperr.ErrValidation, "that did not arrive as a recording")
	case http.StatusUnauthorized, http.StatusForbidden:
		// Retrying will never fix a credential or a network policy.
		return apperr.Wrap(apperr.ErrUnavailable, "transcription refused this deployment")
	case http.StatusPaymentRequired:
		return apperr.Wrap(apperr.ErrPaymentRequired, "transcription is out of credit")
	default:
		// Busy, restarting, or broken: all worth another go in a moment.
		return fmt.Errorf("the transcription service answered %d", status)
	}
}
