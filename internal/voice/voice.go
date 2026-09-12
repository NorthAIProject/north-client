// Package voice turns recordings into the words that were said.
//
// It exists as its own slice, rather than as a method on whichever feature
// happened to need it first, because two products accept speech: the web
// recorder in internal/capture and Telegram voice notes in internal/messaging.
// They arrive in different containers from different clients, and the bounds,
// the refusals and the language that go with a recording should not be written
// twice and drift.
//
// Nothing is stored. A voice note is not a document: the transcript is the
// artefact, and keeping the audio of somebody saying "mood 2, argued with my
// partner" creates a retention question that buys nothing.
package voice

import (
	"context"
	"log/slog"
	"strings"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	"github.com/NorthAIProject/north-client/internal/shared/audio"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// MaxBytes bounds one recording.
//
// Opus at the bitrate a phone picks puts a minute of speech well under a
// megabyte, so eight is generous rather than tight. It is a ceiling on what a
// broken client can send, not a budget the honest path spends.
const MaxBytes = 8 << 20

// MaxSeconds is the longest recording this will accept, where the platform
// reports a duration.
//
// Two minutes, and the number is measured rather than chosen. The recogniser is
// faster-whisper on CPU, and it runs slightly slower than real time: a 4.6
// second clip took 5.4-6.1 seconds across three consecutive runs against the
// deployed service. It is also a single replica shared with another
// application, so a clip does not merely cost its own latency — it is time
// nobody else's recording is being transcribed.
//
// At that rate two minutes of speech is a little over two minutes of the shared
// pod, which is the most it seems fair to let one person hold it. The previous
// ceiling of four minutes was arithmetic about a decoding step that no longer
// happens here.
//
// Callers that know a duration should refuse past this before spending
// anything; the byte ceiling is what catches the rest.
const MaxSeconds = 120

// Vocabulary supplies words this person uses that a recogniser would otherwise
// mangle.
//
// An interface with one method so this package keeps importing nothing but the
// AI layer: everything that knows about goals and habits lives in
// internal/voice/vocab, on the other side of it. Nil sends no hint, which costs
// accuracy and nothing else.
type Vocabulary interface {
	VoiceTerms(ctx context.Context, user users.User) ([]string, error)
}

// Options are what a Service needs.
type Options struct {
	// Transcriber is nil on a deployment with no transcription endpoint
	// configured. Voice is then unavailable and says so, and every typed path
	// is untouched.
	Transcriber ai.Transcriber

	// Vocabulary biases the recogniser toward this account's own words. Nil is
	// a working deployment with slightly worse transcripts.
	Vocabulary Vocabulary

	Log *slog.Logger
}

// Service turns a recording into words.
type Service struct {
	transcriber ai.Transcriber
	vocabulary  Vocabulary
	log         *slog.Logger
}

func NewService(opts Options) *Service {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		transcriber: opts.Transcriber,
		vocabulary:  opts.Vocabulary,
		log:         log,
	}
}

// Available reports whether this deployment can transcribe at all. Callers use
// it to say so in words rather than to hide a button.
func (s *Service) Available() bool { return s.transcriber != nil }

// Transcribe returns the words in a recording.
//
// surface is a parameter rather than a constant because the two callers are
// different products and the spend ledger has to tell them apart.
//
// An empty result is not an error. Silence, a pocket, a button held by
// accident: the caller is better placed than this package to say what that
// means to the person, and turning it into an error here would make every
// caller unwrap one to say it kindly.
func (s *Service) Transcribe(ctx context.Context, user users.User, recording []byte, surface string) (string, error) {
	if s.transcriber == nil {
		return "", apperr.Wrap(apperr.ErrUnavailable, "voice notes are not switched on")
	}
	if len(recording) == 0 {
		return "", apperr.Wrap(apperr.ErrValidation, "that recording was empty")
	}
	if len(recording) > MaxBytes {
		return "", apperr.Wrap(apperr.ErrValidation, "that recording is too long")
	}

	mime := audio.Sniff(recording)
	if mime == "" {
		return "", apperr.Wrap(apperr.ErrValidation, "that did not arrive as a recording")
	}

	// Sniffed, but not converted. The recogniser speaks
	// POST /v1/audio/transcriptions and takes every container that arrives here
	// as-is, so the bytes travel untouched — what the sniff is for now is
	// refusing something that is not a recording at all before it is uploaded,
	// and naming the container for the request.

	// Gathered before the call and never allowed to fail it. The hint is worth
	// a couple of indexed queries against a transcription that takes seconds,
	// and worth nothing at all if it costs somebody their sentence.
	var vocabulary []string
	if s.vocabulary != nil {
		terms, err := s.vocabulary.VoiceTerms(ctx, user)
		if err != nil {
			s.log.Warn("voice: could not build a vocabulary hint", "error", err, "user_id", user.ID)
		} else {
			vocabulary = terms
		}
	}

	ctx = aiattr.WithUser(ctx, user.ID, surface)

	result, err := s.transcriber.Transcribe(ctx, ai.TranscribeRequest{
		Audio:    recording,
		MIMEType: mime,
		// The account's language, which picks the model and stops a
		// multilingual recogniser guessing. Taken from the user rather than the
		// context so background work behaves the same as a request.
		Language:   string(user.Locale),
		Vocabulary: vocabulary,
		Tier:       string(user.Tier),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Text), nil
}
