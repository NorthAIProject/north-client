package anthropic_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func apiError(status int, kind, message string) cannedResponse {
	b, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": kind, "message": message}})
	return cannedResponse{status: status, body: string(b)}
}

func TestErrorsLandInTheClassesTheRunnerActsOn(t *testing.T) {
	cases := map[string]struct {
		res  cannedResponse
		want error
	}{
		"bad key":      {apiError(401, "authentication_error", "invalid x-api-key"), apperr.ErrForbidden},
		"no credit":    {apiError(400, "invalid_request_error", "Your credit balance is too low to access the Anthropic API."), apperr.ErrPaymentRequired},
		"rate limited": {apiError(429, "rate_limit_error", "slow down"), apperr.ErrUnavailable},
		"overloaded":   {apiError(529, "overloaded_error", "Overloaded"), apperr.ErrUnavailable},
		"server error": {apiError(500, "api_error", "boom"), apperr.ErrUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Retries are the SDK's; give it the same answer every time.
			_, client := newFakeAPI(t, tc.res, tc.res, tc.res, tc.res)
			_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("hi")}})
			if !apperr.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "sk-ant-test") {
				t.Error("the error carries the key")
			}
		})
	}
}

func TestAMalformedRequestDoesNotWalkTheChain(t *testing.T) {
	_, client := newFakeAPI(t, apiError(400, "invalid_request_error", "messages: roles must alternate"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("hi")}})
	if err == nil || ai.Failover(err) {
		t.Fatalf("err = %v; a caller error must not fail over", err)
	}
}

func TestARefusalFailsOverRatherThanPostingNothing(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5",
		"content": []any{}, "stop_reason": "refusal",
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 0},
	})
	_, client := newFakeAPI(t, cannedResponse{body: string(b)})
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("hi")}})
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable so the chain answers", err)
	}
}
