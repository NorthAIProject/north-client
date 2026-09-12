package messaging_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// stubVoice stands in for internal/voice. What a recording says is that
// package's problem; this package's problem is what it does with the answer,
// and what it refuses before asking.
type stubVoice struct {
	text string
	err  error

	calls      int
	sawBytes   int
	sawSurface string
}

func (s *stubVoice) Transcribe(_ context.Context, _ users.User, audio []byte, surface string) (string, error) {
	s.calls++
	s.sawBytes = len(audio)
	s.sawSurface = surface
	return s.text, s.err
}

// stubFiles stands in for the platform's file API, and counts. The point of
// several tests below is that it is never called.
type stubFiles struct {
	bytes []byte
	mime  string
	err   error

	calls int
}

func (s *stubFiles) File(_ context.Context, _ string) ([]byte, string, error) {
	s.calls++
	if s.err != nil {
		return nil, "", s.err
	}
	return s.bytes, s.mime, nil
}

// sendVoice delivers a voice note the way the Telegram adapter does: a file id
// and the hints that came with the update, with the bytes still on the platform.
func (h harness) sendVoice(t *testing.T, chat string, audio []byte, seconds int) messaging.OutboundMessage {
	t.Helper()
	return h.sendVoiceNote(t, chat, messaging.InboundFile{
		Kind:            messaging.KindVoice,
		Name:            "voice.ogg",
		MIMEType:        "audio/ogg",
		DurationSeconds: seconds,
		SizeBytes:       int64(len(audio)),
		FileID:          "AwACAgQAAx",
		Bytes:           audio,
	})
}

func (h harness) sendVoiceNote(t *testing.T, chat string, file messaging.InboundFile) messaging.OutboundMessage {
	t.Helper()

	out, err := h.messaging.Handle(context.Background(), messaging.InboundMessage{
		Platform:   messaging.PlatformTelegram,
		ExternalID: chat,
		UpdateID:   nextUpdateID(),
		ReceivedAt: time.Now(),
		Attachment: &file,
	})
	if err != nil {
		t.Fatalf("handle a voice note: %v", err)
	}
	return out
}

func recording() []byte { return []byte("OggS pretend this is opus") }

// The feature, in one test: somebody speaks, and the coach answers the sentence
// as though it had been typed.
func TestAVoiceNoteReachesTheCoachAsText(t *testing.T) {
	client := fake.Text("Six hours is on the light side.")
	voice := &stubVoice{text: "I slept six hours and drank two litres"}
	h := newHarness(t, client, harnessOptions{voice: voice})
	h.link(t, "700001")

	out := h.sendVoice(t, "700001", recording(), 9)

	if out.Text != "Six hours is on the light side." {
		t.Fatalf("reply = %q", out.Text)
	}
	if voice.calls != 1 {
		t.Fatalf("transcriber called %d times, want 1", voice.calls)
	}

	last := client.LastCall()
	if len(last.Messages) == 0 {
		t.Fatal("the coach was never called")
	}
	final := last.Messages[len(last.Messages)-1]
	if !strings.Contains(final.Text(), "I slept six hours and drank two litres") {
		t.Fatalf("the model saw %q, want the transcript", final.Text())
	}
	// Nothing is stored: a voice note is not a document, and the transcript is
	// the artefact. An audio part reaching the model would also re-bill on
	// every later turn that re-inlines it.
	for _, part := range final.Parts {
		if len(part.InlineData) > 0 {
			t.Fatalf("audio was inlined into the turn (%d bytes)", len(part.InlineData))
		}
	}
}

// The surface exists so the ledger can tell dictating on a phone apart from
// dictating in the composer. Same act, different product.
func TestAVoiceNoteIsBilledToItsOwnSurface(t *testing.T) {
	voice := &stubVoice{text: "ran five kilometres"}
	h := newHarness(t, fake.Text("Nice one."), harnessOptions{voice: voice})
	h.link(t, "700002")

	h.sendVoice(t, "700002", recording(), 4)

	if voice.sawSurface == "" {
		t.Fatal("the transcription was not attributed to any surface")
	}
	if voice.sawSurface == "voice_capture" {
		t.Fatal("Telegram voice was billed as the web recorder's surface")
	}
}

// Two budgets, in this order. A recording that is refused for its size must not
// have cost a dictation first, and a dictation that is refused must not cost a
// coach message either.
func TestAVoiceNoteSpendsTheVoiceBudgetThenTheCoachBudget(t *testing.T) {
	quotas := &stubQuotas{allowed: true}
	voice := &stubVoice{text: "how is my week going"}
	h := newHarness(t, fake.Text("Steady."), harnessOptions{quotas: quotas, voice: voice})
	h.link(t, "700003")

	quotas.actions = nil
	h.sendVoice(t, "700003", recording(), 6)

	if len(quotas.actions) != 2 {
		t.Fatalf("metered %v, want two budgets", quotas.actions)
	}
	if quotas.actions[0] != quota.VoiceCapture {
		t.Fatalf("first budget = %q, want %q", quotas.actions[0], quota.VoiceCapture)
	}
	if quotas.actions[1] != quota.CoachMessage {
		t.Fatalf("second budget = %q, want %q", quotas.actions[1], quota.CoachMessage)
	}
}

func TestAVoiceNoteRefusedByItsQuotaNeverReachesTheCoach(t *testing.T) {
	quotas := &stubQuotas{allowed: true, refuse: quota.VoiceCapture, retryAfter: 30 * time.Second}
	voice := &stubVoice{text: "should not be reached"}
	h := newHarness(t, fake.Text("should not be reached"), harnessOptions{quotas: quotas, voice: voice})
	h.link(t, "700004")

	before := len(h.client.Calls())
	out := h.sendVoice(t, "700004", recording(), 5)

	if out.Text == "" {
		t.Fatal("a refused voice note said nothing")
	}
	if voice.calls != 0 {
		t.Fatalf("transcribed %d times despite the refusal", voice.calls)
	}
	if len(h.client.Calls()) != before {
		t.Fatal("the coach was called for a refused voice note")
	}
	for _, action := range quotas.actions {
		if action == quota.CoachMessage {
			t.Fatal("a refused voice note still spent a coach message")
		}
	}
}

// Too long is refused before anything is spent: the duration is the one bound
// that can be applied without paying for the answer.
func TestAVoiceNoteTooLongIsRefusedBeforeAnyBudgetIsSpent(t *testing.T) {
	quotas := &stubQuotas{allowed: true}
	voice := &stubVoice{text: "should not be reached"}
	h := newHarness(t, fake.Text("should not be reached"), harnessOptions{quotas: quotas, voice: voice})
	h.link(t, "700005")

	quotas.actions = nil
	out := h.sendVoice(t, "700005", recording(), 60*60)

	if out.Text == "" {
		t.Fatal("an over-long voice note said nothing")
	}
	if voice.calls != 0 {
		t.Fatalf("transcribed %d times despite being over the limit", voice.calls)
	}
	if len(quotas.actions) != 0 {
		t.Fatalf("metered %v for a recording that was never transcribed", quotas.actions)
	}
}

// Silence is not a failure, and it must not be answered by the coach either —
// an empty turn would be the model inventing a subject.
func TestAnEmptyTranscriptAsksAgainWithoutSpendingACoachMessage(t *testing.T) {
	quotas := &stubQuotas{allowed: true}
	voice := &stubVoice{text: "   "}
	h := newHarness(t, fake.Text("should not be reached"), harnessOptions{quotas: quotas, voice: voice})
	h.link(t, "700006")

	quotas.actions = nil
	before := len(h.client.Calls())
	out := h.sendVoice(t, "700006", recording(), 3)

	if out.Text == "" {
		t.Fatal("a silent recording got no reply at all")
	}
	if len(h.client.Calls()) != before {
		t.Fatal("the coach was asked to answer silence")
	}
	for _, action := range quotas.actions {
		if action == quota.CoachMessage {
			t.Fatal("silence spent a coach message")
		}
	}
}

// A transcription failure must be answered, not returned. A returned error
// reaches the bridge as the generic apology, which is accurate and useless —
// it tells somebody the bot is broken when what failed was hearing them.
func TestATranscriptionFailureApologisesForTheRightThing(t *testing.T) {
	voice := &stubVoice{err: errors.New("the provider fell over")}
	h := newHarness(t, fake.Text("should not be reached"), harnessOptions{voice: voice})
	h.link(t, "700007")

	out, err := h.messaging.Handle(context.Background(), messaging.InboundMessage{
		Platform:   messaging.PlatformTelegram,
		ExternalID: "700007",
		UpdateID:   nextUpdateID(),
		ReceivedAt: time.Now(),
		Attachment: &messaging.InboundFile{
			Kind:  messaging.KindVoice,
			Bytes: recording(),
		},
	})
	if err != nil {
		t.Fatalf("a failed transcription returned an error rather than words: %v", err)
	}
	if out.Text == "" {
		t.Fatal("a failed transcription said nothing")
	}
}

// A deployment with no transcriber says so rather than dropping the message in
// silence, which is what happened before this existed.
func TestAVoiceNoteWithNoTranscriberSaysSo(t *testing.T) {
	h := newHarness(t, fake.Text("should not be reached"), harnessOptions{})
	h.link(t, "700008")

	before := len(h.client.Calls())
	out := h.sendVoice(t, "700008", recording(), 5)

	if out.Text == "" {
		t.Fatal("a voice note was dropped in silence")
	}
	if len(h.client.Calls()) != before {
		t.Fatal("the coach was called without a transcript")
	}
}

// A typed message must not touch any of this.
func TestATypedMessageDoesNotReachTheTranscriber(t *testing.T) {
	voice := &stubVoice{text: "should not be reached"}
	h := newHarness(t, fake.Text("Sure."), harnessOptions{voice: voice})
	h.link(t, "700009")

	h.send(t, "700009", "how did I sleep last week?")

	if voice.calls != 0 {
		t.Fatalf("a typed message was transcribed %d times", voice.calls)
	}
}

// A chain with nobody able to listen is a different thing from a chain that
// tried and failed, and the person should be told the difference: one is worth
// retrying, the other is worth typing.
//
// This is the path a deployment reaches when its paid provider runs out of
// credit and everything behind it in the chain is deaf.
func TestNobodyAbleToListenAsksThemToTypeRatherThanToRetry(t *testing.T) {
	unavailable := &stubVoice{err: apperr.Wrap(apperr.ErrUnavailable, "ai: nothing in the chain can hear")}
	h := newHarness(t, fake.Text("should not be reached"), harnessOptions{voice: unavailable})
	h.link(t, "700010")

	before := len(h.client.Calls())
	out := h.sendVoice(t, "700010", recording(), 5)

	if out.Text == "" {
		t.Fatal("a voice note reached a deaf chain and got silence")
	}
	if len(h.client.Calls()) != before {
		t.Fatal("the coach was called without a transcript")
	}

	// The same words a deployment with no transcriber at all gives, because it
	// is the same situation from where the person is standing.
	withNothing := newHarness(t, fake.Text("should not be reached"), harnessOptions{})
	withNothing.link(t, "700011")
	expected := withNothing.sendVoice(t, "700011", recording(), 5)

	if out.Text != expected.Text {
		t.Fatalf("said %q, want the same offer to type that a server with no transcriber gives: %q",
			out.Text, expected.Text)
	}
}

// The recogniser is a single CPU replica shared with another application, so an
// over-long clip is refused on the duration the platform already sent — before
// any of it crosses the network, and before it costs the person a dictation.
func TestAnOverLongVoiceNoteIsNeverDownloaded(t *testing.T) {
	files := &stubFiles{bytes: recording(), mime: "audio/ogg"}
	quotas := &stubQuotas{allowed: true}
	transcriber := &stubVoice{text: "should not be reached"}
	h := newHarness(t, fake.Text("should not be reached"), harnessOptions{
		quotas: quotas, voice: transcriber, files: files,
	})
	h.link(t, "700020")

	quotas.actions = nil
	out := h.sendVoiceNote(t, "700020", messaging.InboundFile{
		Kind:            messaging.KindVoice,
		FileID:          "AwACAgQAAx",
		MIMEType:        "audio/ogg",
		DurationSeconds: 60 * 60,
	})

	if out.Text == "" {
		t.Fatal("an over-long voice note said nothing")
	}
	if files.calls != 0 {
		t.Fatalf("downloaded %d times a recording that was refused on its duration", files.calls)
	}
	if transcriber.calls != 0 {
		t.Fatalf("transcribed %d times", transcriber.calls)
	}
	if len(quotas.actions) != 0 {
		t.Fatalf("metered %v for a recording that never moved", quotas.actions)
	}
}

// Same for the size the platform reports. It is a claim rather than a fact,
// which is why the bytes are checked again afterwards — but it is a free claim,
// and acting on it costs nothing.
func TestAnOversizedVoiceNoteIsNeverDownloaded(t *testing.T) {
	files := &stubFiles{bytes: recording(), mime: "audio/ogg"}
	transcriber := &stubVoice{text: "should not be reached"}
	h := newHarness(t, fake.Text("should not be reached"), harnessOptions{
		voice: transcriber, files: files,
	})
	h.link(t, "700021")

	out := h.sendVoiceNote(t, "700021", messaging.InboundFile{
		Kind:      messaging.KindVoice,
		FileID:    "AwACAgQAAx",
		MIMEType:  "audio/ogg",
		SizeBytes: 64 << 20,
	})

	if out.Text == "" {
		t.Fatal("an oversized voice note said nothing")
	}
	if files.calls != 0 {
		t.Fatalf("downloaded %d times a recording refused on its size", files.calls)
	}
}

// The ordinary path: the bytes are fetched once, and only once the recording has
// earned it.
func TestAnAcceptedVoiceNoteIsDownloadedOnce(t *testing.T) {
	files := &stubFiles{bytes: recording(), mime: "audio/ogg"}
	transcriber := &stubVoice{text: "ran five kilometres"}
	h := newHarness(t, fake.Text("Nice one."), harnessOptions{voice: transcriber, files: files})
	h.link(t, "700022")

	out := h.sendVoiceNote(t, "700022", messaging.InboundFile{
		Kind:            messaging.KindVoice,
		FileID:          "AwACAgQAAx",
		MIMEType:        "audio/ogg",
		DurationSeconds: 6,
	})

	if out.Text != "Nice one." {
		t.Fatalf("reply = %q", out.Text)
	}
	if files.calls != 1 {
		t.Fatalf("downloaded %d times, want once", files.calls)
	}
	if transcriber.sawBytes != len(recording()) {
		t.Fatalf("the transcriber saw %d bytes, want the downloaded %d", transcriber.sawBytes, len(recording()))
	}
}

// A download that fails is its own message. It is not a transcription failure
// and should not be dressed as one.
func TestAFailedDownloadSaysSoWithoutTranscribing(t *testing.T) {
	files := &stubFiles{err: errors.New("telegram said no")}
	transcriber := &stubVoice{text: "should not be reached"}
	h := newHarness(t, fake.Text("should not be reached"), harnessOptions{
		voice: transcriber, files: files,
	})
	h.link(t, "700023")

	out := h.sendVoiceNote(t, "700023", messaging.InboundFile{
		Kind:            messaging.KindVoice,
		FileID:          "AwACAgQAAx",
		DurationSeconds: 5,
	})

	if out.Text == "" {
		t.Fatal("a failed download said nothing")
	}
	if transcriber.calls != 0 {
		t.Fatalf("transcribed %d times after a failed download", transcriber.calls)
	}
}

// A platform that hands over the bytes itself still works, and is not asked to
// download them again.
func TestBytesAlreadyInHandAreNotFetchedAgain(t *testing.T) {
	files := &stubFiles{bytes: []byte("different bytes entirely"), mime: "audio/ogg"}
	transcriber := &stubVoice{text: "slept six hours"}
	h := newHarness(t, fake.Text("Right."), harnessOptions{voice: transcriber, files: files})
	h.link(t, "700024")

	h.sendVoice(t, "700024", recording(), 5)

	if files.calls != 0 {
		t.Fatalf("downloaded %d times when the bytes had already arrived", files.calls)
	}
	if transcriber.sawBytes != len(recording()) {
		t.Fatalf("the transcriber saw %d bytes, want the %d that arrived", transcriber.sawBytes, len(recording()))
	}
}
