package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/metrics"
)

func scrape(t *testing.T, r *metrics.Registry) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestAGenerationIsCountedByProviderAndOutcome(t *testing.T) {
	r := metrics.New()

	r.CoachGeneration("openrouter", 2*time.Second, false)
	r.CoachGeneration("openrouter", time.Second, true)

	body := scrape(t, r)
	for _, want := range []string{
		`north_coach_generation_duration_seconds_count{outcome="success",provider="openrouter"} 1`,
		`north_coach_generation_duration_seconds_count{outcome="error",provider="openrouter"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestTokensAreCountedByDirection(t *testing.T) {
	r := metrics.New()

	r.CoachTokens("hermes", 120, 340)

	body := scrape(t, r)
	for _, want := range []string{
		`north_coach_tokens_total{direction="input",provider="hermes"} 120`,
		`north_coach_tokens_total{direction="output",provider="hermes"} 340`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
}

// The reason this counter exists: context sources fail soft, so a broken one
// leaves the replies looking fine and only a log line to say otherwise.
func TestAFailedContextSourceIsCountedBySource(t *testing.T) {
	r := metrics.New()

	r.ContextSourceFailed("memories")
	r.ContextSourceFailed("memories")
	r.ContextSourceFailed("goals")

	body := scrape(t, r)
	if !strings.Contains(body, `north_context_source_failures_total{source="memories"} 2`) {
		t.Errorf("memories not counted twice; got %s", body)
	}
	if !strings.Contains(body, `north_context_source_failures_total{source="goals"} 1`) {
		t.Error("goals not counted")
	}
}

func TestAJobIsCountedByKindAndOutcome(t *testing.T) {
	r := metrics.New()

	r.JobFinished("extract_memories", 3*time.Second, false)
	r.JobFinished("extract_memories", time.Second, true)

	body := scrape(t, r)
	for _, want := range []string{
		`north_job_runs_total{kind="extract_memories",outcome="success"} 1`,
		`north_job_runs_total{kind="extract_memories",outcome="error"} 1`,
		`north_job_duration_seconds_count{kind="extract_memories"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
}

// A process with metrics turned off runs the same code paths as one with them
// on. Without this every call site would need a nil check, and one of them
// would eventually be forgotten.
func TestANilRegistryIsSafeToUse(t *testing.T) {
	var r *metrics.Registry

	r.CoachGeneration("openrouter", time.Second, false)
	r.CoachTokens("openrouter", 10, 20)
	r.ContextSourceFailed("memories")
	r.JobFinished("extract_memories", time.Second, false)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when metrics are disabled", rec.Code)
	}
}

// Nothing here may carry a user id, conversation id, or request id. Those are
// unbounded, and an unbounded label set is how a metrics endpoint becomes the
// thing that takes the process down.
func TestNoCollectorCarriesAnUnboundedLabel(t *testing.T) {
	r := metrics.New()

	r.CoachGeneration("openrouter", time.Second, false)
	r.CoachTokens("openrouter", 1, 1)
	r.ContextSourceFailed("memories")
	r.JobFinished("extract_memories", time.Second, false)

	body := scrape(t, r)
	for _, forbidden := range []string{"user_id", "conversation_id", "request_id", "trace_id"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("%q is a label somewhere; that is unbounded cardinality", forbidden)
		}
	}
}

// Transcription is free per request, so what is worth counting is not money but
// time on a shared CPU pod. The histogram's _sum is exactly that: how many
// seconds of one replica North's recordings consumed.
func TestATranscriptionIsCountedBySurfaceAndOutcome(t *testing.T) {
	r := metrics.New()

	r.VoiceTranscription("telegram_voice", 6*time.Second, 14083, metrics.OutcomeSuccess)
	r.VoiceTranscription("telegram_voice", 5*time.Second, 9000, metrics.OutcomeEmpty)
	r.VoiceTranscription("voice_capture", 2*time.Second, 1000, metrics.OutcomeError)

	body := scrape(t, r)
	for _, want := range []string{
		`north_voice_transcriptions_total{outcome="success",surface="telegram_voice"} 1`,
		`north_voice_transcriptions_total{outcome="empty",surface="telegram_voice"} 1`,
		`north_voice_transcriptions_total{outcome="error",surface="voice_capture"} 1`,
		`north_voice_transcription_duration_seconds_count{surface="telegram_voice"} 2`,
		`north_voice_transcription_duration_seconds_sum{surface="telegram_voice"} 11`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
}

// Bytes rather than seconds of speech, because the server never decodes a
// recording and so never learns how long it was.
func TestAudioBytesAreCountedBySurface(t *testing.T) {
	r := metrics.New()

	r.VoiceTranscription("telegram_voice", time.Second, 14083, metrics.OutcomeSuccess)
	r.VoiceTranscription("telegram_voice", time.Second, 917, metrics.OutcomeSuccess)

	if want := `north_voice_audio_bytes_total{surface="telegram_voice"} 15000`; !strings.Contains(scrape(t, r), want) {
		t.Errorf("missing %s", want)
	}
}

// A process with metrics switched off runs the same code paths as one with them
// on, so every method has to tolerate a nil receiver.
func TestVoiceMetricsTolerateNoRegistry(t *testing.T) {
	var r *metrics.Registry
	r.VoiceTranscription("telegram_voice", time.Second, 100, metrics.OutcomeSuccess)
}
