package ai

import (
	"context"
)

// Transcriber turns a recording into the words that were said.
//
// Separate from Client and type-asserted for rather than required, the same
// choice Embedder makes and for the same reason: most of what North talks to
// does not transcribe, and putting Transcribe on Client would force every
// provider to implement a method that answers "not supported".
//
// An interface rather than a concrete type because the ways to buy
// transcription are genuinely different products — a self-hosted server, a
// hosted speech API, a multimodal chat model. This seam is what made moving
// between them a wiring change rather than an edit to either surface, which is
// exactly what it was then asked to do.
//
// One implementation is deliberately no longer available: a chat model. Handed
// a recording it cannot hear, a chat model answers the prompt instead, and that
// answer is indistinguishable from a transcript to everything downstream — so
// it is stored as the user's own words. See openaicompat.TranscriptionClient,
// which is not an ai.Client and so cannot be reached from the chain at all.
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

	// Language is the speaker's language, as the account records it ("en",
	// "pt-BR"). It does two jobs: it picks the model, and it is sent to the
	// server so a multilingual model does not have to guess — and guessing is
	// how Portuguese comes back as Spanish.
	//
	// Empty means unknown, which is honest: nothing is sent and the recogniser
	// detects. Better than asserting a language nobody chose.
	Language string

	// Vocabulary biases the recogniser toward words this person actually uses
	// — their goals, their habits, the exercises they do. Speech recognisers
	// mangle proper nouns, and this is the cheapest correction available.
	//
	// The implementation decides how to carry it; empty sends nothing at all,
	// because an empty hint is still an input rather than the absence of one.
	Vocabulary []string

	// Tier selects the provider chain where an implementation has one. Nothing
	// reads it today — the cluster's own service is not a chain and has no
	// tiers — but it is the seam a hosted fallback would need, so it stays.
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
