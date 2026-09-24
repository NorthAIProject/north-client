package coach

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// relayed runs chunks through relay and returns the SSE body it wrote.
func relayed(ctx context.Context, chunks ...ai.StreamChunk) string {
	stream := make(chan ai.StreamChunk, len(chunks))
	for _, c := range chunks {
		stream <- c
	}
	close(stream)

	rec := httptest.NewRecorder()
	relay(ctx, rec, http.NewResponseController(rec), stream, func(error) {})
	return rec.Body.String()
}

// A tool round is a named "status" frame carrying the header's whole line,
// so the browser only has to put it on screen. Named, because htmx 4 swaps
// unnamed frames into the transcript — a status line there would read as
// something Khepri said.
func TestAToolRoundIsAnnouncedAsANamedStatusFrame(t *testing.T) {
	body := relayed(context.Background(),
		ai.StreamChunk{ToolCalls: []ai.ToolCall{{Name: "list_goals"}}},
		ai.StreamChunk{Text: "You have two goals."},
	)

	want := "event: status\ndata: is checking your goals\n\n"
	if !strings.Contains(body, want) {
		t.Errorf("no status frame %q in body %q", want, body)
	}
	if !strings.Contains(body, "data: You have two goals.\n\n") {
		t.Errorf("the reply after the tool did not go out as content; body = %q", body)
	}
	if strings.Index(body, "event: status") > strings.Index(body, "You have two goals.") {
		t.Errorf("the status frame came after the reply it precedes; body = %q", body)
	}
}

func TestTheStatusLineIsInTheReadersLanguage(t *testing.T) {
	cases := map[string]string{
		"en":    "is checking your goals",
		"es":    "está revisando tus objetivos",
		"pt-BR": "está verificando seus objetivos",
		"pt-PT": "está a ver os teus objetivos",
	}
	for tag, want := range cases {
		ctx := i18n.WithLocale(context.Background(), tag)
		body := relayed(ctx, ai.StreamChunk{ToolCalls: []ai.ToolCall{{Name: "list_goals"}}})
		if !strings.Contains(body, "data: "+want+"\n") {
			t.Errorf("%s: want %q in body %q", tag, want, body)
		}
	}
}

// A capability added to the registry without a label still says something
// true, rather than nothing or its snake_case name.
func TestAnUnlabelledToolFallsBackToAGenericLine(t *testing.T) {
	got := toolStatus(context.Background(), []ai.ToolCall{{Name: "brand_new_tool"}})
	if got != "is looking things up" {
		t.Errorf("toolStatus = %q, want the default line", got)
	}
	if got := toolStatus(context.Background(), nil); got != "is looking things up" {
		t.Errorf("toolStatus(nil) = %q, want the default line", got)
	}
}

// The first call names a round; the header has room for one phrase.
func TestTheFirstCallNamesTheRound(t *testing.T) {
	got := toolStatus(context.Background(), []ai.ToolCall{{Name: "search_exercises"}, {Name: "list_goals"}})
	if got != "is looking up exercises" {
		t.Errorf("toolStatus = %q, want the first call's label", got)
	}
}

// The contract caps the verb phrase at 32 characters: the pill does not wrap,
// and on a 390px phone anything longer runs under the header buttons.
func TestToolStatusLabelsFitTheHeader(t *testing.T) {
	for _, tag := range []string{"en", "es", "pt-BR", "pt-PT"} {
		for _, key := range ToolStatusKeys() {
			label := i18n.Translate(tag, key)
			if label == key {
				t.Errorf("%s: %s has no copy", tag, key)
				continue
			}
			if n := utf8.RuneCountInString(label); n > 32 {
				t.Errorf("%s: %s = %q is %d characters, the contract allows 32", tag, key, label, n)
			}
		}
	}
}

// A failure is a named "failed" signal and then the error panel. The order
// matters: the bridge reads any unnamed frame as a token, so a panel sent first
// would flash "is writing" over an error.
func TestAFailureSignalsBeforeItsPanel(t *testing.T) {
	body := relayed(context.Background(),
		ai.StreamChunk{Text: "Half a "},
		ai.StreamChunk{Err: errors.New("provider fell over")},
		ai.StreamChunk{Text: "never sent"},
	)

	signal := strings.Index(body, "event: failed\n")
	panel := strings.Index(body, "text-destructive")
	if signal < 0 || panel < 0 {
		t.Fatalf("want a failed signal and an error panel; body = %q", body)
	}
	if signal > panel {
		t.Errorf("the failed signal came after the panel; body = %q", body)
	}
	if strings.Contains(body, "never sent") {
		t.Errorf("relay kept writing after the stream failed; body = %q", body)
	}
}

func TestAStatusLineCannotBreakTheFrame(t *testing.T) {
	rec := httptest.NewRecorder()
	writeStatus(rec, http.NewResponseController(rec), "is\nchecking\r\n  things")
	if got, want := rec.Body.String(), "event: status\ndata: is checking things\n\n"; got != want {
		t.Errorf("frame = %q, want %q", got, want)
	}
}
