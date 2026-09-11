package ai_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// brokeClient is a provider that has run out of credit: the one failure the
// chain exists to survive.
type brokeClient struct{}

func (brokeClient) Name() string     { return "broke" }
func (brokeClient) ReadsAudio() bool { return true }

func (brokeClient) Chat(context.Context, ai.Request) (<-chan ai.StreamChunk, error) {
	return nil, apperr.Wrap(apperr.ErrPaymentRequired, "out of credit")
}

func (brokeClient) Generate(context.Context, ai.Request) (*ai.Response, error) {
	return nil, apperr.Wrap(apperr.ErrPaymentRequired, "out of credit")
}

func (brokeClient) UploadFile(context.Context, ai.UploadRequest) (*ai.File, error) {
	return nil, apperr.Wrap(apperr.ErrPaymentRequired, "out of credit")
}

// hearingClient can be handed a recording, and says what it heard.
type hearingClient struct {
	name string
	text string
	saw  int
}

func (c *hearingClient) Name() string     { return c.name }
func (c *hearingClient) ReadsAudio() bool { return true }

func (c *hearingClient) Chat(context.Context, ai.Request) (<-chan ai.StreamChunk, error) {
	return nil, apperr.Wrap(apperr.ErrUnavailable, "not used here")
}

func (c *hearingClient) Generate(_ context.Context, req ai.Request) (*ai.Response, error) {
	c.saw++
	return &ai.Response{Text: c.text, Model: "heard-it"}, nil
}

func (c *hearingClient) UploadFile(context.Context, ai.UploadRequest) (*ai.File, error) {
	return nil, apperr.Wrap(apperr.ErrUnavailable, "not used here")
}

func recording() ai.TranscribeRequest {
	return ai.TranscribeRequest{
		Audio:    []byte("RIFF\x00\x00\x00\x00WAVEpretend-this-is-speech"),
		MIMEType: "audio/wav",
		Tier:     "free",
	}
}

// The regression this file exists for.
//
// When the paid provider runs out of credit the chain fails over, correctly,
// and keeps walking until something answers. The fake client answers anything,
// so its canned sentence became the transcript — with no error — and the coach
// was handed it as though the person had said it. A voice note about sleep came
// back as a reply about API keys, and the fabricated line entered the
// conversation history as the user's own words.
//
// A provider that cannot hear must be stepped over, not asked.
func TestAProviderThatCannotHearNeverSuppliesATranscript(t *testing.T) {
	canned := "This is the fake coach. Set AI_PROVIDER and the matching API key to talk to a real model."

	registry := ai.NewRegistry()
	registry.Register(brokeClient{})
	registry.Register(fake.Text(canned))

	runner := ai.NewRunner(registry, ai.NewChainSet([]string{"broke", "fake"}, nil))

	out, err := ai.NewRunnerTranscriber(runner, "some-model").Transcribe(context.Background(), recording())

	if strings.Contains(out.Text, "fake coach") {
		t.Fatalf("a client that cannot hear supplied the transcript: %q", out.Text)
	}
	if err == nil {
		t.Fatal("err = nil; a chain with nobody able to listen must say so")
	}
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable so the caller can offer typing instead", err)
	}
}

// And the ordinary case still works: a provider that can hear is asked, once.
func TestAProviderThatCanHearIsAsked(t *testing.T) {
	hearing := &hearingClient{name: "ears", text: "I slept six hours"}

	registry := ai.NewRegistry()
	registry.Register(hearing)

	runner := ai.NewRunner(registry, ai.NewChainSet([]string{"ears"}, nil))

	out, err := ai.NewRunnerTranscriber(runner, "some-model").Transcribe(context.Background(), recording())
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if out.Text != "I slept six hours" {
		t.Fatalf("text = %q", out.Text)
	}
	if out.Provider != "ears" {
		t.Fatalf("provider = %q, want ears", out.Provider)
	}
	if hearing.saw != 1 {
		t.Fatalf("asked %d times, want 1", hearing.saw)
	}
}

// Failover still does its job: past the one that cannot pay, on to the one that
// can hear, without stopping at the one that cannot.
func TestTheWalkSkipsTheDeafAndKeepsGoing(t *testing.T) {
	hearing := &hearingClient{name: "ears", text: "two litres of water"}

	registry := ai.NewRegistry()
	registry.Register(brokeClient{})
	registry.Register(fake.Text("This is the fake coach."))
	registry.Register(hearing)

	runner := ai.NewRunner(registry, ai.NewChainSet([]string{"broke", "fake", "ears"}, nil))

	out, err := ai.NewRunnerTranscriber(runner, "some-model").Transcribe(context.Background(), recording())
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if out.Text != "two litres of water" {
		t.Fatalf("text = %q, want the hearing provider's answer", out.Text)
	}
	if hearing.saw != 1 {
		t.Fatalf("the hearing provider was asked %d times, want 1", hearing.saw)
	}
}

// Fails safe. A client that does not say whether it can hear is not asked to,
// because the cost of guessing wrong is words nobody said entering somebody's
// history — where the cost of a false negative is being asked to type.
func TestAClientThatDoesNotDeclareIsNotAsked(t *testing.T) {
	registry := ai.NewRegistry()
	registry.Register(&silentAboutAudio{})

	runner := ai.NewRunner(registry, ai.NewChainSet([]string{"undeclared"}, nil))

	_, err := ai.NewRunnerTranscriber(runner, "some-model").Transcribe(context.Background(), recording())
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

// silentAboutAudio implements Client and nothing else.
type silentAboutAudio struct{}

func (*silentAboutAudio) Name() string { return "undeclared" }

func (*silentAboutAudio) Chat(context.Context, ai.Request) (<-chan ai.StreamChunk, error) {
	return nil, apperr.Wrap(apperr.ErrUnavailable, "not used here")
}

func (*silentAboutAudio) Generate(context.Context, ai.Request) (*ai.Response, error) {
	return &ai.Response{Text: "I am guessing, and I should not have been asked"}, nil
}

func (*silentAboutAudio) UploadFile(context.Context, ai.UploadRequest) (*ai.File, error) {
	return nil, apperr.Wrap(apperr.ErrUnavailable, "not used here")
}
