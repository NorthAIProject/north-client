package anthropic_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/anthropic"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestNewRefusesAnEmptyKey(t *testing.T) {
	if _, err := anthropic.New(anthropic.Options{}); err == nil {
		t.Fatal("a client with no key was built")
	}
}

func TestGenerateSendsTheSystemPromptCachedAndReadsTheReply(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("Sit back and down."))

	resp, err := client.Generate(context.Background(), ai.Request{
		System:   "You are Khepri.",
		Messages: []ai.Message{ai.UserText("show me a squat")},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if resp.Text != "Sit back and down." {
		t.Errorf("text = %q", resp.Text)
	}
	if resp.Model != "claude-opus-5" {
		t.Errorf("model = %q", resp.Model)
	}
	// Cache reads are input the account paid for; the ledger must see them.
	if resp.Usage.InputTokens != 112 || resp.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v, want 112 in (12 + 100 cached), 5 out", resp.Usage)
	}

	body := api.body(t, 0)
	if body["model"] != "claude-opus-5" {
		t.Errorf("model sent = %v", body["model"])
	}
	if body["max_tokens"] != float64(16000) {
		t.Errorf("max_tokens = %v, want the 16000 default", body["max_tokens"])
	}
	for _, key := range []string{"temperature", "top_p", "top_k"} {
		if _, ok := body[key]; ok {
			t.Errorf("%s was sent; Opus 5 rejects sampling parameters", key)
		}
	}
	system, _ := body["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("system = %v, want one block", body["system"])
	}
	block := system[0].(map[string]any)
	if block["text"] != "You are Khepri." || block["cache_control"] == nil {
		t.Errorf("system block = %v, want the prompt with cache_control", block)
	}
}

func TestUploadFileAsksCallersToInlineTheBytes(t *testing.T) {
	_, client := newFakeAPI(t)
	f, err := client.UploadFile(context.Background(), ai.UploadRequest{})
	if err != nil || f == nil || f.URI != "" {
		t.Fatalf("upload = %+v, %v; want an empty URI and no error", f, err)
	}
}

func TestAResponseSchemaAsksForJSON(t *testing.T) {
	api, client := newFakeAPI(t, textMessage(`{"days":3}`))
	_, err := client.Generate(context.Background(), ai.Request{
		Messages:       []ai.Message{ai.UserText("plan")},
		ResponseSchema: ai.Object("plan", map[string]*ai.Schema{"days": ai.Integer("days")}, "days"),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg, _ := api.body(t, 0)["output_config"].(map[string]any)
	format, _ := cfg["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"] == nil {
		t.Errorf("output_config = %v, want a json_schema format", api.body(t, 0)["output_config"])
	}
}

func TestAPersonalKeySendsWhatItAlwaysHas(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("ok"))
	_, err := client.Generate(context.Background(), ai.Request{
		Model:     "claude-sonnet-5-5",
		MaxTokens: 200,
		Messages:  []ai.Message{ai.UserText("hi")},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	body := api.body(t, 0)
	if body["model"] != "claude-sonnet-5-5" {
		t.Errorf("model = %v, want the one asked for", body["model"])
	}
	// No effort, no thinking setting: the model's defaults, as before managed
	// mode existed.
	for _, key := range []string{"thinking", "output_config"} {
		if v, ok := body[key]; ok {
			t.Errorf("%s = %v was sent for a personal key", key, v)
		}
	}
}

func TestTheManagedClientSendsEffortAndThinkingNextToTheSchema(t *testing.T) {
	api, client := newManagedFakeAPI(t, textMessage(`{"days":3}`))
	temp := float32(0.3)
	_, err := client.Generate(context.Background(), ai.Request{
		Messages:       []ai.Message{ai.UserText("plan")},
		Temperature:    &temp,
		ResponseSchema: ai.Object("plan", map[string]*ai.Schema{"days": ai.Integer("days")}, "days"),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	body := api.body(t, 0)

	cfg, _ := body["output_config"].(map[string]any)
	format, _ := cfg["format"].(map[string]any)
	if cfg["effort"] != "low" || format["type"] != "json_schema" {
		t.Errorf("output_config = %v, want effort low and the json_schema format together", body["output_config"])
	}
	thinking, _ := body["thinking"].(map[string]any)
	if thinking["type"] != "adaptive" {
		t.Errorf("thinking = %v, want adaptive", body["thinking"])
	}
	// Haiku 5.5 answers any non-default sampling value with a 400.
	for _, key := range []string{"temperature", "top_p", "top_k"} {
		if _, ok := body[key]; ok {
			t.Errorf("%s was sent", key)
		}
	}
}

func TestTheManagedClientReplacesAModelMeantForAnotherProvider(t *testing.T) {
	cases := map[string]string{
		"":                           "claude-haiku-5-5",
		"google/gemini-2.5-flash":    "claude-haiku-5-5",
		"anthropic/claude-haiku-4.5": "claude-haiku-5-5",
		"claude-sonnet-5-5":          "claude-sonnet-5-5",
	}
	for asked, want := range cases {
		t.Run(asked, func(t *testing.T) {
			api, client := newManagedFakeAPI(t, textMessage("ok"))
			_, err := client.Generate(context.Background(), ai.Request{
				Model: asked, Messages: []ai.Message{ai.UserText("hi")},
			})
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			if got := api.body(t, 0)["model"]; got != want {
				t.Errorf("model = %v, want %s", got, want)
			}
		})
	}
}

func TestThinkingIsDisabledWhenConfiguredOrTheLimitIsShort(t *testing.T) {
	cases := map[string]struct {
		opts      anthropic.Options
		maxTokens int
		want      string
	}{
		"short limit":    {anthropic.Options{Thinking: "adaptive", Managed: true}, 500, "disabled"},
		"configured off": {anthropic.Options{Thinking: "disabled", Managed: true}, 0, "disabled"},
		"room to think":  {anthropic.Options{Thinking: "adaptive", Managed: true}, 1024, "adaptive"},
		"personal key":   {anthropic.Options{}, 500, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api, client := newFakeAPIWith(t, tc.opts, textMessage("ok"))
			_, err := client.Generate(context.Background(), ai.Request{
				MaxTokens: tc.maxTokens, Messages: []ai.Message{ai.UserText("hi")},
			})
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			thinking, _ := api.body(t, 0)["thinking"].(map[string]any)
			if got, _ := thinking["type"].(string); got != tc.want {
				t.Errorf("thinking = %v, want %q", api.body(t, 0)["thinking"], tc.want)
			}
		})
	}
}

func TestRunningOutOfTokensBeforeAnsweringFailsOver(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-haiku-5-5",
		"content":     []any{map[string]any{"type": "thinking", "thinking": "", "signature": "sig"}},
		"stop_reason": "max_tokens",
		"usage":       map[string]any{"input_tokens": 10, "output_tokens": 1024},
	})
	_, client := newManagedFakeAPI(t, cannedResponse{body: string(b)})
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("hi")}})
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable so the chain answers", err)
	}
}

func TestATruncatedAnswerIsStillAnAnswer(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-haiku-5-5",
		"content":     []any{map[string]any{"type": "text", "text": "Sit back and"}},
		"stop_reason": "max_tokens",
		"usage":       map[string]any{"input_tokens": 10, "output_tokens": 4},
	})
	_, client := newManagedFakeAPI(t, cannedResponse{body: string(b)})
	resp, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("hi")}})
	if err != nil || resp.Text != "Sit back and" || resp.FinishReason != "max_tokens" {
		t.Fatalf("resp = %+v, err = %v; want the partial text with its finish reason", resp, err)
	}
}
