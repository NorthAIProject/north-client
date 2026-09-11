package capture_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/capture"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/voice"
)

// stubTranscriber stands in for the model, for the reason stubParser does: what
// a recording says is the provider's problem, and this package's problem is
// what it does with the answer.
type stubTranscriber struct {
	text string
	err  error

	sawMIME string
	sawTier string
	sawSize int
	calls   int
}

func (s *stubTranscriber) Transcribe(_ context.Context, req ai.TranscribeRequest) (ai.TranscribeResult, error) {
	s.calls++
	s.sawMIME = req.MIMEType
	s.sawTier = req.Tier
	s.sawSize = len(req.Audio)
	return ai.TranscribeResult{Text: s.text}, s.err
}

// voiceFixture needs no database. Transcribe resolves nothing and writes
// nothing — it hands words back to the person and stops.
func voiceFixture(t *testing.T, stub *stubTranscriber) (*capture.Service, users.User) {
	t.Helper()
	svc := capture.NewService(capture.Options{Voice: newVoice(stub)})
	return svc, users.User{ID: uuid.New(), Tier: users.TierFree}
}

// newVoice wraps the stub the way cmd/web does, including a transcoder, so
// these tests exercise the path the browser actually reaches.
func newVoice(stub *stubTranscriber) *voice.Service {
	return voice.NewService(voice.Options{Transcriber: stub, Transcode: wavTranscoder{}})
}

// wavTranscoder stands in for ffmpeg. It relabels rather than converts, which
// is all these tests need: what they assert is that a container the provider
// chain cannot read never reaches it, not how the conversion is done.
type wavTranscoder struct{}

func (wavTranscoder) ToWAV(_ context.Context, in []byte) ([]byte, error) {
	return append(wavHeader(), in...), nil
}

// Real leading bytes for each container, because the point of the sniff is that
// it reads them. A test that passed a declared MIME type would be testing the
// thing the server deliberately does not trust.
func webmHeader() []byte {
	return append([]byte{0x1A, 0x45, 0xDF, 0xA3}, bytes.Repeat([]byte{0}, 64)...)
}

func mp4Header() []byte {
	return append([]byte("\x00\x00\x00\x20ftypM4A "), bytes.Repeat([]byte{0}, 64)...)
}
func oggHeader() []byte { return append([]byte("OggS"), bytes.Repeat([]byte{0}, 64)...) }
func wavHeader() []byte {
	return append([]byte("RIFF\x00\x00\x00\x00WAVE"), bytes.Repeat([]byte{0}, 64)...)
}
func mp3Header() []byte { return append([]byte("ID3\x03\x00"), bytes.Repeat([]byte{0}, 64)...) }

// Every container is accepted, and every one of them reaches the model as a
// type the whole provider chain can read.
//
// This last part is a change: the sniffed container used to be forwarded as it
// was, which worked because the browser re-encodes to WAV before uploading and
// hid the fact that webm reaches an OpenAI-dialect provider as nothing at all.
// An older browser, or a failed re-encode, met that silently. See
// internal/shared/audio.ChainSafe.
func TestTranscribeAcceptsEveryContainerABrowserRecords(t *testing.T) {
	cases := map[string]struct {
		audio []byte
		mime  string
	}{
		"webm from chrome and firefox": {webmHeader(), "audio/wav"},
		"mp4 from safari and ios":      {mp4Header(), "audio/wav"},
		"ogg":                          {oggHeader(), "audio/wav"},
		"wav":                          {wavHeader(), "audio/wav"},
		"mp3 with an id3 tag":          {mp3Header(), "audio/mpeg"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stub := &stubTranscriber{text: "slept 6h, 2L water"}
			svc, user := voiceFixture(t, stub)

			text, err := svc.Transcribe(context.Background(), user, tc.audio)
			if err != nil {
				t.Fatalf("transcribe: %v", err)
			}
			if text != "slept 6h, 2L water" {
				t.Fatalf("text = %q", text)
			}
			if stub.sawMIME != tc.mime {
				t.Fatalf("the model saw %q, want %q", stub.sawMIME, tc.mime)
			}
		})
	}
}

// The declared type is never consulted, so bytes that are not a container are
// refused however they are labelled. This is the whole reason the sniff exists:
// an endpoint that trusted Content-Type would forward anything to a provider.
func TestTranscribeRefusesWhatIsNotARecording(t *testing.T) {
	stub := &stubTranscriber{text: "should not be reached"}
	svc, user := voiceFixture(t, stub)

	_, err := svc.Transcribe(context.Background(), user, []byte("GIF89a this is an image, honestly"))
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if stub.calls != 0 {
		t.Fatalf("a provider was called %d times for a non-recording", stub.calls)
	}
}

func TestTranscribeRefusesAnEmptyRecording(t *testing.T) {
	stub := &stubTranscriber{}
	svc, user := voiceFixture(t, stub)

	_, err := svc.Transcribe(context.Background(), user, nil)
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if stub.calls != 0 {
		t.Fatalf("a provider was called for no audio")
	}
}

func TestTranscribeRefusesAnOversizedRecordingBeforeSpendingOnIt(t *testing.T) {
	stub := &stubTranscriber{}
	svc, user := voiceFixture(t, stub)

	oversized := append(webmHeader(), bytes.Repeat([]byte{0}, capture.MaxAudioBytes)...)
	_, err := svc.Transcribe(context.Background(), user, oversized)
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if stub.calls != 0 {
		t.Fatalf("a provider was called %d times for an oversized recording", stub.calls)
	}
}

// Nil is the shape of a deployment whose chain holds no multimodal provider. It
// must refuse clearly rather than panic, because the typed box on the same page
// still works and the person is entitled to keep using it.
func TestTranscribeWithoutAProviderIsUnavailableRatherThanAPanic(t *testing.T) {
	svc := capture.NewService(capture.Options{})
	user := users.User{ID: uuid.New(), Tier: users.TierFree}

	_, err := svc.Transcribe(context.Background(), user, webmHeader())
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want an unavailable error", err)
	}
}

// The tier selects the provider chain. Dropping it would quietly serve a paying
// account the free chain, which is the sort of thing nobody notices from the
// outside.
func TestTranscribeCarriesTheTierToTheChain(t *testing.T) {
	stub := &stubTranscriber{text: "mood 4"}
	svc := capture.NewService(capture.Options{Voice: newVoice(stub)})
	user := users.User{ID: uuid.New(), Tier: users.TierPro}

	if _, err := svc.Transcribe(context.Background(), user, webmHeader()); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if stub.sawTier != string(users.TierPro) {
		t.Fatalf("tier = %q, want %q", stub.sawTier, users.TierPro)
	}
}

// Silence is not a failure. The handler decides what to say about it; turning
// it into an error here would make every caller unwrap one to say it kindly.
func TestTranscribeReturnsNothingForSilenceRatherThanAnError(t *testing.T) {
	stub := &stubTranscriber{text: "   \n  "}
	svc, user := voiceFixture(t, stub)

	text, err := svc.Transcribe(context.Background(), user, webmHeader())
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
}

// A long transcript is cut rather than refused: the person has already spoken,
// and handing back nothing loses words they cannot say again identically.
func TestTranscribeTruncatesToWhatTheBoxHolds(t *testing.T) {
	stub := &stubTranscriber{text: strings.Repeat("a", capture.MaxText+500)}
	svc, user := voiceFixture(t, stub)

	text, err := svc.Transcribe(context.Background(), user, webmHeader())
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if len(text) != capture.MaxText {
		t.Fatalf("len = %d, want %d", len(text), capture.MaxText)
	}
}
