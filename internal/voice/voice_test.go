package voice_test

import (
	"bytes"
	"context"
	"errors"
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

	sawMIME    string
	sawTier    string
	sawSize    int
	sawSurface string
	calls      int
}

func (s *stubTranscriber) Transcribe(ctx context.Context, req ai.TranscribeRequest) (ai.TranscribeResult, error) {
	s.calls++
	s.sawMIME = req.MIMEType
	s.sawTier = req.Tier
	s.sawSize = len(req.Audio)
	s.sawSurface = aiattr.From(ctx).Surface
	return ai.TranscribeResult{Text: s.text}, s.err
}

// stubTranscoder counts conversions so a test can prove one did or did not
// happen without ffmpeg installed.
type stubTranscoder struct {
	out   []byte
	err   error
	calls int
}

func (s *stubTranscoder) ToWAV(_ context.Context, in []byte) ([]byte, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.out != nil {
		return s.out, nil
	}
	return append(wavHeader(), in...), nil
}

func oggHeader() []byte { return append([]byte("OggS"), bytes.Repeat([]byte{0}, 64)...) }
func wavHeader() []byte {
	return append([]byte("RIFF\x00\x00\x00\x00WAVE"), bytes.Repeat([]byte{0}, 64)...)
}

func webmHeader() []byte {
	return append([]byte{0x1A, 0x45, 0xDF, 0xA3}, bytes.Repeat([]byte{0}, 64)...)
}

func newUser() users.User { return users.User{ID: uuid.New(), Tier: users.TierFree} }

const surface = "test_surface"

// The regression test for the whole feature. A Telegram voice note is Ogg/Opus,
// and the OpenAI dialect every non-Gemini provider speaks names only wav and
// mp3 — so the model must be handed WAV, never the container that arrived.
func TestOggIsTranscodedBeforeItReachesTheModel(t *testing.T) {
	stub := &stubTranscriber{text: "ran five kilometres"}
	coder := &stubTranscoder{}
	svc := voice.NewService(voice.Options{Transcriber: stub, Transcode: coder})

	text, err := svc.Transcribe(context.Background(), newUser(), oggHeader(), surface)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if text != "ran five kilometres" {
		t.Fatalf("text = %q", text)
	}
	if coder.calls != 1 {
		t.Fatalf("transcoder called %d times, want 1", coder.calls)
	}
	if stub.sawMIME != "audio/wav" {
		t.Fatalf("the model saw %q, want audio/wav", stub.sawMIME)
	}
}

// The web path already uploads WAV. Transcoding it again would be a second
// process per recording bought for nothing, and this asserts it does not happen.
func TestWavIsNotTranscoded(t *testing.T) {
	stub := &stubTranscriber{text: "slept six hours"}
	coder := &stubTranscoder{}
	svc := voice.NewService(voice.Options{Transcriber: stub, Transcode: coder})

	if _, err := svc.Transcribe(context.Background(), newUser(), wavHeader(), surface); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if coder.calls != 0 {
		t.Fatalf("transcoder called %d times for a wav, want 0", coder.calls)
	}
	if stub.sawMIME != "audio/wav" {
		t.Fatalf("the model saw %q, want audio/wav", stub.sawMIME)
	}
}

// A deployment with no ffmpeg must refuse Opus rather than forward it. Sending
// it anyway reaches the provider as nothing at all, which is the failure that
// looks like the model ignoring the person.
func TestOggWithNoTranscoderIsRefusedRatherThanForwarded(t *testing.T) {
	stub := &stubTranscriber{text: "should not be reached"}
	svc := voice.NewService(voice.Options{Transcriber: stub})

	_, err := svc.Transcribe(context.Background(), newUser(), oggHeader(), surface)
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want an unavailable error", err)
	}
	if stub.calls != 0 {
		t.Fatalf("a provider was called %d times without a transcoder", stub.calls)
	}
}

// webm is not chain-safe either, and a failing ffmpeg must not fall through to
// the provider with the original bytes.
func TestATranscodeFailureDoesNotReachTheModel(t *testing.T) {
	stub := &stubTranscriber{text: "should not be reached"}
	coder := &stubTranscoder{err: errors.New("ffmpeg exploded")}
	svc := voice.NewService(voice.Options{Transcriber: stub, Transcode: coder})

	if _, err := svc.Transcribe(context.Background(), newUser(), webmHeader(), surface); err == nil {
		t.Fatal("err = nil, want the transcode failure")
	}
	if stub.calls != 0 {
		t.Fatalf("a provider was called %d times after a failed transcode", stub.calls)
	}
}

func TestRefusesWhatIsNotARecording(t *testing.T) {
	stub := &stubTranscriber{}
	svc := voice.NewService(voice.Options{Transcriber: stub, Transcode: &stubTranscoder{}})

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

// Decoding to 16 kHz PCM inflates a compressed container, so the bound has to
// hold on what comes out as well as on what went in.
func TestOversizeAfterTranscodingIsRefusedToo(t *testing.T) {
	stub := &stubTranscriber{}
	coder := &stubTranscoder{out: append(wavHeader(), bytes.Repeat([]byte{0}, voice.MaxBytes)...)}
	svc := voice.NewService(voice.Options{Transcriber: stub, Transcode: coder})

	_, err := svc.Transcribe(context.Background(), newUser(), oggHeader(), surface)
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if stub.calls != 0 {
		t.Fatalf("a provider was called with %d bytes", stub.sawSize)
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
