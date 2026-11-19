package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/anthropic"
)

// A turn that searches once and answers, as the API streams it: the search,
// its result, then text that cites the result.
var searchTurn = []string{
	evStart,
	`{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{}}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"creatine loading\"}"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[{"type":"web_search_result","url":"https://example.org/creatine","title":"Creatine [review]","encrypted_content":"ENC1","page_age":"May 2026"}]}}`,
	`{"type":"content_block_stop","index":1}`,
	`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":2,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://example.org/creatine","title":"Creatine [review]","encrypted_index":"IDX1","cited_text":"Loading is optional."}}}`,
	`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Loading is optional."}}`,
	`{"type":"content_block_stop","index":2}`,
}

func searchUsage(stop string) string {
	return `{"type":"message_delta","delta":{"stop_reason":"` + stop + `"},"usage":{"output_tokens":30,"server_tool_use":{"web_search_requests":1}}}`
}

func searchingClient(t *testing.T, responses ...cannedResponse) (*fakeAPI, *anthropic.Client) {
	t.Helper()
	return newFakeAPIWith(t, anthropic.Options{
		DefaultModel: "claude-haiku-5-5", Effort: "low", Thinking: "adaptive", Managed: true,
		WebSearchMaxUses: 2,
	}, responses...)
}

func tools(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	list, _ := body["tools"].([]any)
	for _, item := range list {
		out = append(out, item.(map[string]any))
	}
	return out
}

func hasSearchTool(t *testing.T, body map[string]any) bool {
	t.Helper()
	for _, tool := range tools(t, body) {
		if tool["name"] == "web_search" {
			return true
		}
	}
	return false
}

func TestSearchIsOfferedOnlyWhenAllowedSwitchedOnAndOurKey(t *testing.T) {
	ask := func(opts anthropic.Options, req ai.Request) bool {
		f, client := newFakeAPIWith(t, opts, sse(evStart, evTextStart, evText("ok"), evStop0, evMsgDelta("end_turn", 1), evMsgStop))
		ch, err := client.Chat(context.Background(), req)
		if err != nil {
			t.Fatalf("chat: %v", err)
		}
		collect(t, ch)
		return hasSearchTool(t, f.body(t, 0))
	}
	on := anthropic.Options{DefaultModel: "claude-haiku-5-5", Managed: true, WebSearchMaxUses: 2}
	allowed := ai.Request{Messages: []ai.Message{ai.UserText("creatine?")}, AllowWebSearch: true}

	if !ask(on, allowed) {
		t.Error("search was not offered when allowed, switched on and on our key")
	}
	notAllowed := allowed
	notAllowed.AllowWebSearch = false
	if ask(on, notAllowed) {
		t.Error("search was offered on a request that did not allow it")
	}
	off := on
	off.WebSearchMaxUses = 0
	if ask(off, allowed) {
		t.Error("search was offered while switched off")
	}
	// A person's own key is their bill, and a search is charged per use.
	own := on
	own.Managed = false
	if ask(own, allowed) {
		t.Error("search was offered on a person's own key")
	}
}

func TestSearchToolIsTheBasicOneAndCapped(t *testing.T) {
	f, client := searchingClient(t, sse(evStart, evTextStart, evText("ok"), evStop0, evMsgDelta("end_turn", 1), evMsgStop))
	ch, err := client.Chat(context.Background(), ai.Request{
		Messages:       []ai.Message{ai.UserText("creatine?")},
		Tools:          []ai.Tool{{Name: "get_stats", Description: "stats", Parameters: &ai.Schema{Type: "object"}}},
		AllowWebSearch: true,
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	collect(t, ch)

	list := tools(t, f.body(t, 0))
	if len(list) != 2 || list[0]["name"] != "get_stats" {
		t.Fatalf("tools = %v, want the coach's own tool and then search", list)
	}
	search := list[1]
	if search["type"] != "web_search_20250305" || search["max_uses"] != float64(2) {
		t.Errorf("search tool = %v, want web_search_20250305 capped at 2", search)
	}
	// Generate is structured work with no reader to show sources to.
	f2, client2 := searchingClient(t, textMessage("{}"))
	if _, err := client2.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("x")}, AllowWebSearch: true}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if hasSearchTool(t, f2.body(t, 0)) {
		t.Error("Generate offered search")
	}
}

func TestASearchedReplyNamesItsSourcesAndCountsTheSearch(t *testing.T) {
	events := append(append([]string{}, searchTurn...), searchUsage("end_turn"), evMsgStop)
	_, client := searchingClient(t, sse(events...))

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("creatine?")}, AllowWebSearch: true})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var text strings.Builder
	var cited []ai.Source
	var usage *ai.Usage
	sourcesAt, usageAt := -1, -1
	for i, c := range collect(t, ch) {
		if c.Err != nil {
			t.Fatalf("stream error: %v", c.Err)
		}
		text.WriteString(c.Text)
		if len(c.Sources) > 0 {
			cited, sourcesAt = c.Sources, i
		}
		if c.Usage != nil {
			usage, usageAt = c.Usage, i
		}
	}
	if text.String() != "Loading is optional." {
		t.Errorf("text = %q", text.String())
	}
	if len(cited) != 1 || cited[0].URL != "https://example.org/creatine" || cited[0].Title != "Creatine [review]" {
		t.Errorf("sources = %+v", cited)
	}
	if usage == nil || usage.WebSearches != 1 || usage.OutputTokens != 30 {
		t.Errorf("usage = %+v, want one search and 30 out", usage)
	}
	if sourcesAt < 0 || usageAt < sourcesAt {
		t.Errorf("sources at %d, usage at %d: sources come after the text and before usage", sourcesAt, usageAt)
	}
}

// The API can pause a long search turn. The reply carries on from where it
// stopped: one reply to the reader, every request's spend counted.
func TestAPausedSearchTurnIsCarriedOn(t *testing.T) {
	paused := append(append([]string{}, searchTurn[:6]...), searchUsage("pause_turn"), evMsgStop)
	rest := sse(evStart, evTextStart, evText("Loading is optional."), evStop0, evMsgDelta("end_turn", 12), evMsgStop)
	f, client := searchingClient(t, sse(paused...), rest)

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("creatine?")}, AllowWebSearch: true})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var text strings.Builder
	var usage *ai.Usage
	for _, c := range collect(t, ch) {
		if c.Err != nil {
			t.Fatalf("stream error: %v", c.Err)
		}
		text.WriteString(c.Text)
		if c.Usage != nil {
			if usage != nil {
				t.Error("usage arrived more than once")
			}
			usage = c.Usage
		}
	}
	if text.String() != "Loading is optional." {
		t.Errorf("text = %q", text.String())
	}
	// 100 in and 30 out, then 100 in and 12 out.
	if usage == nil || usage.InputTokens != 200 || usage.OutputTokens != 42 || usage.WebSearches != 1 {
		t.Errorf("usage = %+v, want both requests summed", usage)
	}

	// The second request hands the paused turn back whole.
	messages := f.body(t, 1)["messages"].([]any)
	last := messages[len(messages)-1].(map[string]any)
	if last["role"] != "assistant" {
		t.Fatalf("last message = %v, want the paused assistant turn", last)
	}
	blocks := last["content"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("paused turn went back as %d blocks, want the search and its result", len(blocks))
	}
	use, result := blocks[0].(map[string]any), blocks[1].(map[string]any)
	if use["type"] != "server_tool_use" || use["id"] != "srvtoolu_1" || use["input"].(map[string]any)["query"] != "creatine loading" {
		t.Errorf("search went back as %v", use)
	}
	page := result["content"].([]any)[0].(map[string]any)
	if result["type"] != "web_search_tool_result" || page["encrypted_content"] != "ENC1" || page["url"] != "https://example.org/creatine" {
		t.Errorf("result went back as %v", result)
	}
	if !hasSearchTool(t, f.body(t, 1)) {
		t.Error("the continuation dropped the search tool")
	}
}

// A round that searches and then calls one of the coach's tools is replayed
// with the search in it: the API refuses a citation whose result is missing.
func TestASearchInAToolRoundIsReplayedWhole(t *testing.T) {
	round := append(append([]string{}, searchTurn...),
		`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_stats","input":{}}}`,
		`{"type":"content_block_stop","index":3}`,
		searchUsage("tool_use"), evMsgStop)
	answer := sse(evStart, evTextStart, evText("Done."), evStop0, evMsgDelta("end_turn", 2), evMsgStop)
	f, client := searchingClient(t, sse(round...), answer)

	req := ai.Request{
		Messages: []ai.Message{ai.UserText("creatine, and my stats?")}, AllowWebSearch: true,
		Tools: []ai.Tool{{Name: "get_stats", Description: "stats", Parameters: &ai.Schema{Type: "object"}}},
	}
	ch, err := client.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var calls []ai.ToolCall
	var state json.RawMessage
	for _, c := range collect(t, ch) {
		if c.Err != nil {
			t.Fatalf("stream error: %v", c.Err)
		}
		if len(c.ToolCalls) > 0 {
			calls, state = c.ToolCalls, c.ProviderState
		}
	}
	if len(calls) != 1 || len(state) == 0 {
		t.Fatalf("calls = %+v, state = %s", calls, state)
	}

	callMsg := ai.ToolCallMessage(calls)
	callMsg.ProviderState = state
	req.Messages = append(req.Messages, callMsg, ai.ToolResultMessage([]ai.ToolResult{{ID: "toolu_1", Name: "get_stats", Content: "{}"}}))
	ch, err = client.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("second chat: %v", err)
	}
	collect(t, ch)

	messages := f.body(t, 1)["messages"].([]any)
	turn := messages[1].(map[string]any)["content"].([]any)
	var kinds []string
	for _, b := range turn {
		kinds = append(kinds, b.(map[string]any)["type"].(string))
	}
	if strings.Join(kinds, ",") != "server_tool_use,web_search_tool_result,text,tool_use" {
		t.Fatalf("replayed turn = %v", kinds)
	}
	cite := turn[2].(map[string]any)["citations"].([]any)[0].(map[string]any)
	if cite["encrypted_index"] != "IDX1" || cite["url"] != "https://example.org/creatine" {
		t.Errorf("citation went back as %v", cite)
	}
	if page := turn[1].(map[string]any)["content"].([]any)[0].(map[string]any); page["encrypted_content"] != "ENC1" {
		t.Errorf("result went back as %v", page)
	}
}

// A search that fails is a 200 holding an error where the results would be.
// The reply goes on, and the failed search costs nothing and names no source.
func TestAFailedSearchIsNotAFailedReply(t *testing.T) {
	_, client := searchingClient(t, sse(evStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"x"}}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":{"type":"web_search_tool_result_error","error_code":"max_uses_exceeded"}}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"From what I know: optional."}}`,
		`{"type":"content_block_stop","index":2}`,
		evMsgDelta("end_turn", 9), evMsgStop))

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("creatine?")}, AllowWebSearch: true})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var text strings.Builder
	for _, c := range collect(t, ch) {
		if c.Err != nil {
			t.Fatalf("stream error: %v", c.Err)
		}
		if len(c.Sources) > 0 {
			t.Errorf("a failed search named sources: %+v", c.Sources)
		}
		if c.Usage != nil && c.Usage.WebSearches != 0 {
			t.Errorf("a failed search was counted: %+v", c.Usage)
		}
		text.WriteString(c.Text)
	}
	if text.String() != "From what I know: optional." {
		t.Errorf("text = %q", text.String())
	}
}

// Web search switched off for the organisation is a 400 on every request that
// offers it. Khepri has no provider behind this one, so the coach answers
// without the search instead of not at all.
func TestARefusedSearchIsAskedAgainWithoutIt(t *testing.T) {
	refused := cannedResponse{
		status: http.StatusBadRequest,
		body:   `{"type":"error","error":{"type":"invalid_request_error","message":"Web search is not enabled for this organization."}}`,
	}
	f, client := searchingClient(t, refused, sse(evStart, evTextStart, evText("Optional."), evStop0, evMsgDelta("end_turn", 2), evMsgStop))

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("creatine?")}, AllowWebSearch: true})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var text strings.Builder
	for _, c := range collect(t, ch) {
		text.WriteString(c.Text)
	}
	if text.String() != "Optional." {
		t.Errorf("text = %q", text.String())
	}
	if !hasSearchTool(t, f.body(t, 0)) || hasSearchTool(t, f.body(t, 1)) {
		t.Error("want search offered first and dropped on the second request")
	}
}

// Out of credit is also a 400, and asking again would only fail again.
func TestASpendLimitIsNotAskedAgain(t *testing.T) {
	limit := cannedResponse{
		status: http.StatusBadRequest,
		body:   `{"type":"error","error":{"type":"invalid_request_error","message":"You have reached your specified API usage limits."}}`,
	}
	f, client := searchingClient(t, limit, limit)

	if _, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("creatine?")}, AllowWebSearch: true}); err == nil {
		t.Fatal("want the spend limit reported")
	}
	f.mu.Lock()
	made := len(f.bodies)
	f.mu.Unlock()
	if made != 1 {
		t.Errorf("%d requests made, want 1", made)
	}
}
