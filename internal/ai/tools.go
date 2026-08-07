package ai

import "encoding/json"

// Tool is a capability the model may invoke.
//
// The shape is deliberately the same one every provider agrees on: a name, a
// sentence about when to use it, and a JSON Schema for its arguments. Anything
// richer would be portable in theory and rejected by someone in practice — the
// same reasoning as Schema's, in schema.go.
//
// Parameters reuses Schema rather than introducing a second description of a
// JSON shape, so a tool's arguments and a structured response are constrained
// by the same code and the same provider translation.
type Tool struct {
	Name string

	// Description is what the model reads to decide whether to call this. It
	// is the whole of the model's knowledge about the tool, so it should say
	// when to use it, not merely what it does.
	Description string

	Parameters *Schema
}

// ToolCall is the model asking for a tool to be run.
type ToolCall struct {
	// ID correlates a call with its result. Providers that supply one (the
	// OpenAI-compatible APIs) round-trip it; Gemini has no such field, so its
	// adapter synthesises one. Callers must echo whatever they were given.
	ID string

	Name string

	// Arguments is a JSON object matching the tool's Parameters schema.
	// Raw rather than decoded because only the tool knows its own shape, and
	// decoding here would mean a second copy of it.
	Arguments json.RawMessage
}

// ToolResult is what running a tool produced, on its way back to the model.
type ToolResult struct {
	// ID must match the ToolCall this answers.
	ID   string
	Name string

	// Content is the result as text. Text rather than a typed value because
	// the model reads it: a tool that returns "3 goals, all on track" is more
	// useful than one returning a struct the model has to interpret.
	Content string

	// IsError marks a failed call. The model is told rather than shielded:
	// "that goal does not exist" is something it can recover from by asking,
	// where a silent empty result invites it to invent an answer.
	IsError bool
}

// HasToolCalls reports whether a message is the model asking for tools.
func (m Message) HasToolCalls() bool { return len(m.ToolCalls) > 0 }

// ToolCallMessage builds the model turn recording what it asked for. It must
// be appended to the history before the matching results, because every
// provider requires a call to precede its result.
func ToolCallMessage(calls []ToolCall) Message {
	return Message{Role: RoleModel, ToolCalls: calls}
}

// ToolResultMessage builds the user turn carrying results back.
func ToolResultMessage(results []ToolResult) Message {
	return Message{Role: RoleUser, ToolResults: results}
}
