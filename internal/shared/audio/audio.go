// Package audio names the container a recording arrived in, and says whether
// every provider North talks to can read it.
//
// It sits in shared rather than beside either caller because both surfaces that
// accept speech — the web recorder in internal/capture and Telegram voice notes
// in internal/messaging — must hand a model the same shape. Two copies of this
// would drift, and the drift would be invisible: a container one surface accepts
// and the other silently mis-reads.
package audio

import "strings"

// sniff is one container signature and the type it means.
type sniff struct {
	mime  string
	match func([]byte) bool
}

// types are the containers a recording may arrive in.
//
// Sniffed here rather than through http.DetectContentType because Go's sniffer
// answers "video/webm" and "video/mp4" for containers that hold only an audio
// track — which is exactly what MediaRecorder produces — and "application/ogg"
// for the Ogg that Telegram sends. Taking its word would mean either refusing
// every real recording or widening the allow-list to video.
//
// The declared Content-Type is never consulted. It is a claim; these bytes are
// the fact.
var types = []sniff{
	// EBML, which is Matroska and therefore WebM. What Chrome, Firefox and
	// Android record.
	{"audio/webm", func(b []byte) bool {
		return len(b) >= 4 && b[0] == 0x1A && b[1] == 0x45 && b[2] == 0xDF && b[3] == 0xA3
	}},
	// ISO base media, which is mp4 and m4a. What Safari and iOS record.
	{"audio/mp4", func(b []byte) bool {
		return len(b) >= 12 && string(b[4:8]) == "ftyp"
	}},
	// Ogg, which is what a Telegram voice note is: Opus in an Ogg container.
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

// Sniff names the container, or returns "" for something that is not one of
// them. Callers refuse on "" rather than guessing.
func Sniff(header []byte) string {
	for _, t := range types {
		if t.match(header) {
			return t.mime
		}
	}
	return ""
}

// ChainSafe reports whether every provider in North's chain can read this
// container directly.
//
// Only two, and the number is not arbitrary: the OpenAI dialect carries audio
// as a bare format word and names exactly "wav" and "mp3" — see audioFormat in
// internal/ai/openaicompat/client.go. Gemini ingests far more than that, but a
// deployment whose chain has no Gemini key is the ordinary case, so the floor is
// what the dialect accepts.
//
// Anything else must be transcoded before it reaches a model. Sending it anyway
// fails in the least useful way available: no error naming audio, just a model
// that saw nothing.
func ChainSafe(mime string) bool {
	base, _, _ := strings.Cut(mime, ";")
	switch strings.TrimSpace(base) {
	case "audio/wav", "audio/mpeg":
		return true
	default:
		return false
	}
}
