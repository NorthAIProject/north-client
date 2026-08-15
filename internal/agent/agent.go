// Package agent is the one place North's capabilities are defined for a model
// to call.
//
// The coach's chat loop and the MCP server both read this registry. That is
// the point of it: two definitions of "calculate my macros" — one for the
// chatbot, one for MCP — would drift within a month, and the drift would show
// up as the coach and Telegram giving different answers to the same question.
//
// Capabilities are business operations, not database access. "Search goals" and
// "log this food" belong here; "run this SQL" and "give me row 41" do not. See
// CLAUDE.md's MCP section — the rule holds for the coach's tools too, because
// they are the same tools.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/toolsurface"
)

// Capability is one thing a model can ask North to do.
type Capability struct {
	// Tool is what the model is shown: the name, when to use it, and the
	// shape of its arguments.
	Tool ai.Tool

	// ReadOnly reports that invoking this capability changes nothing.
	//
	// It exists because the MCP surface publishes these tools, and a client
	// running in read-only mode decides what it may call from the annotation
	// alone. Unannotated, calculate_macros — which saves the plan it computes
	// — is indistinguishable from search_exercises, so a client honouring
	// read-only would either refuse everything or write when told not to.
	//
	// The default is the safe one: a capability that says nothing is treated as
	// though it writes.
	ReadOnly bool

	// Idempotent reports that calling this twice with the same arguments leaves
	// the same state as calling it once.
	//
	// Published to MCP clients, which read it to decide whether a call that
	// timed out is safe to retry. create_check_in is the example: it upserts
	// the day's entry, so a retry corrects rather than duplicating.
	//
	// The default is again the safe one — a capability that says nothing is
	// assumed to do something more on a second call.
	Idempotent bool

	// Invoke runs it for a specific person.
	//
	// userID is passed by the caller from an authenticated session, never
	// taken from args. A model that could name its own user id would be one
	// prompt away from reading someone else's data.
	Invoke func(ctx context.Context, userID uuid.UUID, args json.RawMessage) (string, error)
}

// Record is one capability run, reported to whoever is keeping the account of
// what North has done.
//
// Deliberately not internal/toolaudit's own type: that package imports nothing
// from here, and this one must not import it, or the slice that records would
// depend on the slice that runs. The surface is a plain string for the same
// reason.
type Record struct {
	UserID    uuid.UUID
	Tool      string
	Arguments json.RawMessage
	Surface   string
	Failed    bool

	// Detail is what the capability said, or the reason it failed — the same
	// text the model was given.
	Detail string
}

// Recorder keeps the account. Optional: a registry without one still runs.
type Recorder interface {
	RecordToolRun(ctx context.Context, rec Record)
}

// Registry holds the capabilities available to a model.
//
// Not safe for concurrent registration: it is built once at startup, then only
// read. Building it lazily would mean a request could observe it half-formed.
type Registry struct {
	capabilities map[string]Capability
	recorder     Recorder
}

func NewRegistry() *Registry {
	return &Registry{capabilities: map[string]Capability{}}
}

// Record attaches the account-keeper. Called once at startup, like Register.
func (r *Registry) Record(recorder Recorder) { r.recorder = recorder }

// Register adds a capability. A duplicate name panics rather than overwriting:
// this runs at startup, and two capabilities answering to one name is a wiring
// mistake that would otherwise surface as one of them silently never running.
func (r *Registry) Register(capabilities ...Capability) {
	for _, capability := range capabilities {
		if capability.Tool.Name == "" {
			panic("agent: a capability with no name")
		}
		if _, taken := r.capabilities[capability.Tool.Name]; taken {
			panic("agent: two capabilities named " + capability.Tool.Name)
		}
		if capability.Invoke == nil {
			panic("agent: capability " + capability.Tool.Name + " has no Invoke")
		}
		r.capabilities[capability.Tool.Name] = capability
	}
}

// Tools returns every capability's declaration, in a stable order.
//
// Sorted because it goes into a prompt: an order that changed between requests
// would defeat provider-side prompt caching for no benefit.
func (r *Registry) Tools() []ai.Tool {
	tools := make([]ai.Tool, 0, len(r.capabilities))
	for _, capability := range r.capabilities {
		tools = append(tools, capability.Tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

// IsReadOnly reports whether invoking the named capability changes nothing.
//
// An unknown name answers false. A caller asking about a tool that does not
// exist is already wrong, and the safe wrong answer is "assume it writes".
func (r *Registry) IsReadOnly(name string) bool {
	capability, ok := r.capabilities[name]
	return ok && capability.ReadOnly
}

// IsIdempotent reports whether calling the named capability twice leaves the
// same state as calling it once.
//
// An unknown name answers false, for the same reason IsReadOnly does: the safe
// wrong answer is "assume a second call does something more".
func (r *Registry) IsIdempotent(name string) bool {
	capability, ok := r.capabilities[name]
	return ok && capability.Idempotent
}

// Names returns the registered capability names, sorted.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.capabilities))
	for name := range r.capabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Invoke runs one call and returns its result.
//
// Every failure comes back as a ToolResult with IsError set rather than as an
// error, because the model is the one that has to recover: told "no goal by
// that name", it can ask; handed nothing, it invents an answer. The error
// still reaches the caller's logs through the returned content.
func (r *Registry) Invoke(ctx context.Context, userID uuid.UUID, call ai.ToolCall) ai.ToolResult {
	result := ai.ToolResult{ID: call.ID, Name: call.Name}

	capability, known := r.capabilities[call.Name]
	if !known {
		result.Content = fmt.Sprintf("There is no tool called %q. Available tools: %v.", call.Name, r.Names())
		result.IsError = true
		return result
	}

	content, err := capability.Invoke(ctx, userID, call.Arguments)

	// Recorded whichever way it went, and only once the capability has actually
	// been reached: an unknown tool never ran, so there is nothing to answer
	// for. The account is kept here because this is the one line both the
	// coach and the MCP server pass through.
	if r.recorder != nil {
		detail := content
		if err != nil {
			detail = userFacing(err)
		}
		r.recorder.RecordToolRun(ctx, Record{
			UserID:    userID,
			Tool:      call.Name,
			Arguments: call.Arguments,
			Surface:   toolsurface.From(ctx),
			Failed:    err != nil,
			Detail:    detail,
		})
	}

	if err != nil {
		result.Content = userFacing(err)
		result.IsError = true
		return result
	}

	if content == "" {
		// An empty result reads to a model as "nothing to say", which it will
		// paper over. Saying so explicitly is what stops it inventing rows.
		content = "No results."
	}
	result.Content = content
	return result
}

// InvokeAll runs a batch of calls in order.
//
// Sequentially, not in parallel: the calls in one turn frequently depend on
// each other's side effects, and a model that logs a meal and then asks for
// today's total should see the meal it just logged.
func (r *Registry) InvokeAll(ctx context.Context, userID uuid.UUID, calls []ai.ToolCall) []ai.ToolResult {
	results := make([]ai.ToolResult, 0, len(calls))
	for _, call := range calls {
		results = append(results, r.Invoke(ctx, userID, call))
	}
	return results
}

// userFacing reduces an error to something safe to show a model.
//
// Validation and not-found messages are written for people and are the useful
// half of a failed call. Anything else is a bug or an outage, and its text can
// carry connection strings and query fragments — so it is replaced rather than
// forwarded into a conversation the user can read.
func userFacing(err error) string {
	switch {
	case apperr.Is(err, apperr.ErrValidation), apperr.Is(err, apperr.ErrNotFound), apperr.Is(err, apperr.ErrConflict):
		return err.Error()
	default:
		return "That did not work. Tell the user it failed rather than guessing at an answer."
	}
}

// Decode unmarshals a call's arguments, reporting a validation error the model
// can act on rather than a JSON parser's.
//
// Generic so each capability keeps its own argument struct next to itself,
// which is the only place its shape and its schema can be read together.
func Decode[T any](args json.RawMessage) (T, error) {
	var out T
	if len(args) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(args, &out); err != nil {
		return out, apperr.Wrap(apperr.ErrValidation, "those arguments were not the shape this tool expects")
	}
	return out, nil
}
