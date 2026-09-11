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

// AudioReader is a Client that can be handed a recording.
//
// Declared rather than assumed, and assumed false when a client stays silent.
// That direction is the whole point. Every Client answers Generate, so a client
// that cannot actually hear still answers a transcription request — with
// whatever it would have said to the prompt alone. The fake client is the
// clearest case: it answers anything, so when the chain walked past a provider
// that was out of credit and reached it, its canned sentence became the
// transcript. No error, nothing to notice.
//
// That failure is worse than a refusal, because a transcript is not a reply.
// It is entered as the user's own words: the coach answers it, it is stored in
// the conversation, and memory extraction can later treat it as something the
// person said about their life. A wrong answer is visible; words put into
// somebody's mouth are not.
//
// So the cost of the two mistakes is not symmetric. Guessing that a client can
// hear risks inventing a sentence nobody said. Guessing that it cannot costs
// somebody being asked to type instead. A new provider must therefore opt in.
type AudioReader interface {
	// ReadsAudio reports whether a recording can be sent to this client.
	ReadsAudio() bool
}

// canHear reports whether this client may be handed a recording.
//
// Unwrapped because the registry wraps clients for metering, and an assertion
// against the wrapper would fail only where a meter is configured — working on
// a laptop and failing safe in production, which is the worst place to discover
// the difference.
func canHear(c Client) bool {
	reader, ok := Unwrap(c).(AudioReader)
	return ok && reader.ReadsAudio()
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
		// Stepped over rather than asked. ErrUnavailable is a failover error,
		// so the walk continues to the next provider by the ordinary path, and
		// a chain with nobody able to listen ends as ErrUnavailable — which is
		// what lets a caller offer typing instead of apologising for a failure
		// that was really a missing capability.
		if !canHear(c) {
			return apperr.Wrap(apperr.ErrUnavailable,
				"ai: %s cannot be handed a recording", c.Name())
		}

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
