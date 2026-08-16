package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/NorthAIProject/north-client/internal/messaging"
)

func TestMarkdownToHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "that is **three** sessions", "that is <b>three</b> sessions"},
		{"italic", "you *could* rest", "you <i>could</i> rest"},
		{"underscore italic", "you _could_ rest", "you <i>could</i> rest"},
		{"inline code", "run `task dev` first", "run <code>task dev</code> first"},
		{"bullets", "- one\n- two", "• one\n• two"},
		{"asterisk bullets", "* one\n* two", "• one\n• two"},
		{"heading stripped", "## This Week\nthree sessions", "<b>This Week</b>\nthree sessions"},
		{"link", "see [the plan](https://north.test/p)", `see <a href="https://north.test/p">the plan</a>`},

		// Telegram rejects the whole message on a stray tag, so anything that
		// is not markup has to be escaped.
		{"escapes angle brackets", "5 < 7 and 9 > 2", "5 &lt; 7 and 9 &gt; 2"},
		{"escapes ampersand", "eggs & bacon", "eggs &amp; bacon"},
		{"escapes a literal tag", "<script>alert(1)</script>", "&lt;script&gt;alert(1)&lt;/script&gt;"},

		// Unmatched markers are prose, not markup.
		{"lone asterisk", "3 * 4 = 12", "3 * 4 = 12"},
		{"unclosed bold", "**not closed", "**not closed"},

		{"plain text untouched", "three sessions this week", "three sessions this week"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := markdownToHTML(c.in); got != c.want {
				t.Errorf("markdownToHTML(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

func TestFencedCodeBecomesAPreBlock(t *testing.T) {
	got := markdownToHTML("try this:\n```\ngo test ./...\n```")
	if !strings.Contains(got, "<pre>") || !strings.Contains(got, "go test ./...") {
		t.Fatalf("fenced code did not become a pre block: %q", got)
	}
}

// Code spans are literal. Markers inside one are text, not markup.
func TestMarkupInsideCodeIsNotInterpreted(t *testing.T) {
	got := markdownToHTML("use `**not bold**` here")
	if strings.Contains(got, "<b>") {
		t.Fatalf("markup inside code was interpreted: %q", got)
	}
}

func TestStripMarkdown(t *testing.T) {
	cases := map[string]string{
		"that is **three** sessions": "that is three sessions",
		"you _could_ rest":           "you could rest",
		"- one\n- two":               "• one\n• two",
		"## This Week":               "This Week",
		"run `task dev`":             "run task dev",
		"[the plan](https://x.test)": "the plan (https://x.test)",
		"5 < 7":                      "5 < 7",
	}
	for in, want := range cases {
		if got := stripMarkdown(in); got != want {
			t.Errorf("stripMarkdown(%q) = %q, want %q", in, got, want)
		}
	}
}

// refusingAPI rejects the first sendMessage and accepts everything after, which
// is how Telegram behaves when a parse_mode it cannot read reaches it.
type refusingAPI struct {
	mu     sync.Mutex
	bodies []map[string]any
	srv    *httptest.Server
}

func newRefusingAPI(t *testing.T) *refusingAPI {
	t.Helper()

	api := &refusingAPI{}
	api.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(r)

		api.mu.Lock()
		first := len(api.bodies) == 0
		api.bodies = append(api.bodies, body)
		api.mu.Unlock()

		if first {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"description":"can't parse entities: unexpected end of tag"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(api.srv.Close)
	return api
}

func (a *refusingAPI) sent() []map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]map[string]any(nil), a.bodies...)
}

// The safety net matters more than the converter. Slightly plain prose is fine;
// nothing at all, because a stray underscore broke the parser, is not.
func TestARefusedFormattedMessageIsResentAsPlainText(t *testing.T) {
	api := newRefusingAPI(t)

	client := NewClient("test-token")
	client.baseURL = api.srv.URL

	err := client.Send(context.Background(), "884422", messaging.OutboundMessage{
		Text: "that is **three** sessions",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	sent := api.sent()
	if len(sent) != 2 {
		t.Fatalf("expected a retry, got %d call(s)", len(sent))
	}

	if sent[0]["parse_mode"] != "HTML" {
		t.Fatalf("the first attempt should be formatted, got %v", sent[0]["parse_mode"])
	}
	if _, ok := sent[1]["parse_mode"]; ok {
		t.Fatalf("the retry should carry no parse_mode, got %v", sent[1]["parse_mode"])
	}
	if text, _ := sent[1]["text"].(string); text != "that is three sessions" {
		t.Fatalf("the retry should be stripped plain text, got %q", text)
	}
}
