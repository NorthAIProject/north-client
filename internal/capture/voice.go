package capture

import (
	"context"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/voice"
)

// MaxAudioBytes bounds one recording.
//
// The number lives in internal/voice, which owns every recording North accepts;
// it is re-exported here so the handler and its tests read one name rather than
// reaching across slices for a constant.
const MaxAudioBytes = voice.MaxBytes

// MaxAudioSeconds is the recording cap the client enforces.
//
// Declared here so the number lives beside the rest of the feature's bounds
// rather than only in JavaScript, where the server could not see it. It stays
// in this package rather than moving with the rest: it documents a button, not
// a server bound, and the button is this package's.
const MaxAudioSeconds = 60

// Transcribe turns a recording into the sentence the person would have typed.
//
// It stops there. The transcript goes back to the composer for them to read,
// and the parse is still the separate, deliberate act it was before — pressing
// a button twice rather than once is the price of never writing down a number
// nobody said. A mis-heard "seventeen" that goes straight into a preview is a
// value nobody typed and nobody will notice.
//
// The work itself belongs to internal/voice, which Telegram voice notes share.
// What is left here is what is particular to this surface: the composer's
// character bound, and the surface the spend is recorded against.
func (s *Service) Transcribe(ctx context.Context, user users.User, audio []byte) (string, error) {
	if s.voice == nil {
		return "", apperr.Wrap(apperr.ErrUnavailable, "voice notes are not switched on")
	}

	// Metered as its own surface. Audio is the most expensive thing a person
	// can hand North per second of their effort, and rolling it into
	// quick_capture would hide the transcription cost inside the parse it pays
	// for.
	text, err := s.voice.Transcribe(ctx, user, audio, spend.SurfaceVoiceCapture)
	if err != nil {
		return "", err
	}

	if len(text) > MaxText {
		// Truncated rather than refused: the person has already spoken, and
		// handing back nothing to punish a long recording loses words they
		// cannot say again identically.
		text = text[:MaxText]
	}
	return text, nil
}
