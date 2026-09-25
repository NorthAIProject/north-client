package ai_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
)

func TestJSONReplyUnwrapsWhatChatModelsSend(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"bare object":        {`{"a":1}`, `{"a":1}`},
		"bare array":         {` [1,2] `, `[1,2]`},
		"json fence":         {"```json\n{\"a\":1}\n```", `{"a":1}`},
		"plain fence":        {"```\n{\"a\":1}\n```", `{"a":1}`},
		"prose then fence":   {"Here is your plan:\n```json\n{\"a\":{\"b\":2}}\n```\nEnjoy!", `{"a":{"b":2}}`},
		"prose around":       {"I made this plan: {\"a\":1} Let me know.", `{"a":1}`},
		"no json at all":     {"I cannot help with that.", "I cannot help with that."},
		"unterminated fence": {"```json\n{\"a\":1}", `{"a":1}`},
	} {
		if got := string(ai.JSONReply(tc.in)); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}
