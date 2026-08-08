// Package ai is North's boundary with language models.
//
// Everything above this package talks to the Client interface and never to a
// provider SDK. Switching from Gemini to Claude should be an environment
// variable, not a code change — if a service ever needs to know which provider
// is behind the interface, the abstraction has leaked and the leak is the bug.
//
// The interface is deliberately small. It carries the four things North
// actually needs: streamed conversation, schema-constrained generation for
// anything actionable, file upload for video and documents, and a name.
package ai

import (
	"context"
	"io"
	"time"
)

// Role identifies who produced a message.
//
// "model" rather than "assistant" because North is a coach, and because both
// Gemini and the OpenAI-compatible providers can be mapped onto it without
// either vocabulary winning.
type Role string

const (
	RoleUser  Role = "user"
	RoleModel Role = "model"
)

// Part is one piece of a message. Exactly one of the fields carries content.
//
// Small images travel inline; video and large documents are uploaded first and
// referenced by URI, because inlining a training video would blow past every
// provider's request limit.
type Part struct {
	Text string

	// InlineData is raw bytes sent with the request. Keep it small.
	InlineData []byte

	// FileURI references a file already uploaded through UploadFile.
	FileURI string

	// MIMEType is required whenever InlineData or FileURI is set.
	MIMEType string
}

func TextPart(text string) Part { return Part{Text: text} }

func FilePart(uri, mimeType string) Part {
	return Part{FileURI: uri, MIMEType: mimeType}
}

// Message is one turn in a conversation.
//
// Parts, ToolCalls, and ToolResults are alternatives, not a mixture: a turn is
// content, or the model asking for tools, or the answers coming back. Every
// provider models it that way, and pretending otherwise here would only move
// the split into each adapter.
type Message struct {
	Role  Role
	Parts []Part

	// ToolCalls is set on a model turn that asked for tools to be run.
	ToolCalls []ToolCall

	// ToolResults is set on the user turn carrying their answers back.
	ToolResults []ToolResult
}

func UserText(text string) Message {
	return Message{Role: RoleUser, Parts: []Part{TextPart(text)}}
}

func ModelText(text string) Message {
	return Message{Role: RoleModel, Parts: []Part{TextPart(text)}}
}

// Text flattens a message's text parts, ignoring attachments.
func (m Message) Text() string {
	var out string
	for _, p := range m.Parts {
		out += p.Text
	}
	return out
}

// Request is a single call to a model.
type Request struct {
	// Model is the provider-specific identifier ("gemini-2.5-pro",
	// "anthropic/claude-sonnet-4.5"). Empty means the client's default.
	Model string

	// System is the instruction block: persona, grounding rules, and the
	// context the coach is allowed to rely on.
	System string

	Messages []Message

	// ResponseSchema constrains the reply to JSON matching this shape. When it
	// is set the provider enforces the structure, which is how North gets a
	// workout plan it can parse instead of prose it has to guess at.
	ResponseSchema *Schema

	// Tools the model may call. Empty — the default — means a plain reply, so
	// every existing call site is unaffected by tool support existing.
	//
	// Mutually exclusive with ResponseSchema in practice: a provider asked for
	// both is being told to answer in a fixed shape and to call something
	// instead, and they disagree about which wins.
	Tools []Tool

	// Temperature is nil for the provider default. Structured generation should
	// set it low; conversation should usually leave it alone.
	Temperature *float32

	MaxTokens int
}

// Response is a completed non-streaming reply.
type Response struct {
	Text  string
	Usage Usage

	// ToolCalls is non-empty when the model asked for tools instead of
	// answering. Text is usually empty then, and a caller that ignores this
	// field sees a reply that stopped mid-thought.
	ToolCalls []ToolCall

	// FinishReason is the provider's own string, normalised only enough to
	// detect truncation. Callers should treat it as diagnostic.
	FinishReason string
}

// Usage reports token consumption, for cost tracking and for noticing when a
// context window is filling up.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// StreamChunk is one piece of a streamed reply.
//
// A chunk carries either text or an error, never both. The channel is closed
// when the reply is complete; a chunk with a non-nil Err is always the last.
type StreamChunk struct {
	Text  string
	Usage *Usage
	Err   error

	// ToolCalls arrives as a single chunk once the model has finished asking.
	// Not streamed piecewise: a half-decoded argument object is of no use to
	// anyone, and every provider only makes the call actionable when complete.
	ToolCalls []ToolCall
}

// File is a provider-side upload.
type File struct {
	URI      string
	MIMEType string

	// ExpiresAt is when the provider will delete the file. Gemini keeps
	// uploads for a couple of days, so anything North needs long-term must
	// live in North's own object storage and be re-uploaded on demand.
	ExpiresAt time.Time
}

// UploadRequest describes a file to send to the provider.
type UploadRequest struct {
	Name     string
	MIMEType string
	Reader   io.Reader
}

// Client is the interface every provider implements.
//
// Implementations must be safe for concurrent use: one client is built at
// startup and shared by every request.
type Client interface {
	// Name is the registry key ("gemini", "openrouter", "fake").
	Name() string

	// Chat streams a reply. The returned channel is always closed by the
	// implementation, including on error, so a caller may safely range over it.
	Chat(ctx context.Context, req Request) (<-chan StreamChunk, error)

	// Generate returns a complete reply in one call. This is the path for
	// structured output: a schema-constrained answer has to be parsed as a
	// whole, so streaming it would buy nothing.
	Generate(ctx context.Context, req Request) (*Response, error)

	// UploadFile stores a file with the provider and blocks until it is ready
	// to reference. Providers that need no upload step may return a File whose
	// URI is empty, and callers should then inline the bytes instead.
	UploadFile(ctx context.Context, req UploadRequest) (*File, error)
}

// Embedder turns text into vectors.
//
// Separate from Client, and type-asserted for rather than required, because
// most of what North talks to does not do embeddings: OpenRouter proxies chat
// models, xAI has no embedding endpoint. Putting Embed on Client would force
// four providers to implement a method that answers "not supported", and every
// caller would still have to handle that — the same check, moved somewhere it
// reads worse.
type Embedder interface {
	// Name identifies the provider, so a stored vector can record what
	// produced it.
	Name() string

	// EmbedModel is the model these vectors come from. A vector is only
	// comparable with others from the same model, so it is stored alongside
	// them and a change to it invalidates everything that came before.
	EmbedModel() string

	// Dimensions is the length of every vector returned. Fixed per model, and
	// checked against the database column at startup rather than discovered
	// when the first insert fails.
	Dimensions() int

	// EmbedQuery embeds a search query rather than a passage.
	//
	// Separate from Embed because retrieval models are usually asymmetric:
	// they are trained with a different prefix for the question and for the
	// thing being searched. Using the passage side for both still returns
	// results, just worse ones, which is the kind of bug nobody finds.
	EmbedQuery(ctx context.Context, query string) ([]float32, error)

	// Embed returns one vector per input, in the same order.
	//
	// Batched because the cost is dominated by the round trip: embedding a
	// hundred passages one at a time is a hundred times the latency of
	// embedding them together, for identical tokens.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
