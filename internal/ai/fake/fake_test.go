package fake_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
)

// collect drains a stream into its text and its terminating error. Every
// implementation of ai.Client must close its channel, so a broken one hangs
// here and is caught by the test timeout rather than in production.
func collect(t *testing.T, ch <-chan ai.StreamChunk) (string, *ai.Usage, error) {
	t.Helper()

	var text strings.Builder
	var usage *ai.Usage

	for chunk := range ch {
		if chunk.Err != nil {
			return text.String(), usage, chunk.Err
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		text.WriteString(chunk.Text)
	}

	return text.String(), usage, nil
}

func TestChatStreamsInMultipleChunks(t *testing.T) {
	t.Parallel()

	c := fake.Text("Add two and a half kilos next session.")

	ch, err := c.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	var chunks int
	var text strings.Builder
	for chunk := range ch {
		if chunk.Text != "" {
			chunks++
			text.WriteString(chunk.Text)
		}
	}

	if text.String() != "Add two and a half kilos next session." {
		t.Fatalf("reassembled text = %q", text.String())
	}
	// Streaming that arrives as one blob is not streaming; the UI would show
	// nothing and then everything.
	if chunks < 2 {
		t.Fatalf("expected the reply to arrive in several chunks, got %d", chunks)
	}
}

func TestChatReportsScriptedError(t *testing.T) {
	t.Parallel()

	boom := errors.New("provider exploded")
	c := fake.New(fake.Response{Err: boom})

	ch, err := c.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	if _, _, err := collect(t, ch); !errors.Is(err, boom) {
		t.Fatalf("expected the scripted error, got %v", err)
	}
}

func TestChatStopsWhenTheCallerGoesAway(t *testing.T) {
	t.Parallel()

	c := fake.Text(strings.Repeat("word ", 200))
	c.ChunkDelay = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := c.Chat(ctx, ai.Request{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	<-ch     // take one chunk
	cancel() // the browser tab closes

	// The channel must still close, or the goroutine behind it leaks for every
	// abandoned conversation.
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the stream did not close after its context was cancelled")
	}
}

func TestResponsesAreReturnedInOrderThenRepeat(t *testing.T) {
	t.Parallel()

	c := fake.New(
		fake.Response{Text: "first"},
		fake.Response{Text: "second"},
	)
	ctx := context.Background()

	for _, want := range []string{"first", "second", "second"} {
		got, err := c.Generate(ctx, ai.Request{})
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if got.Text != want {
			t.Fatalf("got %q, want %q", got.Text, want)
		}
	}
}

func TestHandlerCanInspectTheRequest(t *testing.T) {
	t.Parallel()

	c := &fake.Client{
		Handler: func(_ context.Context, req ai.Request) (fake.Response, error) {
			if req.ResponseSchema != nil {
				return fake.Response{Text: `{"ok":true}`}, nil
			}
			return fake.Response{Text: "prose"}, nil
		},
	}
	ctx := context.Background()

	plain, _ := c.Generate(ctx, ai.Request{})
	structured, _ := c.Generate(ctx, ai.Request{ResponseSchema: ai.String("anything")})

	if plain.Text != "prose" || structured.Text != `{"ok":true}` {
		t.Fatalf("handler did not see the schema: %q / %q", plain.Text, structured.Text)
	}
}

func TestCallsAreRecordedForAssertions(t *testing.T) {
	t.Parallel()

	c := fake.Text("ok")
	ctx := context.Background()

	if _, err := c.Generate(ctx, ai.Request{System: "you are a coach"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Services assert on this to check what the prompt builder produced.
	if got := c.LastCall().System; got != "you are a coach" {
		t.Fatalf("recorded system prompt = %q", got)
	}
	if len(c.Calls()) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(c.Calls()))
	}
}

func TestUploadConsumesTheReaderAndReturnsAURI(t *testing.T) {
	t.Parallel()

	c := fake.Text("ok")

	file, err := c.UploadFile(context.Background(), ai.UploadRequest{
		Name:     "squat.mp4",
		MIMEType: "video/mp4",
		Reader:   strings.NewReader("pretend this is a video"),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if file.URI == "" {
		t.Fatal("upload returned no URI")
	}
	if file.MIMEType != "video/mp4" {
		t.Fatalf("mime type = %q", file.MIMEType)
	}
	if len(c.Uploads()) != 1 {
		t.Fatalf("expected the upload to be recorded")
	}
}
