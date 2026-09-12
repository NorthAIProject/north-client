package voice_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/voice"
)

// stubTranscriber stands in for the model: what a recording says is the
// provider's problem, and this package's problem is what reaches it.
type stubTranscriber struct {
	text string
	err  error

	sawMIME     string
	sawTier     string
	sawLanguage string
	sawSize     int
	sawSurface  string
	calls       int
}

func (s *stubTranscriber) Transcribe(ctx context.Context, req ai.TranscribeRequest) (ai.TranscribeResult, error) {
	s.calls++
	s.sawMIME = req.MIMEType
	s.sawTier = req.Tier
	s.sawLanguage = req.Language
	s.sawSize = len(req.Audio)
	s.sawSurface = aiattr.From(ctx).Surface
	return ai.TranscribeResult{Text: s.text}, s.err
}

func oggHeader() []byte { return append([]byte("OggS"), bytes.Repeat([]byte{0}, 64)...) }
func wavHeader() []byte {
	return append([]byte("RIFF\x00\x00\x00\x00WAVE"), bytes.Repeat([]byte{0}, 64)...)
}

func newUser() users.User { return users.User{ID: uuid.New(), Tier: users.TierFree} }

const surface = "test_surface"

// The point of moving to a speech endpoint: a Telegram voice note is Ogg/Opus,
// and it now reaches the recogniser as Ogg/Opus. Nothing decodes it on the way,
// which is why there is no ffmpeg in this repo any more.
func TestOggReachesTheRecogniserAsOgg(t *testing.T) {
	stub := &stubTranscriber{text: "ran five kilometres"}
	svc := voice.NewService(voice.Options{Transcriber: stub})

	text, err := svc.Transcribe(context.Background(), newUser(), oggHeader(), surface)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if text != "ran five kilometres" {
		t.Fatalf("text = %q", text)
	}
	if stub.sawMIME != "audio/ogg" {
		t.Fatalf("the recogniser saw %q, want audio/ogg — nothing should convert it", stub.sawMIME)
	}
	if stub.sawSize != len(oggHeader()) {
		t.Fatalf("the recogniser saw %d bytes, want the original %d", stub.sawSize, len(oggHeader()))
	}
}

// Every container the browser records reaches the recogniser untouched too.
func TestWavReachesTheRecogniserAsWav(t *testing.T) {
	stub := &stubTranscriber{text: "slept six hours"}
	svc := voice.NewService(voice.Options{Transcriber: stub})

	if _, err := svc.Transcribe(context.Background(), newUser(), wavHeader(), surface); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if stub.sawMIME != "audio/wav" {
		t.Fatalf("the recogniser saw %q, want audio/wav", stub.sawMIME)
	}
}

// The account's language picks the model and suppresses the recogniser's own
// guess. Dropping it is how a Portuguese note comes back as Spanish.
func TestTheAccountsLanguageReachesTheRecogniser(t *testing.T) {
	stub := &stubTranscriber{text: "dormi seis horas"}
	svc := voice.NewService(voice.Options{Transcriber: stub})
	user := users.User{ID: uuid.New(), Tier: users.TierFree, Locale: users.LocalePTBR}

	if _, err := svc.Transcribe(context.Background(), user, oggHeader(), surface); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if stub.sawLanguage != string(users.LocalePTBR) {
		t.Fatalf("language = %q, want %q", stub.sawLanguage, users.LocalePTBR)
	}
}

func TestRefusesWhatIsNotARecording(t *testing.T) {
	stub := &stubTranscriber{}
	svc := voice.NewService(voice.Options{Transcriber: stub})

	_, err := svc.Transcribe(context.Background(), newUser(), []byte("GIF89a this is an image, honestly"), surface)
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if stub.calls != 0 {
		t.Fatalf("a provider was called for a non-recording")
	}
}

func TestRefusesAnEmptyRecording(t *testing.T) {
	stub := &stubTranscriber{}
	svc := voice.NewService(voice.Options{Transcriber: stub})

	_, err := svc.Transcribe(context.Background(), newUser(), nil, surface)
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if stub.calls != 0 {
		t.Fatalf("a provider was called for no audio")
	}
}

func TestOversizeIsRefusedBeforeTheModel(t *testing.T) {
	stub := &stubTranscriber{}
	svc := voice.NewService(voice.Options{Transcriber: stub})

	oversized := append(wavHeader(), bytes.Repeat([]byte{0}, voice.MaxBytes)...)
	_, err := svc.Transcribe(context.Background(), newUser(), oversized, surface)
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if stub.calls != 0 {
		t.Fatalf("a provider was called %d times for an oversized recording", stub.calls)
	}
}

// Nil is the shape of a deployment whose chain holds no multimodal provider. It
// must refuse clearly rather than panic.
func TestWithoutATranscriberIsUnavailableRatherThanAPanic(t *testing.T) {
	svc := voice.NewService(voice.Options{})

	_, err := svc.Transcribe(context.Background(), newUser(), wavHeader(), surface)
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want an unavailable error", err)
	}
}

// The tier selects the provider chain. Dropping it would quietly serve a paying
// account the free chain.
func TestCarriesTheTierToTheChain(t *testing.T) {
	stub := &stubTranscriber{text: "mood 4"}
	svc := voice.NewService(voice.Options{Transcriber: stub})
	user := users.User{ID: uuid.New(), Tier: users.TierPro}

	if _, err := svc.Transcribe(context.Background(), user, wavHeader(), surface); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if stub.sawTier != string(users.TierPro) {
		t.Fatalf("tier = %q, want %q", stub.sawTier, users.TierPro)
	}
}

// The surface is a parameter rather than a constant because two products share
// this service, and the ledger has to tell their spend apart.
func TestCarriesTheCallersSurfaceToTheLedger(t *testing.T) {
	stub := &stubTranscriber{text: "mood 4"}
	svc := voice.NewService(voice.Options{Transcriber: stub})

	if _, err := svc.Transcribe(context.Background(), newUser(), wavHeader(), "telegram_voice"); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if stub.sawSurface != "telegram_voice" {
		t.Fatalf("surface = %q, want telegram_voice", stub.sawSurface)
	}
}

// Silence is not a failure. The caller decides what to say about it.
func TestAnEmptyTranscriptIsNotAnError(t *testing.T) {
	stub := &stubTranscriber{text: "   \n  "}
	svc := voice.NewService(voice.Options{Transcriber: stub})

	text, err := svc.Transcribe(context.Background(), newUser(), wavHeader(), surface)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
}
