package audio_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/audio"
)

// newFFmpeg skips rather than fails where the binary is absent, the way the
// database tests skip without TEST_DATABASE_URL. A machine without ffmpeg can
// still run everything else.
func newFFmpeg(t *testing.T) *audio.FFmpeg {
	t.Helper()
	f, err := audio.NewFFmpeg("")
	if err != nil {
		if errors.Is(err, audio.ErrNotInstalled) {
			t.Skip("no ffmpeg on PATH; skipping transcode test")
		}
		t.Fatalf("NewFFmpeg: %v", err)
	}
	return f
}

// oggOpusFixture builds the thing Telegram actually sends, with ffmpeg itself.
// A checked-in binary fixture would be a blob nobody could verify; a tone
// generated here is reproducible and obviously what it claims to be.
func oggOpusFixture(t *testing.T) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg on PATH; skipping transcode test")
	}
	cmd := exec.Command("ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-ac", "1", "-c:a", "libopus", "-b:a", "24k", "-f", "ogg", "pipe:1")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build the ogg fixture: %v: %s", err, stderr.String())
	}
	if out.Len() == 0 {
		t.Fatal("the ogg fixture came out empty")
	}
	return out.Bytes()
}

// The conversion Telegram voice notes depend on: Opus in, 16 kHz mono WAV out,
// which is what every provider in the chain can read.
func TestToWAVProducesSixteenKilohertzMono(t *testing.T) {
	f := newFFmpeg(t)
	in := oggOpusFixture(t)

	out, err := f.ToWAV(context.Background(), in)
	if err != nil {
		t.Fatalf("ToWAV: %v", err)
	}
	if audio.Sniff(out) != "audio/wav" {
		t.Fatalf("the result sniffs as %q, want audio/wav", audio.Sniff(out))
	}

	channels, rate, bits := readWAVFormat(t, out)
	if channels != 1 {
		t.Fatalf("channels = %d, want 1", channels)
	}
	if rate != 16000 {
		t.Fatalf("sample rate = %d, want 16000", rate)
	}
	if bits != 16 {
		t.Fatalf("bit depth = %d, want 16", bits)
	}
}

// A WAV written to a pipe cannot seek back to fill in its length, so the header
// carries a placeholder. This asserts the output is still readable as WAV,
// because the alternative — a provider rejecting every recording over a field
// nobody looks at — would be silent.
func TestToWAVOverAPipeIsStillReadable(t *testing.T) {
	f := newFFmpeg(t)

	out, err := f.ToWAV(context.Background(), oggOpusFixture(t))
	if err != nil {
		t.Fatalf("ToWAV: %v", err)
	}
	// ffmpeg reads its own output: if this succeeds the container is valid.
	cmd := exec.Command("ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", "pipe:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("the wav this produced is not readable: %v: %s", err, stderr.String())
	}
}

func TestToWAVRefusesWhatIsNotAudio(t *testing.T) {
	f := newFFmpeg(t)

	_, err := f.ToWAV(context.Background(), []byte("GIF89a this is an image, honestly"))
	if err == nil {
		t.Fatal("err = nil, want a failure")
	}
	if errors.Is(err, audio.ErrNotInstalled) {
		t.Fatalf("err = %v, want a conversion failure", err)
	}
}

func TestToWAVRefusesNothing(t *testing.T) {
	f := newFFmpeg(t)

	if _, err := f.ToWAV(context.Background(), nil); err == nil {
		t.Fatal("err = nil, want a failure for empty input")
	}
}

// A wedged process must not outlive the request that started it.
func TestToWAVStopsWhenTheContextDoes(t *testing.T) {
	f := newFFmpeg(t)
	in := oggOpusFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if _, err := f.ToWAV(ctx, in); err == nil {
		t.Fatal("err = nil, want the cancelled context to stop it")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("took %s to notice a cancelled context", elapsed)
	}
}

// The encoder probe is what turns "voice replies are silently off" into a line
// in the boot log. It must answer without being asked twice.
func TestCanEncodeOpusAnswersFromTheBinaryItFound(t *testing.T) {
	f := newFFmpeg(t)

	// Whatever the answer, asking twice must not change it.
	if first, second := f.CanEncodeOpus(), f.CanEncodeOpus(); first != second {
		t.Fatalf("CanEncodeOpus = %v then %v", first, second)
	}
}

// A path that is not ffmpeg is a misconfiguration, and it must be one the boot
// reports rather than one every recording discovers.
func TestAMissingBinaryIsNamedAtConstruction(t *testing.T) {
	_, err := audio.NewFFmpeg("/nonexistent/definitely-not-ffmpeg")
	if !errors.Is(err, audio.ErrNotInstalled) {
		t.Fatalf("err = %v, want ErrNotInstalled", err)
	}
}

// readWAVFormat reads the fmt chunk. Hand-parsed rather than pulled from a
// library: it is twelve bytes at a known offset, and a dependency to read them
// would be a dependency bought for one test.
func readWAVFormat(t *testing.T, wav []byte) (channels, rate int, bits int) {
	t.Helper()
	if len(wav) < 12 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatal("not a RIFF/WAVE container")
	}
	for i := 12; i+8 <= len(wav); {
		id := string(wav[i : i+4])
		size := int(binary.LittleEndian.Uint32(wav[i+4 : i+8]))
		body := i + 8
		if id == "fmt " {
			if body+16 > len(wav) {
				t.Fatal("the fmt chunk is truncated")
			}
			return int(binary.LittleEndian.Uint16(wav[body+2 : body+4])),
				int(binary.LittleEndian.Uint32(wav[body+4 : body+8])),
				int(binary.LittleEndian.Uint16(wav[body+14 : body+16]))
		}
		if size <= 0 {
			break
		}
		i = body + size + size%2
	}
	t.Fatal("no fmt chunk")
	return 0, 0, 0
}
