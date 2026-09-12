package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/NorthAIProject/north-client/internal/ai/openaicompat"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/shared/audio"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/voice"
)

// runVoiceCheck proves this deployment can turn a recording into words.
//
// It exists for the reason telegram-check does, and for one more. The tests
// prove the client against an httptest server, which cannot prove that the real
// service is reachable — and in the cluster it usually is not reachable for a
// reason that looks nothing like itself: `horus` is default-deny ingress, so a
// consumer that is not named in `allow-whisper` sees a hang or a refused
// connection that reads exactly like the service being down.
//
// So this is the command to run from inside the pod. It either hands back a
// transcript or names the policy.
//
// Reads no database and needs no AI credentials. Transcription costs nothing
// per request, so there is no flag to hold it back.
func runVoiceCheck(args []string) error {
	fs := flag.NewFlagSet("voice-check", flag.ContinueOnError)
	file := fs.String("file", "", "a recording to send instead of the built-in tone — the one that failed, for instance")
	language := fs.String("language", "en", "the account language to test, which selects the model (en, pt-PT, pt-BR, es)")
	timeout := fs.Duration("timeout", 200*time.Second, "how long to allow for the whole check")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: main voice-check [flags]

Sends a recording to the configured transcription service and reports what came
back. Carries its own audio; reads no database; costs nothing.

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

	if !cfg.Transcription.Enabled() {
		fmt.Println("endpoint:    NOT CONFIGURED")
		fmt.Println()
		fmt.Println("TRANSCRIBE_PROVIDER_OPENAI_BASEURL is empty, so voice is switched off.")
		fmt.Println("Both surfaces refuse voice notes in words and every typed path works.")
		fmt.Println()
		fmt.Println("In the cluster this should be:")
		fmt.Println("  http://whisper.horus.svc.cluster.local:8000/v1")
		return nil
	}

	// The key is reported as present or absent and never printed. The service
	// this was written for has none at all.
	key := "not set (correct for the in-cluster service, which uses a NetworkPolicy)"
	if cfg.Transcription.APIKey != "" {
		key = "set"
	}
	fmt.Printf("endpoint:    %s\n", cfg.Transcription.BaseURL)
	fmt.Printf("key:         %s\n", key)
	fmt.Printf("model:       %s\n", cfg.Transcription.Model)
	fmt.Printf("model (en):  %s\n", cfg.Transcription.EnglishModel)

	sample, label := toneOggOpus, "built-in tone"
	if *file != "" {
		sample, err = os.ReadFile(*file)
		if err != nil {
			return fmt.Errorf("read %s: %w", *file, err)
		}
		label = *file
	}

	sniffed := audio.Sniff(sample)
	if sniffed == "" {
		fmt.Printf("recording:   %d bytes, NOT A RECOGNISED CONTAINER\n", len(sample))
		return fmt.Errorf("voice-check: those bytes are not a container North accepts")
	}
	fmt.Printf("recording:   %s, %d bytes, %s\n", label, len(sample), sniffed)

	client, err := openaicompat.NewTranscriptionClient(openaicompat.TranscriptionOptions{
		BaseURL:      cfg.Transcription.BaseURL,
		APIKey:       cfg.Transcription.APIKey,
		Model:        cfg.Transcription.Model,
		EnglishModel: cfg.Transcription.EnglishModel,
	})
	if err != nil {
		return err
	}

	svc := voice.NewService(voice.Options{Transcriber: client})
	user := users.User{ID: uuid.New(), Tier: users.TierFree, Locale: users.Locale(*language)}

	started := time.Now()
	text, err := svc.Transcribe(ctx, user, sample, spend.SurfaceVoiceCapture)
	elapsed := time.Since(started)

	if err != nil {
		fmt.Printf("transcribe:  FAILED after %s\n", elapsed.Round(time.Millisecond))
		fmt.Println()
		fmt.Printf("  %v\n", err)
		fmt.Println()
		fmt.Println("If that was a hang or a refused connection, check the NetworkPolicy")
		fmt.Println("before anything else. `horus` is default-deny ingress, so a consumer")
		fmt.Println("that is not named in allow-whisper in the infra repo's")
		fmt.Println("cluster/network-policies/horus.yaml cannot reach the service — and the")
		fmt.Println("failure looks exactly like the service being down.")
		return err
	}

	// The wall-clock matters as much as the words. The service is CPU-bound and
	// shared, and this number is what voice.MaxSeconds should be set against.
	fmt.Printf("transcribe:  %s (language %s)\n", elapsed.Round(time.Millisecond), *language)
	if text == "" {
		fmt.Println("transcript:  empty — correct for a tone, suspicious for speech")
		return nil
	}
	fmt.Printf("transcript:  %q\n", text)
	return nil
}

// toneOggOpus is one second of a 440 Hz sine, as Opus in an Ogg container.
//
// Embedded rather than generated: it is exactly the shape a Telegram voice note
// arrives in, and it now travels to the server unmodified, so it proves the one
// thing this whole change rests on — that the service takes the container as-is.
//
//go:embed voicedata/tone.ogg
var toneOggOpus []byte
