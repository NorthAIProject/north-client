package integrations

import (
	"context"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// dialTimeout bounds one connect-and-call round trip.
//
// Short on purpose. This runs while somebody waits for a coach reply, and a
// calendar that cannot answer in a few seconds is worth doing without — the
// context source degrades, the reply still happens.
const dialTimeout = 8 * time.Second

// Client is North talking to somebody else's MCP server.
//
// Deliberately generic: it knows how to connect, list tools and call one. It
// knows nothing about calendars. That keeps the provider-specific part (which
// tool to call, how to read what comes back) in one adapter that a second
// provider can sit beside without touching this.
type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: dialTimeout}}
}

// Session is one short-lived connection. Callers must Close it.
type Session struct {
	cs *mcp.ClientSession
}

// Connect opens a session against an MCP server.
//
// Short-lived by design: one connection per use, closed straight after. North
// has no reason to hold an idle socket open per user per server, and the
// standalone SSE stream that would keep one alive is turned off for the same
// reason — nothing here reacts to server-initiated notifications.
func (c *Client) Connect(ctx context.Context, endpoint, token string) (*Session, error) {
	if endpoint == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "no endpoint configured")
	}

	httpClient := c.http
	if token != "" {
		httpClient = &http.Client{
			Timeout:   dialTimeout,
			Transport: bearer{token: token, base: http.DefaultTransport},
		}
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "north", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, apperr.Wrap(err, "connect to the MCP server")
	}
	return &Session{cs: cs}, nil
}

func (s *Session) Close() error {
	if s == nil || s.cs == nil {
		return nil
	}
	return s.cs.Close()
}

// ToolNames is what the server says it can do.
//
// Names only: the adapters choose a tool by name, and returning the SDK's own
// types would spread the MCP dependency into every provider adapter.
func (s *Session) ToolNames(ctx context.Context) ([]string, error) {
	res, err := s.cs.ListTools(ctx, nil)
	if err != nil {
		return nil, apperr.Wrap(err, "list MCP tools")
	}
	out := make([]string, 0, len(res.Tools))
	for _, t := range res.Tools {
		out = append(out, t.Name)
	}
	return out, nil
}

// Call runs one tool and returns its text content.
//
// Text only: North puts summary strings in front of the coach, so structured
// content it cannot render is not worth carrying up the stack.
func (s *Session) Call(ctx context.Context, name string, args map[string]any) (string, error) {
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", apperr.Wrap(err, "call MCP tool %s", name)
	}
	if res.IsError {
		return "", apperr.Wrap(apperr.ErrUnavailable, "the MCP server refused the %s call", name)
	}

	var out string
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			out += text.Text
		}
	}
	return out, nil
}

// bearer attaches the stored token to every request.
type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	// Cloned: RoundTrip must not modify the request it is given.
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(clone)
}
