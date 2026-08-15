// Package mcpserver exposes North's coaching capabilities over the Model
// Context Protocol, so an outside agent — Hermes on the VPS, Claude Code, any
// MCP client — can use them as tools.
//
// What is exposed is business capability, not schema: "log a check-in", not
// "insert a row into check_ins". A tool that leaked internal identifiers or
// table shapes would tie every consumer to North's storage, which is the thing
// the layering exists to prevent.
//
// This is a transport, so the handlers here obey the same rule as the HTTP
// ones: parse arguments, call a service, render the answer. No business logic.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/agent"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/shared/toolsurface"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Services are the slices the tools call into. They are the same constructors
// cmd/web uses; nothing here is built specially for MCP.
type Services struct {
	Users    *users.Service
	Goals    *goals.Service
	CheckIns *checkins.Service
	Memories *memories.Service

	// Documents is the person's own notes and uploads. Nil in a process with
	// no index behind it, which the tools check rather than assume.
	Documents *documents.Service
	Activity  *activity.Service
	Coach     *coach.Service

	// Agent is the capability registry the coach's own tool loop reads. Its
	// tools are published here too, so a capability defined once is reachable
	// from both the coach mid-conversation and an outside MCP client.
	//
	// The tools defined in this package are the ones that have no capability
	// yet; they belong in the registry, and moving them there is the follow-up
	// that removes this split.
	Agent *agent.Registry
}

// Register wires every tool onto a server, acting as the given user.
//
// The user is fixed for the life of the session rather than passed per call:
// letting a caller name the user it wants to act as would make the bearer token
// an authorisation bypass.
func Register(s *mcp.Server, svc Services, user users.User) {
	registerAgentCapabilities(s, svc.Agent, user)
	registerGoals(s, svc, user)
	registerCheckIns(s, svc, user)
	registerKnowledge(s, svc, user)
	registerFitness(s, svc, user)
	registerCoach(s, svc, user)
}

// registerAgentCapabilities publishes the shared registry over MCP.
//
// Server.AddTool rather than the generic mcp.AddTool: the argument schema is
// already described by ai.Schema, and going through a Go type just to have the
// SDK infer the schema back would mean two descriptions of every tool's
// arguments — the duplication internal/agent exists to avoid.
func registerAgentCapabilities(s *mcp.Server, registry *agent.Registry, user users.User) {
	if registry == nil {
		return
	}

	for _, tool := range registry.Tools() {
		schema, err := json.Marshal(ai.JSONSchema(tool.Parameters))
		if err != nil {
			// Only reachable if a capability declares a schema that cannot be
			// marshalled, which is a programming error present at startup.
			panic("mcpserver: cannot marshal the schema for " + tool.Name + ": " + err.Error())
		}

		s.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: json.RawMessage(schema),

			// Carried through from the capability. Without it every registry
			// tool arrived unannotated, so a client in read-only mode could not
			// tell search_exercises from calculate_macros — which saves the plan
			// it computes.
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:   registry.IsReadOnly(tool.Name),
				IdempotentHint: registry.IsIdempotent(tool.Name),
			},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// The user was fixed when the session authenticated. Nothing in the
			// request can change it.
			result := registry.Invoke(toolsurface.With(ctx, toolsurface.MCP), user.ID, ai.ToolCall{
				ID:        tool.Name,
				Name:      tool.Name,
				Arguments: req.Params.Arguments,
			})

			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: result.Content}},
				IsError: result.IsError,
			}, nil
		})
	}
}

func registerGoals(s *mcp.Server, svc Services, user users.User) {
	type searchArgs struct {
		Query      string `json:"query,omitempty" jsonschema:"case-insensitive text to match against goal titles and motivations; omit to list all"`
		ActiveOnly bool   `json:"active_only,omitempty" jsonschema:"restrict to goals still in progress"`
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_goals",
		Description: "Search the user's goals by text, optionally restricted to those still active. Returns title, category, status, progress, and target date.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		var (
			found []goals.Goal
			err   error
		)
		if args.ActiveOnly {
			found, err = svc.Goals.ListActive(ctx, user.ID)
		} else {
			found, err = svc.Goals.List(ctx, user.ID)
		}
		if err != nil {
			return fail(err), nil, nil
		}

		return structured(matchGoals(found, args.Query))
	})
}

func registerCheckIns(s *mcp.Server, svc Services, user users.User) {
	type listArgs struct {
		Limit int `json:"limit,omitempty" jsonschema:"how many recent check-ins to return, default 7"`
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_check_ins",
		Description: "Return the user's recent daily check-ins, newest first, with mood, energy, wins, challenges, and notes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args listArgs) (*mcp.CallToolResult, any, error) {
		limit := args.Limit
		if limit <= 0 {
			limit = 7
		}

		entries, err := svc.CheckIns.List(ctx, user.ID, limit)
		if err != nil {
			return fail(err), nil, nil
		}

		out := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			out = append(out, map[string]any{
				"date":       e.LocalDate.Format(time.DateOnly),
				"mood":       e.Mood,
				"energy":     e.Energy,
				"wins":       e.Wins,
				"challenges": e.Challenges,
				"notes":      e.Notes,
			})
		}

		return structured(out)
	})
}

func registerKnowledge(s *mcp.Server, svc Services, user users.User) {
	type searchArgs struct {
		Query string `json:"query,omitempty" jsonschema:"text to match; omit to return everything the coach remembers"`
		Limit int    `json:"limit,omitempty" jsonschema:"maximum results, default 20"`
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "search_knowledge",
		Description: "Search what North has remembered about the user — the durable facts, preferences, and " +
			"constraints the coach reasons from. Only approved memories are returned.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}

		// Approved only: a pending memory is one the user has not confirmed,
		// and handing it to an outside agent as fact would put words in their
		// mouth.
		found, err := svc.Memories.ListApproved(ctx, user.ID)
		if err != nil {
			return fail(err), nil, nil
		}

		out := make([]map[string]any, 0, len(found))
		for _, m := range found {
			if !matches(args.Query, m.Content) {
				continue
			}
			out = append(out, map[string]any{"memory": m.Content, "pinned": m.Pinned})
			if len(out) == limit {
				break
			}
		}

		return structured(out)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "knowledge_status",
		Description: "Report what North has actually read: document counts by state, how many passages " +
			"are indexed, which documents have changed since they were last read, and what the last " +
			"indexing pass did. Use it before concluding the user has not told North something.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		if svc.Documents == nil {
			return fail(errors.New("this server has no document index")), nil, nil
		}

		counts, err := svc.Documents.Counts(ctx, user.ID)
		if err != nil {
			return fail(err), nil, nil
		}

		status := map[string]any{
			"documents_read":       counts.Ready,
			"documents_waiting":    counts.Pending,
			"documents_unreadable": counts.Failed,
			"documents_stale":      counts.Stale,
			"passages_indexed":     counts.Chunks,
		}

		// A missing run is not an error: it means nothing has ever been
		// indexed, which is itself the answer.
		if run, err := svc.Documents.LatestRun(ctx, user.ID); err == nil {
			status["last_index_run"] = map[string]any{
				"started_at": run.StartedAt.UTC().Format(time.RFC3339),
				"succeeded":  run.Success,
				"seen":       run.Seen,
				"unchanged":  run.Unchanged,
				"failed":     run.Failed,
				"warnings":   run.Warnings,
			}
		}

		return structured(status)
	})
}

func registerFitness(s *mcp.Server, svc Services, user users.User) {
	type summaryArgs struct {
		Days int `json:"days,omitempty" jsonschema:"how many days back to summarise, default 7"`
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_fitness_summary",
		Description: "Summarise the user's recent training: sessions logged, total calories, and whether something is in progress right now.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args summaryArgs) (*mcp.CallToolResult, any, error) {
		days := args.Days
		if days <= 0 {
			days = 7
		}
		since := time.Now().AddDate(0, 0, -days)

		sessions, err := svc.Activity.List(ctx, user.ID, 50)
		if err != nil {
			return fail(err), nil, nil
		}

		calories, err := svc.Activity.TotalCaloriesSince(ctx, user.ID, since)
		if err != nil {
			return fail(err), nil, nil
		}

		_, inProgress, err := svc.Activity.Active(ctx, user.ID)
		if err != nil {
			return fail(err), nil, nil
		}

		recent := 0
		for _, s := range sessions {
			if s.StartedAt.After(since) {
				recent++
			}
		}

		return structured(map[string]any{
			"days":                days,
			"sessions":            recent,
			"calories":            calories,
			"session_in_progress": inProgress,
		})
	})
}

func registerCoach(s *mcp.Server, svc Services, user users.User) {
	type askArgs struct {
		Message string `json:"message" jsonschema:"what to ask the coach, in the user's voice"`
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "ask_coach",
		Description: "Ask North's coach a question. The reply is grounded in the user's goals, check-ins, " +
			"training, and remembered facts, and the exchange is saved to their history like any other conversation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args askArgs) (*mcp.CallToolResult, any, error) {
		conversation, err := svc.Coach.StartConversation(ctx, user.ID)
		if err != nil {
			return fail(err), nil, nil
		}

		stream, err := svc.Coach.SendMessage(ctx, user, conversation.ID, args.Message)
		if err != nil {
			return fail(err), nil, nil
		}

		// Collected rather than streamed: a tool call is one request and one
		// answer. The coach still persists the turn as it goes.
		var reply string
		for chunk := range stream {
			if chunk.Err != nil {
				return fail(chunk.Err), nil, nil
			}
			reply += chunk.Text
		}

		return textResult(reply, false), nil, nil
	})
}

func matchGoals(all []goals.Goal, query string) []map[string]any {
	out := make([]map[string]any, 0, len(all))
	for _, g := range all {
		if !matches(query, g.Title, g.Motivation) {
			continue
		}

		entry := map[string]any{
			"title":      g.Title,
			"category":   g.Category,
			"status":     g.Status,
			"motivation": g.Motivation,
		}
		if pct, ok := g.Progress(); ok {
			entry["progress"] = pct
		}
		if !g.TargetDate.IsZero() {
			entry["target_date"] = g.TargetDate.Format(time.DateOnly)
		}
		out = append(out, entry)
	}
	return out
}

// matches reports whether any field contains query, case-insensitively. An
// empty query matches everything, so a tool called with no arguments lists
// rather than returns nothing.
func matches(query string, fields ...string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), query) {
			return true
		}
	}
	return false
}

// structured returns a result an agent can read as data as well as prose.
//
// Every tool used to end `return jsonResult(v), nil, nil`, so structuredContent
// was never populated and a caller received JSON it had to parse back out of a
// text block. That was tolerable while the answers were flat numbers. It
// stopped being tolerable once retrieval started carrying citations: a ref an
// agent has to recover with a regular expression is a ref it will get wrong.
//
// The text block stays alongside it, because clients predating structured
// content read that and nothing else.
func structured[T any](v T) (*mcp.CallToolResult, any, error) {
	return jsonResult(v), v, nil
}

func jsonResult(v any) *mcp.CallToolResult {
	encoded, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textResult("could not encode the result", true)
	}
	return textResult(string(encoded), false)
}

func textResult(text string, isError bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: isError,
	}
}

// fail reports a tool failure to the model rather than to the transport.
//
// Returned as a result with IsError set, not as a Go error: a protocol-level
// error aborts the call, while this lets the agent read what went wrong and try
// something else.
func fail(err error) *mcp.CallToolResult {
	return textResult(err.Error(), true)
}
