// Package voice turns recordings into the words that were said.
//
// It exists as its own slice, rather than as a method on whichever feature
// happened to need it first, because two products now accept speech: the web
// recorder in internal/capture and Telegram voice notes in internal/messaging.
// One of them records in a browser and the other receives whatever Telegram
// sends, so they arrive in different containers — and the rule that a model
// must never be handed a container it cannot read has to hold for both. One
// service means one place that rule lives.
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

// MaxBytes bounds one recording, before and after conversion.
//
// Opus at the bitrate a phone picks puts a minute of speech well under a
// megabyte, so eight is generous rather than tight. It is a ceiling on what a
// broken client can send, not a budget the honest path spends.
const MaxBytes = 8 << 20

// MaxSeconds is the longest recording this will accept, where the platform
// reports a duration.
//
// Four minutes, and the number is arithmetic rather than taste: decoded to the
// 16 kHz mono 16-bit PCM a model is handed, speech is 32 KB per second, so
// MaxBytes is 262 seconds of it. Four minutes fits with margin. Callers that
// know a duration should refuse past this before spending anything; the byte
// ceiling is what catches the rest.
const MaxSeconds = 240

// Transcoder is the slice of ffmpeg this package needs.
//
// An interface so a test can prove a conversion happened without the binary
// installed, and so a deployment without ffmpeg is a nil field rather than a
// build tag.
type Transcoder interface {
	// ToWAV decodes any container ffmpeg can read into 16 kHz mono WAV.
	ToWAV(ctx context.Context, in []byte) ([]byte, error)
}

// Options are what a Service needs. Both dependencies are optional, and each
// nil switches off a little more rather than breaking the rest.
type Options struct {
	// Transcriber is nil on a deployment whose chain holds no multimodal
	// provider. Voice is then unavailable and every typed path is untouched.
	Transcriber ai.Transcriber

	// Transcode is nil when ffmpeg is not installed. Recordings that are
	// already chain-safe still work; anything else is refused rather than
	// forwarded in a container the provider cannot read.
	Transcode Transcoder

	Log *slog.Logger
}

// Service turns a recording into words.
type Service struct {
	transcriber ai.Transcriber
	transcode   Transcoder
	log         *slog.Logger
}

func NewService(opts Options) *Service {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		transcriber: opts.Transcriber,
		transcode:   opts.Transcode,
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

	// The conversion, and the reason this package is not two lines in a
	// handler. A container the chain cannot read reaches the provider as
	// nothing at all — no error naming audio, just a model that saw nothing —
	// so it is decoded here or it is refused here.
	if !audio.ChainSafe(mime) {
		if s.transcode == nil {
			return "", apperr.Wrap(apperr.ErrUnavailable, "I cannot read that kind of recording on this server")
		}
		converted, err := s.transcode.ToWAV(ctx, recording)
		if err != nil {
			return "", apperr.Wrap(err, "convert the recording for the model")
		}
		if len(converted) == 0 {
			return "", apperr.Wrap(apperr.ErrValidation, "there was nothing in that recording")
		}
		// Decoding inflates a compressed container, so the ceiling has to hold
		// on the way out as well as on the way in.
		if len(converted) > MaxBytes {
			return "", apperr.Wrap(apperr.ErrValidation, "that recording is too long")
		}
		recording, mime = converted, "audio/wav"
	}

	ctx = aiattr.WithUser(ctx, user.ID, surface)

	result, err := s.transcriber.Transcribe(ctx, ai.TranscribeRequest{
		Audio:    recording,
		MIMEType: mime,
		Tier:     string(user.Tier),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Text), nil
}
