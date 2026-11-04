package conversations_test

import (
	"reflect"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
)

// The window is a plain LIMIT, so it can start anywhere in a tool turn. Every
// provider refuses a result whose call it has not been shown, and the thread
// stays broken until the window slides past the cut, so what the window starts
// with has to be something a provider can read.
func TestToAIMessagesTrimsToolRowsAtTheWindowEdge(t *testing.T) {
	t.Parallel()

	calls := func(id string) conversations.Message {
		return conversations.Message{Role: ai.RoleModel, ToolCalls: []ai.ToolCall{{ID: id, Name: "get_goals"}}}
	}
	results := func(id string) conversations.Message {
		return conversations.Message{Role: ai.RoleUser, ToolResults: []ai.ToolResult{{ID: id, Name: "get_goals", Content: "ok"}}}
	}
	user := conversations.Message{Role: ai.RoleUser, Content: "how am I doing?"}
	reply := conversations.Message{Role: ai.RoleModel, Content: "Well."}
	blank := conversations.Message{Role: ai.RoleUser, Content: "  "}

	cases := []struct {
		name   string
		window []conversations.Message
		want   []string
	}{
		{"a result whose call fell out", []conversations.Message{results("a"), reply, user}, []string{"text", "text"}},
		{"several rows of the same cut", []conversations.Message{results("a"), results("b"), user}, []string{"text"}},
		{"a call whose result never landed", []conversations.Message{calls("a"), user, reply}, []string{"text", "text"}},
		{"a pair with no question before it", []conversations.Message{results("a"), calls("b"), results("b"), reply, user}, []string{"text", "text"}},
		{"an empty turn is not somewhere to stop", []conversations.Message{results("a"), blank, results("b"), user}, []string{"text"}},
		{"a tool turn after the first plain one stays", []conversations.Message{user, calls("a"), results("a"), reply}, []string{"text", "calls", "results", "text"}},
		// A chain of approvals can fill the window with no words at all. Dropping
		// every row would send an empty request, so only the orphans go.
		{"a window with nothing plain in it keeps its pairs", []conversations.Message{results("a"), calls("b"), results("b")}, []string{"calls", "results"}},
		{"nothing but orphans leaves nothing", []conversations.Message{results("a")}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := []string{}
			for _, m := range conversations.ToAIMessages(tc.window) {
				switch {
				case len(m.ToolCalls) > 0:
					got = append(got, "calls")
				case len(m.ToolResults) > 0:
					got = append(got, "results")
				default:
					got = append(got, "text")
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("turns = %v, want %v", got, tc.want)
			}
		})
	}
}
