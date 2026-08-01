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
type Message struct {
	Role  Role
	Parts []Part
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

	// Temperature is nil for the provider default. Structured generation should
	// set it low; conversation should usually leave it alone.
	Temperature *float32

	MaxTokens int
}

// Response is a completed non-streaming reply.
type Response struct {
	Text  string
	Usage Usage

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
