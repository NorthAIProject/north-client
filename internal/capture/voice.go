package capture

import (
	"context"
	"strings"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// MaxAudioBytes bounds one recording.
//
// Opus at the bitrate a browser picks puts a minute of speech well under a
// megabyte, so eight is generous rather than tight. It is a ceiling on what a
// broken client can post, not a budget the honest path spends.
const MaxAudioBytes = 8 << 20

// MaxAudioSeconds is the recording cap the client enforces.
//
// Declared here so the number lives beside the rest of the feature's bounds
// rather than only in JavaScript, where the server could not see it. The server
// does not measure duration — that would mean decoding the container to count
// frames, which is a codec dependency bought to re-check something the byte
// ceiling already bounds.
const MaxAudioSeconds = 60

// audioSniff is one container signature and the type it means.
type audioSniff struct {
	mime  string
	match func([]byte) bool
}

// audioTypes are the containers a recording may arrive in.
//
// Sniffed here rather than through http.DetectContentType because Go's sniffer
// answers "video/webm" and "video/mp4" for containers that hold only an audio
// track — which is exactly what MediaRecorder produces. Taking its word would
// mean either refusing every real recording or widening the allow-list to
// video, and a video allow-list on an audio endpoint is a hole rather than a
// convenience.
//
// The client's declared Content-Type is never consulted. It is a claim; these
// bytes are the fact.
var audioTypes = []audioSniff{
	// EBML, which is Matroska and therefore WebM. What Chrome, Firefox and
	// Android record.
	{"audio/webm", func(b []byte) bool {
		return len(b) >= 4 && b[0] == 0x1A && b[1] == 0x45 && b[2] == 0xDF && b[3] == 0xA3
	}},
	// ISO base media, which is mp4 and m4a. What Safari and iOS record.
	{"audio/mp4", func(b []byte) bool {
		return len(b) >= 12 && string(b[4:8]) == "ftyp"
	}},
	{"audio/ogg", func(b []byte) bool {
		return len(b) >= 4 && string(b[0:4]) == "OggS"
	}},
	{"audio/wav", func(b []byte) bool {
		return len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WAVE"
	}},
	// An ID3 tag, or an MPEG frame header: eleven set bits, then a layer that
	// is not the reserved one.
	{"audio/mpeg", func(b []byte) bool {
		if len(b) >= 3 && string(b[0:3]) == "ID3" {
			return true
		}
		return len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0 && b[1]&0x06 != 0x00
	}},
}

// sniffAudio names the container, or returns "" for something that is not one
// of them.
func sniffAudio(header []byte) string {
	for _, t := range audioTypes {
		if t.match(header) {
			return t.mime
		}
	}
	return ""
}

// Transcribe turns a recording into the sentence the person would have typed.
//
// It stops there. The transcript goes back to the composer for them to read,
// and the parse is still the separate, deliberate act it was before — pressing
// a button twice rather than once is the price of never writing down a number
// nobody said. A mis-heard "seventeen" that goes straight into a preview is a
// value nobody typed and nobody will notice.
//
// Nothing is stored. A voice note is not a document: the transcript is the
// artefact, and keeping the audio of somebody saying "mood 2, argued with my
// partner" creates a retention question that buys nothing.
func (s *Service) Transcribe(ctx context.Context, user users.User, audio []byte) (string, error) {
	if s.transcriber == nil {
		return "", apperr.Wrap(apperr.ErrUnavailable, "voice notes are not switched on")
	}
	if len(audio) == 0 {
		return "", apperr.Wrap(apperr.ErrValidation, "that recording was empty")
	}
	if len(audio) > MaxAudioBytes {
		return "", apperr.Wrap(apperr.ErrValidation, "that recording is too long")
	}

	mime := sniffAudio(audio)
	if mime == "" {
		return "", apperr.Wrap(apperr.ErrValidation, "that did not arrive as a recording")
	}

	// Metered as its own surface. Audio is the most expensive thing a person
	// can hand North per second of their effort, and rolling it into
	// quick_capture would hide the transcription cost inside the parse it pays
	// for.
	ctx = aiattr.WithUser(ctx, user.ID, spend.SurfaceVoiceCapture)

	result, err := s.transcriber.Transcribe(ctx, ai.TranscribeRequest{
		Audio:    audio,
		MIMEType: mime,
		Tier:     string(user.Tier),
	})
	if err != nil {
		return "", err
	}

	text := strings.TrimSpace(result.Text)
	if len(text) > MaxText {
		// Truncated rather than refused: the person has already spoken, and
		// handing back nothing to punish a long recording loses words they
		// cannot say again identically.
		text = text[:MaxText]
	}
	return text, nil
}
