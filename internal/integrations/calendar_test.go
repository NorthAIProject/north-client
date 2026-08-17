package integrations_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NorthAIProject/north-client/internal/integrations"
)

// stubServer is a real MCP server over real HTTP, exposing one calendar tool.
//
// A stub rather than a mock: the point of these tests is that North's client
// speaks the protocol correctly, and a hand-written fake of the transport would
// only prove North agrees with itself.
func stubServer(t *testing.T, toolName, reply string, wantToken string) string {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "stub-calendar", Version: "test"}, nil)
	server.AddTool(&mcp.Tool{
		Name:        toolName,
		Description: "list calendar events",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: reply}},
		}, nil
	})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantToken != "" && r.Header.Get("Authorization") != "Bearer "+wantToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

// The whole path: connect, find the tool, call it, reduce to summary strings.
func TestUpcomingReadsEventsFromARealMCPServer(t *testing.T) {
	endpoint := stubServer(t, "list_events",
		"Tue 18 Aug 09:00 — Standup\nWed 19 Aug 18:30 — Squat session", "")

	adapter := integrations.NewCalendarAdapter(integrations.NewClient())
	lines, err := adapter.Upcoming(context.Background(), endpoint, "", time.Now())
	if err != nil {
		t.Fatalf("upcoming: %v", err)
	}

	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2: %v", len(lines), lines)
	}
	if !strings.Contains(lines[1], "Squat session") {
		t.Fatalf("second line = %q", lines[1])
	}
}

// The stored token must reach the server as a bearer header.
func TestUpcomingSendsTheStoredToken(t *testing.T) {
	endpoint := stubServer(t, "list_events", "Thu 20 Aug — Physio", "sekrit")

	adapter := integrations.NewCalendarAdapter(integrations.NewClient())
	lines, err := adapter.Upcoming(context.Background(), endpoint, "sekrit", time.Now())
	if err != nil {
		t.Fatalf("upcoming with token: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}

	// And without it, the same server refuses.
	if _, err := adapter.Upcoming(context.Background(), endpoint, "", time.Now()); err == nil {
		t.Fatal("the server accepted a call with no token")
	}
}

// A server whose tool is spelled differently is still usable.
func TestUpcomingFindsDifferentlyNamedTools(t *testing.T) {
	endpoint := stubServer(t, "calendar_list_events", "Fri 21 Aug — Dentist", "")

	adapter := integrations.NewCalendarAdapter(integrations.NewClient())
	lines, err := adapter.Upcoming(context.Background(), endpoint, "", time.Now())
	if err != nil {
		t.Fatalf("upcoming: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
}

// A server with nothing calendar-shaped fails rather than guessing wildly.
func TestUpcomingRefusesAServerWithNoCalendarTool(t *testing.T) {
	endpoint := stubServer(t, "send_email", "nope", "")

	adapter := integrations.NewCalendarAdapter(integrations.NewClient())
	if _, err := adapter.Upcoming(context.Background(), endpoint, "", time.Now()); err == nil {
		t.Fatal("accepted a server that cannot answer about a calendar")
	}
}

// An unreachable server produces an error, not a panic or a hang.
func TestUpcomingFailsCleanlyOnADeadServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	adapter := integrations.NewCalendarAdapter(integrations.NewClient())
	if _, err := adapter.Upcoming(context.Background(), srv.URL, "", time.Now()); err == nil {
		t.Fatal("a failing server was reported as success")
	}
}

// One external server must not be able to crowd out North's own context.
func TestUpcomingBoundsWhatOneServerCanContribute(t *testing.T) {
	var many strings.Builder
	for i := range 200 {
		many.WriteString("event ")
		many.WriteString(string(rune('a' + i%26)))
		many.WriteString("\n")
	}
	endpoint := stubServer(t, "list_events", many.String(), "")

	adapter := integrations.NewCalendarAdapter(integrations.NewClient())
	lines, err := adapter.Upcoming(context.Background(), endpoint, "", time.Now())
	if err != nil {
		t.Fatalf("upcoming: %v", err)
	}
	if len(lines) > 25 {
		t.Fatalf("lines = %d, want at most 25", len(lines))
	}
}
