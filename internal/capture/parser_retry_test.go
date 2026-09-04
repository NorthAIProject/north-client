package capture_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/users"
)

const goodReply = `{"items":[{"source":"2L water","kind":"water","confidence":"high",
"amount_ml":2000,"minutes":0,"quality":0,"date":"","habit":"","weight":0,
"weight_unit":"","mood":0,"energy":0,"notes":"","food":"","grams":0}],"unparsed":[]}`

func parserOver(client ai.Client) *capture.AIParser {
	registry := ai.NewRegistry()
	registry.Register(client)
	return capture.NewAIParser(ai.NewRunner(registry, ai.NewChainSet([]string{client.Name()}, nil)), "")
}

func person() users.User {
	return users.User{DisplayName: "Fernando", Timezone: "Europe/Lisbon", Tier: users.TierFree}
}

// A malformed reply is not a provider refusing, so ai.Failover answers false
// and the walk stops. Without a retry, one bad reply costs the whole request —
// which is an ordinary event on a provider that has the schema asked for in the
// prompt rather than enforced by the API.
func TestAMalformedReplyIsCorrectedRatherThanFailed(t *testing.T) {
	t.Parallel()

	client := fake.New(
		fake.Response{Text: "Sure! Here are your entries:"},
		fake.Response{Text: goodReply},
	)

	draft, err := parserOver(client).Parse(context.Background(), person(), "2L water", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(draft.Items) != 1 || draft.Items[0].Water.AmountML != 2000 {
		t.Fatalf("draft = %+v, want one 2000 ml water entry", draft.Items)
	}

	calls := client.Calls()
	if len(calls) != 2 {
		t.Fatalf("made %d calls, want 2: the reply was not corrected", len(calls))
	}

	// The correction only works because it carries the model's own words back
	// with a named problem; "try again" produces the same reply.
	second := calls[1].Messages
	if len(second) < 3 {
		t.Fatalf("the retry sent %d messages, want the original plus the exchange", len(second))
	}
	var carried, named bool
	for _, m := range second {
		text := messageText(m)
		if strings.Contains(text, "Sure! Here are your entries:") {
			carried = true
		}
		if strings.Contains(text, "not valid JSON matching the schema") {
			named = true
		}
	}
	if !carried {
		t.Error("the retry did not carry the model's own reply back")
	}
	if !named {
		t.Error("the retry did not name the problem")
	}
}

// Two attempts, not more: a capture is a cheap call with a person waiting on
// it. A provider that cannot produce the shape twice is not going to on the
// third go.
func TestAPersistentlyBadProviderGivesUpAfterTwo(t *testing.T) {
	t.Parallel()

	client := fake.New(fake.Response{Text: "not json"})

	_, err := parserOver(client).Parse(context.Background(), person(), "2L water", nil)
	if err == nil {
		t.Fatal("want an error when the reply never decodes")
	}
	if calls := client.Calls(); len(calls) != 2 {
		t.Fatalf("made %d calls, want exactly 2", len(calls))
	}
}

// A reply that decodes first time must not cost a second call. The retry is
// for the exception, and paying for it every time would double the bill.
func TestAGoodReplyIsNotRetried(t *testing.T) {
	t.Parallel()

	client := fake.New(fake.Response{Text: goodReply})

	if _, err := parserOver(client).Parse(context.Background(), person(), "2L water", nil); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if calls := client.Calls(); len(calls) != 1 {
		t.Fatalf("made %d calls, want 1", len(calls))
	}
}

// The schema goes with every attempt, including the correction. Dropping it on
// the retry would ask the model to guess the shape it just got wrong.
func TestTheRetryStillCarriesTheSchema(t *testing.T) {
	t.Parallel()

	client := fake.New(
		fake.Response{Text: "nope"},
		fake.Response{Text: goodReply},
	)

	if _, err := parserOver(client).Parse(context.Background(), person(), "2L water", nil); err != nil {
		t.Fatalf("parse: %v", err)
	}

	for i, call := range client.Calls() {
		if call.ResponseSchema == nil {
			t.Errorf("call %d carried no response schema", i+1)
		}
	}
}

func messageText(m ai.Message) string {
	var b strings.Builder
	for _, part := range m.Parts {
		b.WriteString(part.Text)
	}
	return b.String()
}
