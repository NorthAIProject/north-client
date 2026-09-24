package anthropic_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/anthropic"
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
