package audio_test

import (
	"bytes"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/audio"
)

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

func TestSniffNamesEveryContainerItAccepts(t *testing.T) {
	cases := map[string]struct {
		header []byte
		want   string
	}{
		"webm from chrome and firefox":      {webmHeader(), "audio/webm"},
		"mp4 from safari and ios":           {mp4Header(), "audio/mp4"},
		"ogg, which is what telegram sends": {oggHeader(), "audio/ogg"},
		"wav":                               {wavHeader(), "audio/wav"},
		"mp3 with an id3 tag":               {mp3Header(), "audio/mpeg"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := audio.Sniff(tc.header); got != tc.want {
				t.Fatalf("Sniff = %q, want %q", got, tc.want)
			}
		})
	}
}

// An empty answer rather than a guess. Every caller refuses on "", so a
// container this does not know must never be forwarded to a provider.
func TestSniffRefusesWhatIsNotARecording(t *testing.T) {
	for name, header := range map[string][]byte{
		"a png":          []byte("\x89PNG\r\n\x1a\n"),
		"a gif":          []byte("GIF89a this is an image, honestly"),
		"nothing":        nil,
		"a short prefix": []byte("Og"),
	} {
		t.Run(name, func(t *testing.T) {
			if got := audio.Sniff(header); got != "" {
				t.Fatalf("Sniff = %q, want empty", got)
			}
		})
	}
}
