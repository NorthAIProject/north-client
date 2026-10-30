package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/config"
)

// A native client's whole turn, through the real router and database with the
// fake model: start a conversation, send a message in the body of one POST,
// read the reply as typed events, and find it stored afterwards.
func TestCoachAPIRepliesAsTypedEvents(t *testing.T) {
	// testRoutesAndPool registers the fake model; the chain has to name it,
	// or the coach has no provider and answers 503.
	handler, pool := testRoutesAndPool(t, func(cfg *config.Config) { cfg.AI.Chain = []string{"fake"} })
	bearer := "Bearer " + signIn(t, pool).Value

	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", bearer)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := call(http.MethodPost, "/api/v1/conversations", `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	var started struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &started)

	rec = call(http.MethodPost, "/api/v1/conversations/"+started.ID+"/reply", `{"text":"How do I do a push-up?"}`)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("reply: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}

	events, text := readEvents(t, rec.Body.String())
	if len(events) == 0 || events[len(events)-1] != "done" {
		t.Fatalf("events = %v, want a stream ending in done", events)
	}
	if !strings.Contains(text, "unused") {
		t.Fatalf("streamed text = %q, want the fake model's reply", text)
	}

	rec = call(http.MethodGet, "/api/v1/conversations/"+started.ID, "")
	var detail struct {
		Messages []struct{ Role, Text string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if len(detail.Messages) != 2 || detail.Messages[0].Text != "How do I do a push-up?" || detail.Messages[1].Role != "coach" {
		t.Fatalf("stored messages = %+v, want the question and the reply", detail.Messages)
	}
}

// Refusals that need no model come back as ordinary JSON errors with a status,
// before any stream opens, so a client can tell them apart from a reply.
func TestCoachAPIRefusesAnEmptyTurnBeforeStreaming(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	bearer := "Bearer " + signIn(t, pool).Value

	start := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", strings.NewReader(`{}`))
	start.Header.Set("Authorization", bearer)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, start)
	var started struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &started)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+started.ID+"/reply", strings.NewReader(`{"text":"   "}`))
	req.Header.Set("Authorization", bearer)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("content type = %q, want JSON, not a stream", rec.Header().Get("Content-Type"))
	}
}

// readEvents returns the event names in order and the concatenated token text.
func readEvents(t *testing.T, body string) ([]string, string) {
	t.Helper()
	var (
		names []string
		text  strings.Builder
		event string
	)
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
			names = append(names, event)
		case strings.HasPrefix(line, "data: ") && event == "token":
			var token struct{ Text string }
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &token); err != nil {
				t.Fatalf("token data is not JSON: %q", line)
			}
			text.WriteString(token.Text)
		}
	}
	return names, text.String()
}
