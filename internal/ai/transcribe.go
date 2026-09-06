package ai

import (
	"context"
	"strings"

	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Transcriber turns a recording into the words that were said.
//
// Separate from Client and type-asserted for rather than required, the same
// choice Embedder makes and for the same reason: most of what North talks to
// does not transcribe, and putting Transcribe on Client would force every
// provider to implement a method that answers "not supported".
//
// It is an interface rather than a concrete type because the two ways to buy
// transcription are genuinely different products. A multimodal chat model reads
// audio as one more kind of part, which is what RunnerTranscriber below does and
// what costs North nothing new today. A dedicated speech endpoint — Whisper,
// Deepgram, Scribe — is a different request shape at a different price with
// different accuracy. Swapping to one should be a new implementation of this
// interface, not an edit to the capture handler.
type Transcriber interface {
	Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResult, error)
}

// TranscribeRequest is one recording.
//
// The audio arrives as bytes rather than an io.Reader because the caller has
// already read it to sniff its type, and because it must be bounded before it
// reaches a provider — a Reader here would move that bound somewhere it is
// easier to forget.
type TranscribeRequest struct {
	Audio    []byte
	MIMEType string

	// Tier selects the provider chain, as it does everywhere else. A plain
	// string, so this package still knows nothing about accounts.
	Tier string
}

// TranscribeResult is what was heard, and who heard it.
type TranscribeResult struct {
	Text string

	// Provider and Model record what answered. Kept for the same reason
	// Response.Model is: a price is keyed on the model, and an unreported one
	// must stay visible as unknown rather than be guessed.
	Provider string
	Model    string
}

// transcribeTemperature is zero. Two runs over the same recording should not
// disagree about how much water it mentions.
var transcribeTemperature float32 = 0

// RunnerTranscriber transcribes by asking a multimodal chat model.
//
// It buys transcription with infrastructure North already owns: Part carries
// InlineData and a MIME type, Gemini ingests audio natively, and the chain walk
// and the spend meter both apply because the call goes through a registered
// client like every other. No new provider, no new credential, no new failure
// mode beyond the one every model call already has.
type RunnerTranscriber struct {
	runner *Runner
	model  string
}

// NewRunnerTranscriber builds the default transcriber. model may be empty for
// the chain's default; a fast model is the right choice here, because reading
// words back is not reasoning.
func NewRunnerTranscriber(runner *Runner, model string) *RunnerTranscriber {
	return &RunnerTranscriber{runner: runner, model: model}
}

// Transcribe returns the words in a recording.
//
// An empty result is not an error. Silence, a pocket, a button held by
// accident: the caller is better placed to say what that means to the person
// than this package is, and turning it into an error here would make every
// caller unwrap one to say it kindly.
func (t *RunnerTranscriber) Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResult, error) {
	if len(req.Audio) == 0 {
		return TranscribeResult{}, apperr.Wrap(apperr.ErrValidation, "there was no audio in that")
	}
	if req.MIMEType == "" {
		return TranscribeResult{}, apperr.Wrap(apperr.ErrValidation, "ai: transcribe needs the audio's type")
	}

	system, err := prompts.Render(prompts.Transcribe, nil)
	if err != nil {
		return TranscribeResult{}, apperr.Wrap(err, "render the transcription prompt")
	}

	var out TranscribeResult
	client, err := t.runner.Run(ctx, RunOptions{Tier: req.Tier}, func(c Client) error {
		resp, genErr := c.Generate(ctx, Request{
			Model:  t.model,
			System: system,
			Messages: []Message{{
				Role:  RoleUser,
				Parts: []Part{{InlineData: req.Audio, MIMEType: req.MIMEType}},
			}},
			Temperature: &transcribeTemperature,
		})
		if genErr != nil {
			return apperr.Wrap(genErr, "transcribe the recording")
		}
		out = TranscribeResult{Text: strings.TrimSpace(resp.Text), Model: resp.Model}
		return nil
	})
	if err != nil {
		return TranscribeResult{}, err
	}

	out.Provider = client.Name()
	return out, nil
}
