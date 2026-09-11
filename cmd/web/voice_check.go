package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/shared/audio"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/voice"
)

// runVoiceCheck says which parts of the voice path this machine actually has.
//
// It exists for the same reason telegram-check does. Every test in the tree
// proves the conversion is correct against a stub or against ffmpeg on a
// developer's laptop, and none of them can prove the container image shipped a
// binary with the codecs it needs. The gap is invisible from a green build and
// shows up as voice notes that go quiet, which is the least debuggable failure
// this feature has.
//
// Read-only and free by default: it generates a tone, converts it, and reports.
// --transcribe is the flag that spends money, and it says so.
func runVoiceCheck(args []string) error {
	fs := flag.NewFlagSet("voice-check", flag.ContinueOnError)
	transcribe := fs.Bool("transcribe", false, "send the recording to the provider chain (spends money)")
	file := fs.String("file", "", "a recording to use instead of the built-in tone — the one that failed, for instance")
	timeout := fs.Duration("timeout", 60*time.Second, "how long to allow for the whole check")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: main voice-check [flags]

Reports whether this deployment can turn a recording into words. Generates its
own audio; reads no database. Free unless --transcribe is given.

flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	_ = godotenv.Load()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// ffmpeg first, because everything below depends on it and its absence is
	// the most likely reason somebody is running this.
	ff, err := audio.NewFFmpeg(cfg.FFmpegPath)
	if err != nil {
		if errors.Is(err, audio.ErrNotInstalled) {
			fmt.Println("ffmpeg:      NOT INSTALLED")
			fmt.Println()
			fmt.Println("Voice notes in a compressed container — which is every voice note")
			fmt.Println("Telegram sends — will be refused. Typed paths are unaffected, and the")
			fmt.Println("web recorder still works because the browser uploads WAV.")
			fmt.Println()
			fmt.Println("Install it with `brew install ffmpeg`, or set FFMPEG_PATH.")
			return nil
		}
		return err
	}
	fmt.Println("ffmpeg:      installed")
	fmt.Printf("opus encode: %v\n", ff.CanEncodeOpus())

	// A real recording when one is given. This is the flag somebody reaches for
	// when a particular voice note came back wrong: it answers "can this
	// machine read that file" without involving Telegram, an account, or a
	// database.
	sample, label := toneOggOpus, "test tone"
	if *file != "" {
		sample, err = os.ReadFile(*file)
		if err != nil {
			return fmt.Errorf("read %s: %w", *file, err)
		}
		label = "recording"
	}

	sniffed := audio.Sniff(sample)
	if sniffed == "" {
		fmt.Printf("%-12s %d bytes, NOT A RECOGNISED RECORDING\n", label+":", len(sample))
		return fmt.Errorf("voice-check: those bytes are not a container North accepts")
	}
	fmt.Printf("%-12s %d bytes, sniffed as %s\n", label+":", len(sample), sniffed)

	converted, err := ff.ToWAV(ctx, sample)
	if err != nil {
		return fmt.Errorf("convert the test recording: %w", err)
	}
	fmt.Printf("converted:   %d bytes, sniffed as %s, chain-safe %v\n",
		len(converted), audio.Sniff(converted), audio.ChainSafe(audio.Sniff(converted)))

	if !*transcribe {
		fmt.Println()
		fmt.Println("Conversion works. Add --transcribe to send this to the provider chain")
		fmt.Println("and prove the other half, which costs one model call.")
		return nil
	}

	// No meter and no pool: a check that can fail on an unrelated dependency is
	// a check that answers the wrong question. The call is still real.
	registry, err := providers.Build(ctx, cfg.AI.ProviderOptions(cfg.Env))
	if err != nil {
		return err
	}
	runner := ai.NewRunner(registry, cfg.AI.ChainSet())

	svc := voice.NewService(voice.Options{
		Transcriber: ai.NewRunnerTranscriber(runner, cfg.AI.FastModel),
		Transcode:   ff,
	})

	text, err := svc.Transcribe(ctx, users.User{ID: uuid.New(), Tier: users.TierFree}, sample, spend.SurfaceVoiceCapture)
	if err != nil {
		return fmt.Errorf("transcribe the test recording: %w", err)
	}

	// A sine tone has no words in it, so an empty answer is the correct one
	// there. What is being proved is that the audio reached a model at all,
	// and the failure this catches — a container the provider cannot read —
	// returns no words and no error, so error versus no error is the
	// distinction that matters. Give --file a recording of speech to see the
	// other half.
	if text == "" {
		fmt.Println("transcribe:  a provider answered, with no words — correct for a tone")
	} else {
		fmt.Printf("transcribe:  a provider answered %q\n", text)
	}
	return nil
}

// toneOggOpus is one second of a 440 Hz sine, as Opus in an Ogg container.
//
// Embedded rather than generated, because generating it would need an encoder
// and the thing being checked is the decoder — a machine whose ffmpeg cannot
// encode Opus can still read Telegram perfectly well, and this check must not
// fail on it. It is also exactly the shape a voice note arrives in, which a
// locally produced WAV would not be.
//
//go:embed voicedata/tone.ogg
var toneOggOpus []byte
