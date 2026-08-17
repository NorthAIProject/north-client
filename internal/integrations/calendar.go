package integrations

import (
	"context"
	"strings"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// horizon is how far ahead the calendar summary looks.
//
// Seven days, per the ticket. Far enough that "you have three sessions booked
// this week" is a real observation; near enough that it is still about now.
const horizon = 7 * 24 * time.Hour

// calendarToolNames are the tool names North will use, most preferred first.
//
// A list rather than one name because there is no standard for this: every
// calendar MCP server spells it differently. Matching on a small set of known
// spellings, and falling back to a substring match, is what lets somebody
// connect a server North has never seen without North shipping a plugin for it.
var calendarToolNames = []string{
	"list_events",
	"get_events",
	"list_calendar_events",
	"search_events",
	"calendar_list_events",
}

// CalendarAdapter turns an MCP server's answer into summary strings.
//
// This is the provider-specific half, and the seam a Google-specific adapter
// would sit beside: it decides which tool to call and how to read the reply,
// while Client knows only how to speak MCP.
type CalendarAdapter struct {
	client *Client
}

func NewCalendarAdapter(client *Client) *CalendarAdapter {
	return &CalendarAdapter{client: client}
}

// Upcoming returns a summary of the next seven days.
//
// The returned strings are what the coach sees. Deliberately prose, not
// structure: the coach reads context as text, and a shape here would be North
// pretending to own a calendar model it does not.
func (a *CalendarAdapter) Upcoming(ctx context.Context, endpoint, token string, now time.Time) ([]string, error) {
	session, err := a.client.Connect(ctx, endpoint, token)
	if err != nil {
		return nil, err
	}
	// Nothing useful to do with a close error on a read: the answer is already
	// in hand, and the connection is discarded either way.
	defer func() { _ = session.Close() }()

	tools, err := session.ToolNames(ctx)
	if err != nil {
		return nil, err
	}

	name, ok := pickCalendarTool(tools)
	if !ok {
		return nil, apperr.Wrap(apperr.ErrValidation,
			"that server exposes no tool this can read a calendar from")
	}

	// Times as RFC3339, which is what every calendar server this was tried
	// against expects. A server wanting something else will error, and the
	// context source degrades — which is the documented behaviour, not a crash.
	text, err := session.Call(ctx, name, map[string]any{
		"start":       now.Format(time.RFC3339),
		"end":         now.Add(horizon).Format(time.RFC3339),
		"timeMin":     now.Format(time.RFC3339),
		"timeMax":     now.Add(horizon).Format(time.RFC3339),
		"max_results": 25,
	})
	if err != nil {
		return nil, err
	}

	return summarize(text), nil
}

// pickCalendarTool chooses which of a server's tools to call.
func pickCalendarTool(available []string) (string, bool) {
	for _, want := range calendarToolNames {
		for _, have := range available {
			if strings.EqualFold(have, want) {
				return have, true
			}
		}
	}

	// Nothing known matched. Anything that mentions both listing and events is
	// a better guess than giving up.
	for _, have := range available {
		lower := strings.ToLower(have)
		if strings.Contains(lower, "event") && (strings.Contains(lower, "list") || strings.Contains(lower, "get")) {
			return have, true
		}
	}
	return "", false
}

// summarize turns the server's text into lines for the coach.
//
// No parsing of anybody's JSON schema: MCP tools answer in text meant to be
// read by a model, and that is exactly what this is passed to. Splitting into
// lines and trimming is all the structure North needs, and all it can rely on
// across servers it has never seen.
func summarize(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
		if len(out) >= maxCalendarLines {
			break
		}
	}
	return out
}

// maxCalendarLines bounds what one external server can put into the coach's
// prompt. Somebody else's calendar must not be able to crowd out North's own
// context, however many events it returns.
const maxCalendarLines = 25
